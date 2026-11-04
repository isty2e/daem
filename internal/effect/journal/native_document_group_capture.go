package journal

import (
	"context"
	"fmt"
	"slices"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type wholeDocumentCaptureInput struct {
	journalobservation.CaptureInput
	mutation ManagedPathMutation
	payloads payload.PayloadSet
}

// prepareWholeDocumentGroup captures persistence facts, not a publication seal.
// The containing operation must admit complete snapshots and all group relations.
func prepareWholeDocumentGroup(ctx context.Context, namespace journalnamespace.DocumentNamespace, input wholeDocumentCaptureInput) (recoveryDocumentGroup, journaldocument.Comparison, error) {
	candidate, err := prepareWholeDocumentCandidate(ctx, namespace, input)
	if err != nil {
		return recoveryDocumentGroup{}, journaldocument.Comparison{}, err
	}
	return candidate.captureRemoval(ctx, input.Budget())
}

func prepareWholeDocumentCandidate(ctx context.Context, namespace journalnamespace.DocumentNamespace, input wholeDocumentCaptureInput) (nativeDocumentCaptureCandidate, error) {
	if err := input.CaptureInput.Validate(ctx); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	facts, err := wholeDocumentMutationFacts(input.mutation)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	address, err := aggregate.NewDocumentAddress(facts.consumerTargets[0], facts.scope, facts.destination)
	if err != nil || !slices.Contains(namespace.Addresses(), address) {
		return nativeDocumentCaptureCandidate{}, fmt.Errorf("whole document mutation has no captured namespace alias")
	}
	draft, _, err := nativeDocumentGroupMetadata(ctx, namespace, input.CaptureInput)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}

	original, mode, _, err := namespace.ReadReferent(ctx, min(input.Limits().MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), input.Budget())
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	comparison, err := PrepareWholeDocument(ctx, WholeDocumentPreparationInput{Before: original, BeforeMode: mode, Limits: input.Limits()}, input.mutation, input.payloads)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	record, err := draft.withComparison(comparison, input.GroupIndex(), nil)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	if err := ctx.Err(); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	return nativeDocumentCaptureCandidate{record: record, namespace: namespace, relations: []aggregate.DocumentAddress{address}, budget: input.Budget()}, nil
}

func prepareRetainedWholeDocumentCandidate(ctx context.Context, namespace journalnamespace.DocumentNamespace, input journalobservation.CaptureInput) (nativeDocumentCaptureCandidate, error) {
	if err := input.Validate(ctx); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	draft, _, err := nativeDocumentGroupMetadata(ctx, namespace, input)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	original, mode, _, err := namespace.ReadReferent(ctx, min(input.Limits().MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), input.Budget())
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	comparison, err := journaldocument.NewWholeComparison(ctx, journaldocument.ComparisonInput{Before: original, Expected: original, BeforeMode: mode, ExpectedMode: mode, Limits: input.Limits()})
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	record, err := draft.withComparison(comparison, input.GroupIndex(), nil)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	return nativeDocumentCaptureCandidate{record: record, namespace: namespace, relations: namespace.Addresses(), budget: input.Budget()}, nil
}

func nativeDocumentGroupMetadata(ctx context.Context, namespace journalnamespace.DocumentNamespace, input journalobservation.CaptureInput) (documentGroupRecordDraft, journalnamespace.RootGrant, error) {
	draft, err := beginDocumentGroupRecord(documentGroupCaptureFacts{owner: input.Owner(), operationID: input.OperationID(), bindings: input.Bindings(), claims: input.Claims(), provisional: input.Provisional()})
	if err != nil {
		return documentGroupRecordDraft{}, journalnamespace.RootGrant{}, err
	}
	grants, err := namespace.QualifyRoots(ctx, input.Budget())
	if err != nil {
		return documentGroupRecordDraft{}, journalnamespace.RootGrant{}, err
	}
	manifest, present := grants.Manifest()
	if !present {
		return documentGroupRecordDraft{}, journalnamespace.RootGrant{}, fmt.Errorf("native document capture requires the operation manifest root")
	}

	aliases := make([]documentGroupAliasFacts, 0, len(namespace.Aliases()))
	for _, alias := range namespace.Aliases() {
		aliases = append(aliases, documentGroupAliasFacts{address: alias.Address(), root: alias.Root().Provenance(), observation: alias.Observation()})
	}
	pin := namespace.WriterPin()
	draft, err = draft.withNamespace(ctx, namespace.Observation(), pin.Provenance(), pin.Relative(), aliases, manifest.Provenance())
	if err != nil {
		return documentGroupRecordDraft{}, journalnamespace.RootGrant{}, err
	}
	return draft, manifest, nil
}
