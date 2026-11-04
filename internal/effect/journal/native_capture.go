package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

type NativeCaptureOptions struct {
	CaptureOptions
	OrderObservations         []pipackage.OrderObservation
	ProjectCarrierCompletions []durablecarrier.ManagedCarrierClaim
	WholeDocuments            []ManagedPathMutation
	Payloads                  payload.PayloadSet
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
	if ctx == nil || !batch.initialized || options.OperationAuthority == nil || options.Filesystem == nil || options.StateCodec == nil {
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
	mutations, err := pathMutations(options.ManagedPathMutations, options.ManagedAggregateMutations, options.ManagedPathEvidence)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	for _, order := range options.OrderObservations {
		if err := order.Validate(); err != nil {
			return CapturedNativeJournal{}, err
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
	wholeMutations, err := pathMutations(options.WholeDocuments, nil, options.ManagedPathEvidence)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := validateNativeCaptureWork(batch.before, after, edits, append(slices.Clone(mutations), wholeMutations...), options.ProjectCarrierCompletions); err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := ensureNoActive(ctx, paths.RecoveryDir, inventoryOptions{
		Filesystem: options.Filesystem, StateCodec: options.StateCodec, PhysicalPathBudget: batch.budget, recordDecoder: decodeNativeCaptureInventoryJournal,
	}); err != nil {
		return CapturedNativeJournal{}, err
	}
	directory, err = mutation.CanonicalDirectoryEntryPathBounded(filepath.Join(paths.RecoveryDir, operationID), physicalwork.MaximumPhysicalPathDepth, batch.budget)
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
	members, err := batch.captureMembers(ctx, after, options.OrderObservations...)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := validateNativePublicationPartition(ctx, directory, members, mutations, options.Resolver); err != nil {
		return CapturedNativeJournal{}, err
	}
	journal, comparisons, err := prepareNativeCapture(ctx, operationID, createdAt, members, after, edits, options.OrderObservations, options.RemovalDemands, options.WholeDocuments, options.Payloads)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	roots := batch.roots
	options.ManifestRoot = roots.Manifest.Root
	stageOptions := nativeDocumentStageOptions{stateCodec: options.StateCodec, codecs: options.Codecs, limits: batch.limits, budget: batch.budget}
	var fingerprint string
	prepared, err := options.Filesystem.PrepareRootedTree(ctx, capability, func(writer mutationfs.RootedTreeWriter) error {
		var err error
		if len(mutations)+len(options.ClaimTransitions)+len(options.ProvisionalAcquires) != 0 || options.RemovalDemands.Len() != 0 || len(journal.record.DocumentGroups) == 0 {
			fingerprint, err = journal.stageMixedDocumentJournal(ctx, paths, options.CaptureOptions, comparisons, writer, nativeMixedStageOptions{nativeDocumentStageOptions: stageOptions, roots: roots})
		} else {
			if err := writer.SetRootMode(retirement.DirectoryMode); err != nil {
				return err
			}
			fingerprint, err = journal.stageDocumentJournal(ctx, comparisons, writer, stageOptions)
		}
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

func prepareNativeCapture(ctx context.Context, operationID string, createdAt time.Time, batch NativeDocumentObservationBatch, after durable.Snapshot, edits []compose.ProjectionEdit, orders []pipackage.OrderObservation, ordinary recovery.RemovalDemandSet, whole []ManagedPathMutation, payloads payload.PayloadSet) (nativeJournalSnapshots, []journaldocument.Comparison, error) {
	inputs, err := batch.prepareOwnership(ctx, after, operationID)
	if err != nil {
		return nativeJournalSnapshots{}, nil, err
	}
	subjects, err := indexDocumentCaptureSubjects(ctx, batch.before, after)
	if err != nil {
		return nativeJournalSnapshots{}, nil, err
	}
	afterIndex, err := journaldocument.IndexSnapshot(ctx, after)
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
	groupOrders := make([]*pipackage.OrderObservation, len(inputs))
	groupWhole := make([]*ManagedPathMutation, len(inputs))
	for index, mutation := range whole {
		facts, err := wholeDocumentMutationFacts(mutation)
		if err != nil {
			return nativeJournalSnapshots{}, nil, err
		}
		if !aggregate.IsHostConfigDocumentSubject(facts.subject) {
			return nativeJournalSnapshots{}, nil, fmt.Errorf("whole capture requires its canonical native subject")
		}
		address, err := aggregate.NewDocumentAddress(facts.consumerTargets[0], facts.scope, facts.destination)
		if err != nil {
			return nativeJournalSnapshots{}, nil, err
		}
		found := false
		for group, namespace := range batch.namespaces {
			if !slices.Contains(namespace.Addresses(), address) {
				continue
			}
			if groupWhole[group] != nil {
				return nativeJournalSnapshots{}, nil, fmt.Errorf("native document has duplicate whole writers")
			}
			groupWhole[group], found = &whole[index], true
		}
		if !found {
			return nativeJournalSnapshots{}, nil, fmt.Errorf("native whole writer has no retained namespace")
		}
	}
	for _, order := range orders {
		if !order.Changed() {
			continue
		}
		alias, err := piOrderDocumentAlias(order.Scope())
		if err != nil {
			return nativeJournalSnapshots{}, nil, err
		}
		found := false
		for index, namespace := range batch.namespaces {
			if !slices.Contains(namespace.Addresses(), alias) {
				continue
			}
			if groupOrders[index] != nil {
				return nativeJournalSnapshots{}, nil, fmt.Errorf("selected document has duplicate Pi order actors")
			}
			groupOrders[index], found = &order, true
		}
		if !found {
			return nativeJournalSnapshots{}, nil, fmt.Errorf("Pi order has no selected native settings group")
		}
	}
	work := !batch.before.Equal(after)
	documentWork := false
	for index, input := range inputs {
		var candidate nativeDocumentCaptureCandidate
		var err error
		members, err := afterIndex.Subjects(ctx, batch.namespaces[index].Addresses())
		if err != nil {
			return nativeJournalSnapshots{}, nil, err
		}
		wholeAfter := false
		for _, subject := range members {
			if _, whole := afterIndex.WholePath(subject); whole {
				wholeAfter = true
				break
			}
		}
		if groupWhole[index] != nil {
			if len(groupEdits[index]) != 0 || groupOrders[index] != nil {
				return nativeJournalSnapshots{}, nil, fmt.Errorf("whole writer overlaps selected document work")
			}
			candidate, err = prepareWholeDocumentCandidate(ctx, batch.namespaces[index], wholeDocumentCaptureInput{nativeDocumentCaptureInput: input, mutation: *groupWhole[index], payloads: payloads})
		} else if wholeAfter && len(groupEdits[index]) == 0 && groupOrders[index] == nil {
			candidate, err = prepareRetainedWholeDocumentCandidate(ctx, batch.namespaces[index], input)
		} else {
			candidate, err = prepareSelectedDocumentCandidate(ctx, batch.namespaces[index], selectedDocumentCaptureInput{nativeDocumentCaptureInput: input, edits: groupEdits[index], order: groupOrders[index]}, subjects)
		}
		if err != nil {
			return nativeJournalSnapshots{}, nil, err
		}
		original := batch.originals[index]
		if !candidate.comparison.BeforeDocument().Equal(original.document) || !sameNativeImageMode(candidate.comparison.BeforeMode(), original.mode) {
			return nativeJournalSnapshots{}, nil, fmt.Errorf("selected candidate differs from its decision original")
		}
		documentWork = documentWork || !candidate.comparison.BeforeDocument().Equal(candidate.comparison.ExpectedDocument()) || !candidate.comparison.BeforeState().SameRootMetadata(candidate.comparison.ExpectedState())
		work = work || documentWork
		candidates = append(candidates, candidate)
	}
	if err := requireNativeDocumentCommitWork(batch.before, after, documentWork); err != nil {
		return nativeJournalSnapshots{}, nil, err
	}
	if !work {
		return nativeJournalSnapshots{}, nil, fmt.Errorf("native capture requires actual document or state work")
	}
	groups, comparisons, _, err := captureNativeDocumentCandidates(ctx, candidates, ordinary, batch.budget)
	if err != nil {
		return nativeJournalSnapshots{}, nil, err
	}
	grant, err := batch.manifest(ctx)
	if err != nil {
		return nativeJournalSnapshots{}, nil, err
	}
	manifest := persistedNativeRoot(grant.provenance)
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
