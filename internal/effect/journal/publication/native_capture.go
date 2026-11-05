package publication

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

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
	WholeDocuments            []journalrecord.ManagedPathMutation
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
	if ctx == nil || (batch.Evidence().Validate() != nil) || options.OperationAuthority == nil || options.Filesystem == nil || options.StateCodec == nil {
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
	mutations, err := journalrecord.NormalizeCaptureMutations(options.ManagedPathMutations, options.ManagedAggregateMutations, options.ManagedPathEvidence)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	for _, order := range options.OrderObservations {
		if err := order.Validate(); err != nil {
			return CapturedNativeJournal{}, err
		}
	}
	if !journalrecord.IsSafeOperationID(operationID) {
		return CapturedNativeJournal{}, fmt.Errorf("native recovery operation id must be a safe path component")
	}
	if _, _, err := journalrecord.EncodeSnapshots(batch.Evidence().BeforeState(), after, options.StateCodec); err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := validateNativeCaptureContext(ctx, paths, batch, options.CaptureOptions); err != nil {
		return CapturedNativeJournal{}, err
	}
	wholeMutations, err := journalrecord.NormalizeCaptureMutations(options.WholeDocuments, nil, options.ManagedPathEvidence)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := validateNativeCaptureWork(batch.Evidence().BeforeState(), after, edits, append(slices.Clone(mutations), wholeMutations...), options.ProjectCarrierCompletions); err != nil {
		return CapturedNativeJournal{}, err
	}
	if err := journalinventory.RequireClear(ctx, paths.RecoveryDir, journalinventory.Options{
		Filesystem: options.Filesystem, StateCodec: options.StateCodec, PhysicalPathBudget: batch.budget, Decoder: decodeNativeCaptureInventoryJournal,
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
	if err := journalqualification.RequireOperationDestination(capability, directory); err != nil {
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
	hasDocuments, err := journal.HasDocumentGroups()
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	roots := batch.roots
	options.ManifestRoot = roots.Manifest.Root
	stageOptions := StageOptions{StateCodec: options.StateCodec, Codecs: options.Codecs, Limits: batch.Evidence().DocumentLimits(), Budget: batch.budget}
	var fingerprint string
	prepared, err := options.Filesystem.PrepareRootedTree(ctx, capability, func(writer mutationfs.RootedTreeWriter) error {
		var err error
		if len(mutations)+len(options.ClaimTransitions)+len(options.ProvisionalAcquires) != 0 || options.RemovalDemands.Len() != 0 || !hasDocuments {
			fingerprint, err = stageMixedDocumentJournal(journal, ctx, paths, options.CaptureOptions, comparisons, writer, nativeMixedStageOptions{StageOptions: stageOptions, roots: roots})
		} else {
			if err := writer.SetRootMode(retirement.DirectoryMode); err != nil {
				return err
			}
			fingerprint, err = StageDocuments(journal, ctx, comparisons, writer, stageOptions)
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

func prepareNativeCapture(ctx context.Context, operationID string, createdAt time.Time, batch NativeDocumentObservationBatch, after durable.Snapshot, edits []compose.ProjectionEdit, orders []pipackage.OrderObservation, ordinary recovery.RemovalDemandSet, whole []journalrecord.ManagedPathMutation, payloads payload.PayloadSet) (journalrecord.Native, []journaldocument.Comparison, error) {
	inputs, err := batch.prepareOwnership(ctx, after, operationID)
	if err != nil {
		return journalrecord.Native{}, nil, err
	}
	namespaces := batch.Evidence().Namespaces()
	subjects, err := indexDocumentCaptureSubjects(ctx, batch.Evidence().BeforeState(), after)
	if err != nil {
		return journalrecord.Native{}, nil, err
	}
	afterIndex, err := journaldocument.IndexSnapshot(ctx, after)
	if err != nil {
		return journalrecord.Native{}, nil, err
	}
	groupEdits := make([][]compose.ProjectionEdit, len(namespaces))
	for _, edit := range edits {
		address := edit.Participant().Selection().DocumentAddress()
		found := false
		for index, namespace := range namespaces {
			if slices.Contains(namespace.Addresses(), address) {
				groupEdits[index] = append(groupEdits[index], edit)
				found = true
				break
			}
		}
		if !found {
			return journalrecord.Native{}, nil, fmt.Errorf("selected edit has no observed physical document group")
		}
	}
	candidates := make([]nativeDocumentCaptureCandidate, 0, len(inputs))
	groupOrders := make([]*pipackage.OrderObservation, len(inputs))
	groupWhole := make([]*journalrecord.ManagedPathMutation, len(inputs))
	for index, mutation := range whole {
		facts, err := wholeDocumentMutationFacts(mutation)
		if err != nil {
			return journalrecord.Native{}, nil, err
		}
		if !aggregate.IsHostConfigDocumentSubject(facts.Subject()) {
			return journalrecord.Native{}, nil, fmt.Errorf("whole capture requires its canonical native subject")
		}
		address, err := aggregate.NewDocumentAddress(facts.ConsumerTargets()[0], facts.Scope(), facts.Destination())
		if err != nil {
			return journalrecord.Native{}, nil, err
		}
		found := false
		for group, namespace := range namespaces {
			if !slices.Contains(namespace.Addresses(), address) {
				continue
			}
			if groupWhole[group] != nil {
				return journalrecord.Native{}, nil, fmt.Errorf("native document has duplicate whole writers")
			}
			groupWhole[group], found = &whole[index], true
		}
		if !found {
			return journalrecord.Native{}, nil, fmt.Errorf("native whole writer has no retained namespace")
		}
	}
	for _, order := range orders {
		if !order.Changed() {
			continue
		}
		alias, err := journalrecord.PiOrderDocumentAlias(order.Scope())
		if err != nil {
			return journalrecord.Native{}, nil, err
		}
		found := false
		for index, namespace := range namespaces {
			if !slices.Contains(namespace.Addresses(), alias) {
				continue
			}
			if groupOrders[index] != nil {
				return journalrecord.Native{}, nil, fmt.Errorf("selected document has duplicate Pi order actors")
			}
			groupOrders[index], found = &order, true
		}
		if !found {
			return journalrecord.Native{}, nil, fmt.Errorf("Pi order has no selected native settings group")
		}
	}
	work := !batch.Evidence().BeforeState().Equal(after)
	documentWork := false
	for index, input := range inputs {
		var candidate nativeDocumentCaptureCandidate
		var err error
		members, err := afterIndex.Subjects(ctx, namespaces[index].Addresses())
		if err != nil {
			return journalrecord.Native{}, nil, err
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
				return journalrecord.Native{}, nil, fmt.Errorf("whole writer overlaps selected document work")
			}
			candidate, err = prepareWholeDocumentCandidate(ctx, namespaces[index], wholeDocumentCaptureInput{CaptureInput: input, mutation: *groupWhole[index], payloads: payloads})
		} else if wholeAfter && len(groupEdits[index]) == 0 && groupOrders[index] == nil {
			candidate, err = prepareRetainedWholeDocumentCandidate(ctx, namespaces[index], input)
		} else {
			candidate, err = prepareSelectedDocumentCandidate(ctx, namespaces[index], selectedDocumentCaptureInput{CaptureInput: input, edits: groupEdits[index], order: groupOrders[index]}, subjects)
		}
		if err != nil {
			return journalrecord.Native{}, nil, err
		}
		comparison := candidate.record.Comparison()
		if !batch.evidence.MatchesOriginal(index, comparison.BeforeDocument(), comparison.BeforeMode()) {
			return journalrecord.Native{}, nil, fmt.Errorf("selected candidate differs from its decision original")
		}
		documentWork = documentWork || !comparison.BeforeDocument().Equal(comparison.ExpectedDocument()) || !comparison.BeforeState().SameRootMetadata(comparison.ExpectedState())
		work = work || documentWork
		candidates = append(candidates, candidate)
	}
	if err := journalrecord.RequireDocumentCommitWork(batch.Evidence().BeforeState(), after, documentWork); err != nil {
		return journalrecord.Native{}, nil, err
	}
	if !work {
		return journalrecord.Native{}, nil, fmt.Errorf("native capture requires actual document or state work")
	}
	groups, comparisons, _, err := captureNativeDocumentCandidates(ctx, candidates, ordinary, batch.budget)
	if err != nil {
		return journalrecord.Native{}, nil, err
	}
	grant, err := batch.manifest(ctx)
	if err != nil {
		return journalrecord.Native{}, nil, err
	}
	journal, err := journalrecord.NewNativeCapture(operationID, createdAt, batch.owner, grant.Provenance(), batch.Evidence().BeforeState(), after, groups)
	if err != nil {
		return journalrecord.Native{}, nil, err
	}
	return journal, comparisons, nil
}
