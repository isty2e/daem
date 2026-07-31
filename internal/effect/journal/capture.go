package journal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/filesystem/artifactstage"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

// CaptureOptions supplies boundary authority and ownership facts for journal capture.
// Managed path before-state comes from exact subject/address evidence; aggregate
// before-state comes from each validated mutation's snapshot and document. Callers
// do not supply a parallel raw observation model. ProjectRoot, OperationAuthority,
// and the RootedCapability callback are borrowed. Capture closes every capability
// returned by the authorities.
type CaptureOptions struct {
	ClaimTransitions          []ownershipmutation.ClaimTransition
	ManagedPathMutations      []ManagedPathMutation
	ManagedAggregateMutations []ManagedAggregateMutation
	ManagedPathEvidence       []observe.ManagedPathEvidence
	Resolver                  func(destination output.Destination) (string, error)
	ProjectRoot               *rootedpath.CapturedRoot
	OperationAuthority        *rootedpath.EntryAuthority
	RootedCapability          RootedCapabilityResolver
	Codecs                    aggregate.CodecCatalog
	StateCodec                durable.SnapshotCodec
	Filesystem                mutationfs.Store
}

// CaptureJournalWithOptions persists host, statefile, ownership, and project-root evidence.
// The caller must serialize recovery-directory mutation for the complete call.
func CaptureJournalWithOptions(
	ctx context.Context,
	paths Paths,
	operationID string,
	createdAt time.Time,
	currentState durable.Snapshot,
	nextState durable.Snapshot,
	options CaptureOptions,
) (result CaptureResult, resultErr error) {
	if options.Resolver == nil {
		return CaptureResult{}, fmt.Errorf("recovery journal destination resolver is required")
	}
	if options.StateCodec == nil {
		return CaptureResult{}, errRecoveryJournalStateCodecRequired
	}
	if options.Filesystem == nil {
		return CaptureResult{}, fmt.Errorf("recovery journal filesystem is required")
	}
	statefileBefore, statefileAfter, err := encodeRecoveryJournalSnapshots(
		currentState,
		nextState,
		options.StateCodec,
	)
	if err != nil {
		return CaptureResult{}, err
	}
	if err := ensureNoActive(ctx, paths.RecoveryDir, inventoryOptions{
		Filesystem: options.Filesystem,
		StateCodec: options.StateCodec,
	}); err != nil {
		return CaptureResult{}, err
	}
	if !isSafeRecoveryOperationID(operationID) {
		return CaptureResult{}, fmt.Errorf("recovery operation id %q must be a safe path component", operationID)
	}
	finalDir, err := mutation.CanonicalDirectoryEntryPath(filepath.Join(paths.RecoveryDir, operationID))
	if err != nil {
		return CaptureResult{}, fmt.Errorf("canonicalize recovery operation path: %w", err)
	}
	capability, ownedRoot, err := captureOperationCapability(
		finalDir,
		options.OperationAuthority,
	)
	if ownedRoot != nil {
		defer func() {
			if closeErr := ownedRoot.Close(); closeErr != nil {
				result = CaptureResult{}
				resultErr = errors.Join(
					resultErr,
					fmt.Errorf("close recovery journal publication root: %w", closeErr),
				)
			}
		}()
	}
	if err != nil {
		return CaptureResult{}, fmt.Errorf("bind recovery journal publication: %w", err)
	}
	capabilityOpen := true
	defer func() {
		if capabilityOpen {
			if closeErr := capability.Close(); closeErr != nil {
				result = CaptureResult{}
				resultErr = errors.Join(
					resultErr,
					fmt.Errorf("close recovery journal publication capability: %w", closeErr),
				)
			}
		}
	}()
	tempDir, err := os.MkdirTemp("", "daem-journal-build-")
	if err != nil {
		return CaptureResult{}, fmt.Errorf("create private recovery journal build tree: %w", err)
	}
	defer func() {
		if cleanupErr := removePrivateBuildTree(tempDir); cleanupErr != nil {
			published := resultErr == nil && result.OperationID != ""
			result = CaptureResult{}
			cleanupFailure := fmt.Errorf(
				"remove private recovery journal build tree: %w",
				cleanupErr,
			)
			if published {
				cleanupFailure = fmt.Errorf(
					"%w; recovery journal retained; run: daem recover --dry-run",
					cleanupFailure,
				)
			}
			resultErr = errors.Join(resultErr, cleanupFailure)
		}
	}()

	journal, err := buildRecoveryJournal(
		ctx,
		paths,
		tempDir,
		operationID,
		createdAt,
		currentState,
		nextState,
		options,
	)
	if err != nil {
		return CaptureResult{}, err
	}
	content, err := marshalRecoveryJournalWithStateContent(
		journal,
		statefileBefore,
		statefileAfter,
	)
	if err != nil {
		return CaptureResult{}, err
	}
	journalPath := filepath.Join(tempDir, recoveryJournalFileName)
	if err := os.WriteFile(journalPath, content, recoveryJournalMode); err != nil {
		return CaptureResult{}, fmt.Errorf("stage recovery journal: %w", err)
	}
	stagedHash, stagedKind, err := access.HashPath(ctx, tempDir)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("hash private recovery journal build tree: %w", err)
	}
	stagedIdentity, err := artifact.NewExactIdentity(
		artifact.SourceID("recovery:journal-build"),
		artifact.ResolvedRef(operationID),
		stagedKind,
		stagedHash,
	)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("identify private recovery journal build tree: %w", err)
	}
	stagedView, err := access.OpenView(tempDir)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("open private recovery journal build tree: %w", err)
	}
	prepared, err := options.Filesystem.PrepareRootedTree(
		ctx,
		capability,
		func(writer mutationfs.RootedTreeWriter) error {
			sink, err := artifactstage.New(writer)
			if err != nil {
				return err
			}
			return stagedView.CopyVerified(
				ctx,
				stagedIdentity,
				sink,
			)
		},
	)
	capabilityOpen = false
	if err != nil {
		return CaptureResult{}, fmt.Errorf("prepare recovery journal publication: %w", err)
	}
	if err := prepared.Commit(ctx); err != nil {
		return CaptureResult{}, fmt.Errorf("commit recovery journal: %w", err)
	}

	return CaptureResult{
		OperationID: operationID,
		Directory:   finalDir,
		JournalPath: filepath.Join(finalDir, recoveryJournalFileName),
	}, nil
}

