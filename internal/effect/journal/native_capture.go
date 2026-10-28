package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

type NativeCaptureOptions struct {
	CaptureOptions
	OrderObservations []pipackage.OrderObservation
}

// NativeJournalPublicationError retains the storage conclusion when a commit
// was attempted but capture could not return qualified evidence. It is not a
// rollback conclusion; the selected recovery directory must be inspected.
type NativeJournalPublicationError struct {
	directory string
	outcome   mutationfs.CommitOutcome
	cause     error
}

func (failure *NativeJournalPublicationError) Error() string {
	return fmt.Sprintf("native journal publication at %q has outcome %q: %v", failure.directory, failure.outcome.State(), failure.cause)
}

func (failure *NativeJournalPublicationError) Unwrap() error     { return failure.cause }
func (failure *NativeJournalPublicationError) Directory() string { return failure.directory }
func (failure *NativeJournalPublicationError) Outcome() mutationfs.CommitOutcome {
	return failure.outcome
}

// CaptureNativeJournalWithOptions borrows live observation roots and publishes
// only journal artifacts. The containing owner serializes recovery mutation,
// supplies effect-derived After/edits and binds OperationAuthority with the same
// allocation. EntryAuthority owns that private traversal-policy correlation.
// Success is reloaded capture evidence, never a host/state/Registry write grant.
func CaptureNativeJournalWithOptions(
	ctx context.Context,
	paths Paths,
	operationID string,
	createdAt time.Time,
	batch NativeDocumentObservationBatch,
	after durable.Snapshot,
	edits []compose.ProjectionEdit,
	options NativeCaptureOptions,
) (result CapturedNativeJournal, resultErr error) {
	if ctx == nil || !batch.initialized || len(batch.namespaces) == 0 || options.OperationAuthority == nil || options.Filesystem == nil || options.StateCodec == nil {
		return CapturedNativeJournal{}, fmt.Errorf("native capture requires context, live observations, selected publication authority, filesystem and state codec")
	}
	var outcome mutationfs.CommitOutcome
	commitAttempted := false
	var directory string
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = CapturedNativeJournal{}
			if commitAttempted {
				resultErr = &NativeJournalPublicationError{directory: directory, outcome: outcome, cause: resultErr}
			}
		}
	}()
	if err := errors.Join(ctx.Err(), batch.budget.RequireCapturePending()); err != nil {
		return CapturedNativeJournal{}, err
	}
	if len(options.ManagedPathMutations)+len(options.ManagedAggregateMutations)+len(options.ClaimTransitions)+len(options.ProvisionalAcquires) != 0 || options.RemovalDemands.Len() != 0 {
		return CapturedNativeJournal{}, fmt.Errorf("mixed ordinary capture requires cumulative source-build budget integration")
	}
	for _, order := range options.OrderObservations {
		if err := order.Validate(); err != nil {
			return CapturedNativeJournal{}, err
		}
		if order.Changed() {
			return CapturedNativeJournal{}, fmt.Errorf("changed Pi order requires its native publication integration")
		}
	}
	if !isSafeRecoveryOperationID(operationID) {
		return CapturedNativeJournal{}, fmt.Errorf("native recovery operation id must be a safe path component")
	}
	if _, _, err := encodeRecoveryJournalSnapshots(batch.before, after, options.StateCodec); err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := validateNativeCaptureContext(ctx, paths, batch, options.CaptureOptions); err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := validateNativeCaptureWork(batch.before, after, edits); err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := ensureNoActive(ctx, paths.RecoveryDir, inventoryOptions{
		Filesystem: options.Filesystem, StateCodec: options.StateCodec, PhysicalPathBudget: batch.budget, recordDecoder: decodeNativeCaptureInventoryJournal,
	}); err != nil {
		return CapturedNativeJournal{}, err
	}
	var err error
	directory, err = mutation.CanonicalDirectoryEntryPathBounded(filepath.Join(paths.RecoveryDir, operationID), recovery.MaximumPhysicalPathDepth, batch.budget)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	capability, err := options.OperationAuthority.Acquire()
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	capabilityOpen := true
	defer func() {
		if capabilityOpen {
			resultErr = errors.Join(resultErr, capability.Close())
		}
	}()
	if err := validateOperationCapabilityPath(capability, directory); err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := validateNativePublicationPartition(ctx, directory, batch); err != nil {
		return CapturedNativeJournal{}, err
	}
	journal, comparisons, err := prepareNativeCapture(ctx, operationID, createdAt, batch, after, edits)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	roots := batch.namespaces[0].roots
	options.ManifestRoot = roots.Manifest.Root
	stageOptions := nativeDocumentStageOptions{stateCodec: options.StateCodec, codecs: options.Codecs, limits: batch.limits, budget: batch.budget}
	var fingerprint string
	prepared, err := options.Filesystem.PrepareRootedTree(ctx, capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.SetRootMode(retirement.DirectoryMode); err != nil {
			return err
		}
		var err error
		fingerprint, err = journal.stageDocumentJournal(ctx, comparisons, writer, stageOptions)
		return err
	})
	capabilityOpen = false // PrepareRootedTree consumes it on every outcome.
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, prepared.Abort(context.WithoutCancel(ctx))) }()
	if err := batch.validateOriginals(ctx); err != nil {
		return CapturedNativeJournal{}, err
	}
	commitAttempted = true
	outcome, err = prepared.CommitWithOutcome(ctx)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if outcome.State() != mutationfs.CommitOutcomeComplete {
		return CapturedNativeJournal{}, fmt.Errorf("native journal publication did not establish complete storage visibility")
	}
	result, err = reloadNativeCapture(ctx, directory, operationID, fingerprint, batch, after, options.CaptureOptions)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := batch.validateOriginals(ctx); err != nil {
		return CapturedNativeJournal{}, err
	}
	return result, nil
}

