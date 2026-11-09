package publication

import (
	"context"
	"fmt"
	"slices"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type wholeDocumentCaptureInput struct {
	journalobservation.CaptureInput
	mutation journalrecord.ManagedPathMutation
	payloads payload.PayloadSet
}

func prepareWholeDocumentCandidate(ctx context.Context, namespace journalnamespace.DocumentNamespace, input wholeDocumentCaptureInput) (nativeDocumentCaptureCandidate, error) {
	if err := input.CaptureInput.Validate(ctx); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	facts, err := wholeDocumentMutationFacts(input.mutation)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	address, err := aggregate.NewDocumentAddress(facts.ConsumerTargets()[0], facts.Scope(), facts.Destination())
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
	record, err := draft.WithComparison(comparison, input.GroupIndex(), nil)
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
	record, err := draft.WithComparison(comparison, input.GroupIndex(), nil)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	return nativeDocumentCaptureCandidate{record: record, namespace: namespace, relations: namespace.Addresses(), budget: input.Budget()}, nil
}

func nativeDocumentGroupMetadata(ctx context.Context, namespace journalnamespace.DocumentNamespace, input journalobservation.CaptureInput) (journalrecord.DocumentGroupDraft, journalnamespace.RootGrant, error) {
	draft, err := journalrecord.BeginDocumentGroup(input.Owner(), input.OperationID(), input.Bindings(), input.Claims(), input.Provisional())
	if err != nil {
		return journalrecord.DocumentGroupDraft{}, journalnamespace.RootGrant{}, err
	}
	grants, err := namespace.QualifyRoots(ctx, input.Budget())
	if err != nil {
		return journalrecord.DocumentGroupDraft{}, journalnamespace.RootGrant{}, err
	}
	manifest, present := grants.Manifest()
	if !present {
		return journalrecord.DocumentGroupDraft{}, journalnamespace.RootGrant{}, fmt.Errorf("native document capture requires the operation manifest root")
	}

	aliases := make([]journalrecord.DocumentGroupAlias, 0, len(namespace.Aliases()))
	for _, alias := range namespace.Aliases() {
		binding, bound := alias.Binding()
		if !bound {
			return journalrecord.DocumentGroupDraft{}, journalnamespace.RootGrant{}, fmt.Errorf("native17 capture requires the actual alias binding")
		}
		aliases = append(aliases, journalrecord.NewBoundDocumentGroupAlias(alias.Address(), binding, alias.Root().Provenance(), alias.Observation()))
	}
	pin := namespace.WriterPin()
	spelling, err := namespace.WriterPath()
	if err != nil {
		return journalrecord.DocumentGroupDraft{}, journalnamespace.RootGrant{}, err
	}
	draft, err = draft.WithBoundNamespace(ctx, namespace.Observation(), pin, spelling, aliases, manifest.Provenance())
	if err != nil {
		return journalrecord.DocumentGroupDraft{}, journalnamespace.RootGrant{}, err
	}
	return draft, manifest, nil
}