func removePrivateBuildTree(root string) error {
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("private recovery journal build tree contains symlink %q", path)
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o700)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.RemoveAll(root)
}

func captureOperationCapability(
	finalDir string,
	authority *rootedpath.EntryAuthority,
) (
	rootedpath.CommitCapability,
	*rootedpath.CapturedRoot,
	error,
) {
	if authority != nil {
		capability, err := authority.Acquire()
		if err != nil {
			return nil, nil, err
		}
		if err := validateOperationCapabilityPath(capability, finalDir); err != nil {
			return nil, nil, errors.Join(err, capability.Close())
		}
		return capability, nil, nil
	}
	root, destination, err := rootedpath.CaptureDestination(finalDir)
	if err != nil {
		return nil, nil, err
	}
	capability, err := root.Acquire(destination)
	if err != nil {
		return nil, root, err
	}
	if err := validateOperationCapabilityPath(capability, finalDir); err != nil {
		return nil, root, errors.Join(err, capability.Close())
	}
	return capability, root, nil
}

func validateOperationCapabilityPath(
	capability rootedpath.CommitCapability,
	finalDir string,
) error {
	if capability == nil {
		return fmt.Errorf("recovery journal publication capability is required")
	}
	capabilityPath, err := capability.Destination().LexicalPath()
	if err != nil {
		return fmt.Errorf("read recovery journal publication destination: %w", err)
	}
	if filepath.Clean(capabilityPath) != filepath.Clean(finalDir) {
		return fmt.Errorf(
			"recovery journal publication destination %q does not match operation directory %q",
			capabilityPath,
			finalDir,
		)
	}
	return nil
}