func prepareNativeCapture(ctx context.Context, operationID string, createdAt time.Time, batch NativeDocumentObservationBatch, after durable.Snapshot, edits []compose.ProjectionEdit) (nativeJournalSnapshots, []DocumentComparison, error) {
	inputs, err := batch.prepareOwnership(ctx, after, operationID)
	if err != nil {
		return nativeJournalSnapshots{}, nil, err
	}
	subjects, err := indexDocumentCaptureSubjects(ctx, batch.before, after)
	if err != nil {
		return nativeJournalSnapshots{}, nil, err
	}
	groupEdits := make([][]compose.ProjectionEdit, len(batch.namespaces))
	for _, edit := range edits {
		address := edit.Participant().Selection().DocumentAddress()
		found := false
		for index, namespace := range batch.namespaces {
			if slices.Contains(namespace.Addresses(), address) {
				groupEdits[index] = append(groupEdits[index], edit)
				found = true
				break
			}
		}
		if !found {
			return nativeJournalSnapshots{}, nil, fmt.Errorf("selected edit has no observed physical document group")
		}
	}
	candidates := make([]nativeDocumentCaptureCandidate, 0, len(inputs))
	work := !batch.before.Equal(after)
	documentWork := false
	for index, input := range inputs {
		candidate, err := prepareSelectedDocumentCandidate(ctx, batch.namespaces[index], selectedDocumentCaptureInput{nativeDocumentCaptureInput: input, edits: groupEdits[index]}, subjects)
		if err != nil {
			return nativeJournalSnapshots{}, nil, err
		}
		original := batch.originals[index]
		if !candidate.comparison.before.Equal(original.document) || !sameNativeImageMode(candidate.comparison.beforeMode(), original.mode) {
			return nativeJournalSnapshots{}, nil, fmt.Errorf("selected candidate differs from its decision original")
		}
		documentWork = documentWork || !candidate.comparison.before.Equal(candidate.comparison.expected) || !candidate.comparison.beforeState.SameRootMetadata(candidate.comparison.expectedState)
		work = work || documentWork
		candidates = append(candidates, candidate)
	}
	if err := requireNativeDocumentCommitWork(batch.before, after, documentWork); err != nil {
		return nativeJournalSnapshots{}, nil, err
	}
	if !work {
		return nativeJournalSnapshots{}, nil, fmt.Errorf("native capture requires actual document or state work")
	}
	groups, comparisons, _, err := captureNativeDocumentCandidates(ctx, candidates, recovery.RemovalDemandSet{}, batch.budget)
	if err != nil {
		return nativeJournalSnapshots{}, nil, err
	}
	manifest := persistedNativeRoot(candidates[0].manifest.provenance)
	record := nativeJournalRecord{
		Version: nativeRecoveryJournalVersion, Operation: recoveryOperationApply, OperationID: operationID, CreatedAt: createdAt.UTC().Format(time.RFC3339),
		StateAuthority:         recoveryStateAuthority{StatefileAuthority: *persistedPathAuthority(batch.owner.StatefileAuthority()), ManifestPath: batch.owner.ManifestPath()},
		ManifestRootProvenance: manifest, Entries: []recoveryEntry{}, ClaimTransitions: []recoveryClaimTransition{}, ProvisionalAcquires: []recoveryProvisionalAcquireIntent{}, RemovalIntents: []recoveryRemovalIntent{}, DocumentGroups: groups,
	}
	return nativeJournalSnapshots{record: record, owner: batch.owner, before: batch.before, after: after}, comparisons, nil
}

func sameNativeImageMode(left, right *recovery.PermissionMode) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
