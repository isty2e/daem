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
	"github.com/isty2e/daem/internal/target"
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
	group, manifest, err := nativeDocumentGroupMetadata(ctx, namespace, input.CaptureInput)
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
	_, group.Original, group.Expected, err = documentImageReferences(comparison, input.GroupIndex())
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	group.Extent = "whole"
	if err := ctx.Err(); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	return nativeDocumentCaptureCandidate{group: group, comparison: comparison, namespace: namespace, manifest: manifest, relations: []aggregate.DocumentAddress{address}, budget: input.Budget()}, nil
}

func prepareRetainedWholeDocumentCandidate(ctx context.Context, namespace journalnamespace.DocumentNamespace, input journalobservation.CaptureInput) (nativeDocumentCaptureCandidate, error) {
	if err := input.Validate(ctx); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	group, manifest, err := nativeDocumentGroupMetadata(ctx, namespace, input)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	original, mode, _, err := namespace.ReadReferent(ctx, min(input.Limits().MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), input.Budget())
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	comparison, err := journaldocument.NewWholeComparison(ctx, journaldocument.ComparisonInput{
		Before: original, Expected: original, BeforeMode: mode, ExpectedMode: mode, Limits: input.Limits(),
	})
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	_, group.Original, group.Expected, err = documentImageReferences(comparison, input.GroupIndex())
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	group.Extent = "whole"
	return nativeDocumentCaptureCandidate{group: group, comparison: comparison, namespace: namespace, manifest: manifest, relations: namespace.Addresses(), budget: input.Budget()}, nil
}

func nativeDocumentGroupMetadata(ctx context.Context, namespace journalnamespace.DocumentNamespace, input journalobservation.CaptureInput) (recoveryDocumentGroup, journalnamespace.RootGrant, error) {
	if err := validateRecoveryOwnershipWorkBudget(len(input.Bindings()), 0, 0); err != nil {
		return recoveryDocumentGroup{}, journalnamespace.RootGrant{}, err
	}
	grants, err := namespace.QualifyRoots(ctx, input.Budget())
	if err != nil {
		return recoveryDocumentGroup{}, journalnamespace.RootGrant{}, err
	}
	manifest, present := grants.Manifest()
	if !present {
		return recoveryDocumentGroup{}, journalnamespace.RootGrant{}, fmt.Errorf("native document capture requires the operation manifest root")
	}
	observed, err := persistedNativeObservation(namespace.Observation())
	if err != nil {
		return recoveryDocumentGroup{}, journalnamespace.RootGrant{}, err
	}
	group := recoveryDocumentGroup{
		CapturedTarget: observed, Writer: persistedNativeWriter(namespace.WriterPin().Provenance(), namespace.WriterPin().Relative()),
		SelectedSubjects: []persistedSubjectRef{},
		Aliases:          make([]recoveryDocumentAlias, 0, len(namespace.Aliases())),
		BindingIntents:   make([]recoveryDocumentBindingIntent, 0, len(input.Bindings())),
	}
	for _, alias := range namespace.Aliases() {
		if err := ctx.Err(); err != nil {
			return recoveryDocumentGroup{}, journalnamespace.RootGrant{}, err
		}
		role := "global-root"
		if alias.Address().Scope() == target.ScopeProject {
			role = "manifest-root"
		}
		selected, err := persistedNativeObservation(alias.Observation())
		if err != nil {
			return recoveryDocumentGroup{}, journalnamespace.RootGrant{}, err
		}
		group.Aliases = append(group.Aliases, recoveryDocumentAlias{
			Address:             persistedDocumentAlias(alias.Address()),
			LogicalRoot:         recoveryDocumentLogicalRoot{Role: role, Provenance: persistedNativeRoot(alias.Root().Provenance())},
			SelectedObservation: selected, ObservedBefore: observed,
		})
	}
	group.ExclusiveClaims, err = nativeDocumentGroupClaims(namespace, input)
	if err != nil {
		return recoveryDocumentGroup{}, journalnamespace.RootGrant{}, err
	}
	for _, intent := range input.Bindings() {
		if err := ctx.Err(); err != nil {
			return recoveryDocumentGroup{}, journalnamespace.RootGrant{}, err
		}
		record, err := persistedDocumentBindingIntent(intent)
		if err != nil {
			return recoveryDocumentGroup{}, journalnamespace.RootGrant{}, err
		}
		group.BindingIntents = append(group.BindingIntents, record)
	}
	if _, err := group.canonicalBindingIntents(ctx, input.Owner()); err != nil {
		return recoveryDocumentGroup{}, journalnamespace.RootGrant{}, err
	}
	return group, manifest, nil
}

func nativeDocumentGroupClaims(namespace journalnamespace.DocumentNamespace, input journalobservation.CaptureInput) (recoveryDocumentClaimSet, error) {
	if input.Claims() != nil {
		if len(input.Provisional()) != 0 {
			return recoveryDocumentClaimSet{}, fmt.Errorf("native document capture cannot mix exact and provisional claims")
		}
		record, err := persistedDocumentClaimTransition(*input.Claims(), input.OperationID())
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		observed, err := persistedNativeObservation(namespace.Observation())
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		if _, err := record.canonicalObservedExact(input.Owner(), input.OperationID(), observed); err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		return record, nil
	}
	if len(input.Provisional()) != 0 {
		provisional, pending := namespace.Observation().Provisional()
		if !pending {
			return recoveryDocumentClaimSet{}, fmt.Errorf("provisional document claims require a provisional captured namespace")
		}
		record, err := persistedDocumentProvisionalClaims(input.Provisional())
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		if _, err := record.canonicalProvisional(namespace.Addresses(), input.Owner(), input.OperationID(), provisional); err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		return record, nil
	}
	return recoveryDocumentClaimSet{Kind: "absent"}, nil
}