func buildRecoveryJournal(
	ctx context.Context,
	paths Paths,
	operationDir string,
	operationID string,
	createdAt time.Time,
	currentState durable.Snapshot,
	nextState durable.Snapshot,
	options CaptureOptions,
) (result recoveryJournal, resultErr error) {
	mutations, err := pathMutations(
		options.ManagedPathMutations,
		options.ManagedAggregateMutations,
		options.ManagedPathEvidence,
	)
	if err != nil {
		return recoveryJournal{}, err
	}
	projectAuthority, err := projectAuthorityForCapture(paths, mutations, options.ProjectRoot)
	if err != nil {
		return recoveryJournal{}, err
	}
	if projectAuthority != nil && projectAuthority.owned {
		defer func() {
			if closeErr := projectAuthority.close(); closeErr != nil {
				result = recoveryJournal{}
				resultErr = errors.Join(resultErr, fmt.Errorf("close recovery journal project root: %w", closeErr))
			}
		}()
	}
	stateByAction, err := recoveryStateByAction(currentState)
	if err != nil {
		return recoveryJournal{}, err
	}
	backupIndex := 0
	contentPathBaselines, err := newRecoveryContentPathBaselineCache(
		mutations,
		options.Codecs,
		options.Filesystem,
	)
	if err != nil {
		return recoveryJournal{}, err
	}
	entries := make([]recoveryEntry, 0, len(mutations))
	for _, action := range mutations {
		if err := ctx.Err(); err != nil {
			return recoveryJournal{}, err
		}

		before, nextBackupIndex, err := captureRecoveryBeforePath(
			ctx,
			operationDir,
			backupIndex,
			action,
			options.Filesystem,
			options.Resolver,
			projectAuthority,
			options.RootedCapability,
			contentPathBaselines,
		)
		if err != nil {
			return recoveryJournal{}, err
		}
		backupIndex = nextBackupIndex
		stateBefore, err := captureRecoveryStateBefore(action, stateByAction)
		if err != nil {
			return recoveryJournal{}, err
		}
		expectedAfter, stateExpectedAfter, err := captureRecoveryExpectedAfter(action)
		if err != nil {
			return recoveryJournal{}, err
		}

		entries = append(entries, recoveryEntry{
			Subject:             subjectRefFromAction(action),
			Target:              string(action.Target),
			Targets:             targetStrings(action.ConsumerTargets),
			Scope:               string(action.Scope),
			Path:                action.Destination.String(),
			ContentPath:         string(action.ContentPath),
			ContentKind:         string(action.ContentKind),
			Before:              persistedBeforePathState(before),
			ExpectedAfter:       persistedExpectedPathState(expectedAfter),
			StateBeforeIdentity: stateBeforeIdentityFromAction(action),
			StateBefore:         stateBefore,
			StateExpectedAfter:  stateExpectedAfter,
			Aggregate:           persistedAggregateContractFromMutation(action),
			StateIndependent:    action.StateIndependent,
		})
	}

	sortRecoveryEntries(entries)
	persistedTransitions, err := recoveryClaimTransitions(options.ClaimTransitions)
	if err != nil {
		return recoveryJournal{}, err
	}
	if err := validateRecoveryClaimCoverage(entries, options.ClaimTransitions, options.Resolver); err != nil {
		return recoveryJournal{}, err
	}
	return recoveryJournal{
		Version:               recoveryJournalVersion,
		OperationID:           operationID,
		Operation:             recoveryOperationApply,
		CreatedAt:             createdAt.UTC().Format(time.RFC3339),
		ProjectRootProvenance: projectAuthority.persisted(),
		Entries:               entries,
		StatefileBefore:       currentState,
		StatefileAfter:        nextState,
		ClaimTransitions:      persistedTransitions,
	}, nil
}

func stateBeforeIdentityFromAction(action pathMutation) *recoveryStateIdentity {
	if action.StateIndependent || action.PreviousState == nil {
		return nil
	}

	previous := recoveryStateIdentityFromPrevious(*action.PreviousState)
	current := recoveryStateIdentityFromAction(action)
	if sameRecoveryStateIdentity(previous, current) {
		return nil
	}
	return &previous
}

func recoveryStateIdentityFromAction(action pathMutation) recoveryStateIdentity {
	return recoveryStateIdentity{
		Subject:     subjectRefFromAction(action),
		Target:      string(action.Target),
		Targets:     targetStrings(action.ConsumerTargets),
		Scope:       string(action.Scope),
		Path:        action.Destination.String(),
		ContentPath: string(action.ContentPath),
		ContentKind: string(action.ContentKind),
		Aggregate:   action.AggregateContract != nil,
	}
}

func recoveryStateIdentityFromPrevious(previous previousPathState) recoveryStateIdentity {
	return recoveryStateIdentity{
		Subject: persistedSubjectRef{
			Kind:      string(previous.Subject.Kind()),
			Namespace: previous.Subject.Namespace(),
			Name:      previous.Subject.Key(),
		},
		Target:      string(previous.Target),
		Targets:     targetStrings(previous.ConsumerTargets),
		Scope:       string(previous.Scope),
		Path:        previous.Destination.String(),
		ContentPath: string(previous.ContentPath),
		ContentKind: string(previous.ContentKind),
	}
}

func sameRecoveryStateIdentity(left recoveryStateIdentity, right recoveryStateIdentity) bool {
	if left.Subject != right.Subject || left.Target != right.Target ||
		left.Scope != right.Scope || left.Path != right.Path || left.ContentPath != right.ContentPath ||
		left.ContentKind != right.ContentKind || left.Aggregate != right.Aggregate || len(left.Targets) != len(right.Targets) {
		return false
	}
	for index := range left.Targets {
		if left.Targets[index] != right.Targets[index] {
			return false
		}
	}
	return true
}

func subjectRefFromAction(action pathMutation) persistedSubjectRef {
	if action.Subject.IsZero() {
		return persistedSubjectRef{}
	}
	return persistedSubjectRef{
		Kind:      string(action.Subject.Kind()),
		Namespace: action.Subject.Namespace(),
		Name:      action.Subject.Key(),
	}
}
