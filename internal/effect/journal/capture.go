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
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/filesystem/artifactstage"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

// CaptureOptions supplies boundary authority and ownership facts for journal capture.
// Managed path before-state comes from exact subject/address evidence; aggregate
// before-state comes from each validated mutation's snapshot and document. Callers
// do not supply a parallel raw observation model. ManifestRoot, OperationAuthority,
// and the RootedCapability callback are borrowed. Capture closes every capability
// returned by the authorities.
type CaptureOptions struct {
	ClaimTransitions          []ownershipmutation.ClaimTransition
	ProvisionalAcquires       []ownership.ProvisionalAcquireIntent
	ManagedPathMutations      []journalrecord.ManagedPathMutation
	ManagedAggregateMutations []journalrecord.ManagedAggregateMutation
	ManagedPathEvidence       []observe.ManagedPathEvidence
	RemovalDemands            recovery.RemovalDemandSet
	Resolver                  func(destination output.Destination) (string, error)
	ManifestRoot              *rootedpath.CapturedRoot
	OperationAuthority        *rootedpath.EntryAuthority
	RootedCapability          RootedCapabilityResolver
	Codecs                    aggregate.CodecCatalog
	StateCodec                durable.SnapshotCodec
	Filesystem                mutationfs.Store
}

// CaptureJournalWithOptions persists host, statefile, ownership, and rooted-path evidence.
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
		return CaptureResult{}, journalrecord.ErrStateCodecRequired
	}
	if options.Filesystem == nil {
		return CaptureResult{}, fmt.Errorf("recovery journal filesystem is required")
	}
	statefileBefore, statefileAfter, err := journalrecord.EncodeSnapshots(
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
	if !journalrecord.IsSafeOperationID(operationID) {
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

	material, err := buildOrdinaryCaptureMaterial(
		ctx,
		paths,
		tempDir,
		operationID,
		createdAt,
		currentState,
		nextState,
		options,
		&ordinaryCaptureWork{},
	)
	if err != nil {
		return CaptureResult{}, err
	}
	content, err := material.EncodeOrdinary(
		statefileBefore,
		statefileAfter,
	)
	if err != nil {
		return CaptureResult{}, err
	}
	journalPath := filepath.Join(tempDir, journalrecord.FileName)
	if err := os.WriteFile(journalPath, content, journalrecord.Mode); err != nil {
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
		OperationID:       operationID,
		Directory:         finalDir,
		JournalPath:       filepath.Join(finalDir, journalrecord.FileName),
		RecordFingerprint: journalrecord.Fingerprint(content),
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

func buildOrdinaryCaptureMaterial(
	ctx context.Context,
	paths Paths,
	operationDir string,
	operationID string,
	createdAt time.Time,
	currentState durable.Snapshot,
	nextState durable.Snapshot,
	options CaptureOptions,
	work *ordinaryCaptureWork,
) (result journalrecord.OrdinaryEvidence, resultErr error) {
	if work == nil {
		return journalrecord.OrdinaryEvidence{}, fmt.Errorf("ordinary capture requires its selected work policy")
	}
	options.RootedCapability = work.rootedResolver(options.RootedCapability)
	var filesystem mutationfs.Reader = options.Filesystem
	if work.budget != nil {
		filesystem = ordinaryCaptureReader{Reader: filesystem, work: work}
	}

	mutations, err := journalrecord.NormalizeCaptureMutations(
		options.ManagedPathMutations,
		options.ManagedAggregateMutations,
		options.ManagedPathEvidence,
	)
	if err != nil {
		return journalrecord.OrdinaryEvidence{}, err
	}
	globalPathBindings, err := work.globalBindings(
		mutations,
		options.Resolver,
		options.RootedCapability,
	)
	if err != nil {
		return journalrecord.OrdinaryEvidence{}, err
	}
	boundResolver := globalPathBindings.resolver(options.Resolver)
	manifestAuthority, err := work.manifest(paths, options.ManifestRoot)
	if err != nil {
		return journalrecord.OrdinaryEvidence{}, err
	}
	if manifestAuthority.owned {
		defer func() {
			if closeErr := manifestAuthority.close(); closeErr != nil {
				result = journalrecord.OrdinaryEvidence{}
				resultErr = errors.Join(resultErr, fmt.Errorf("close recovery journal manifest root: %w", closeErr))
			}
		}()
	}
	draft, err := journalrecord.NewOrdinaryDraft(operationID, createdAt, currentState, nextState, manifestAuthority.provenance, mutations, options.RemovalDemands)
	if err != nil {
		return journalrecord.OrdinaryEvidence{}, err
	}
	backupIndex := 0
	contentPathBaselines, err := newRecoveryContentPathBaselineCache(mutations, options.Codecs, filesystem)
	if err != nil {
		return journalrecord.OrdinaryEvidence{}, err
	}
	for range draft.EntryCount() {
		action, err := draft.BeginEntry(ctx)
		if err != nil {
			return journalrecord.OrdinaryEvidence{}, err
		}
		before, nextBackupIndex, err := captureRecoveryBeforePath(
			ctx, operationDir, backupIndex, action, filesystem, boundResolver,
			manifestAuthority, options.RootedCapability, contentPathBaselines, work,
		)
		if err != nil {
			return journalrecord.OrdinaryEvidence{}, err
		}
		backupIndex = nextBackupIndex
		if err := draft.SupplyBefore(before); err != nil {
			return journalrecord.OrdinaryEvidence{}, err
		}
		binding, err := globalPathBindings.captured(action.Scope(), action.Destination())
		if err != nil {
			return journalrecord.OrdinaryEvidence{}, err
		}
		if err := draft.SupplyGlobalBinding(binding); err != nil {
			return journalrecord.OrdinaryEvidence{}, err
		}
	}
	if err := draft.CloseEntries(); err != nil {
		return journalrecord.OrdinaryEvidence{}, err
	}
	removalIntents, err := work.removalIntents(ctx, options.RemovalDemands, boundResolver)
	if err != nil {
		return journalrecord.OrdinaryEvidence{}, err
	}
	if err := draft.SupplyRemovalIntents(removalIntents); err != nil {
		return journalrecord.OrdinaryEvidence{}, err
	}
	if err := draft.BeginClaimCoverage(options.ClaimTransitions, options.ProvisionalAcquires); err != nil {
		return journalrecord.OrdinaryEvidence{}, err
	}
	if err := captureClaimAuthorities(ctx, boundResolver, draft.NextClaimAuthority, draft.BindClaimAuthority); err != nil {
		return journalrecord.OrdinaryEvidence{}, err
	}
	return draft.Finish()
}
