package journal

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

type wholeDocumentCaptureInput struct {
	nativeDocumentCaptureInput
	mutation ManagedPathMutation
	payloads payload.PayloadSet
}

type nativeDocumentCaptureInput struct {
	owner       stateauthority.Authority
	operationID string
	claims      *ownershipmutation.DocumentClaimTransition
	provisional []documentProvisionalAcquire
	bindings    []ownership.DocumentBindingIntent
	groupIndex  int
	limits      configdocument.Limits
	budget      *physicalwork.Budget
}

// prepareWholeDocumentGroup captures persistence facts, not a publication seal.
// The containing operation must admit complete snapshots and all group relations.
func prepareWholeDocumentGroup(ctx context.Context, namespace NativeDocumentNamespace, input wholeDocumentCaptureInput) (recoveryDocumentGroup, DocumentComparison, error) {
	candidate, err := prepareWholeDocumentCandidate(ctx, namespace, input)
	if err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	return candidate.captureRemoval(ctx, input.budget)
}

func prepareWholeDocumentCandidate(ctx context.Context, namespace NativeDocumentNamespace, input wholeDocumentCaptureInput) (nativeDocumentCaptureCandidate, error) {
	if err := validateNativeDocumentCaptureInput(ctx, input.nativeDocumentCaptureInput); err != nil {
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
	group, manifest, err := nativeDocumentGroupMetadata(ctx, namespace, input.nativeDocumentCaptureInput)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}

	original, mode, _, err := readNativeRecoveryDocument(ctx, namespace, min(input.limits.MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), input.budget)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	comparison, err := PrepareWholeDocument(ctx, WholeDocumentPreparationInput{Before: original, BeforeMode: mode, Limits: input.limits}, input.mutation, input.payloads)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	_, group.Original, group.Expected, err = documentImageReferences(comparison, input.groupIndex)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	group.Extent = "whole"
	if err := ctx.Err(); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	return nativeDocumentCaptureCandidate{group: group, comparison: comparison, namespace: namespace, manifest: manifest, relations: []aggregate.DocumentAddress{address}, budget: input.budget}, nil
}

func prepareRetainedWholeDocumentCandidate(ctx context.Context, namespace NativeDocumentNamespace, input nativeDocumentCaptureInput) (nativeDocumentCaptureCandidate, error) {
	if err := validateNativeDocumentCaptureInput(ctx, input); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	group, manifest, err := nativeDocumentGroupMetadata(ctx, namespace, input)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	original, mode, _, err := readNativeRecoveryDocument(ctx, namespace, min(input.limits.MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), input.budget)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	comparison, err := NewWholeDocumentComparison(ctx, DocumentComparisonInput{
		Before: original, Expected: original, BeforeMode: mode, ExpectedMode: mode, Limits: input.limits,
	})
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	_, group.Original, group.Expected, err = documentImageReferences(comparison, input.groupIndex)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	group.Extent = "whole"
	return nativeDocumentCaptureCandidate{group: group, comparison: comparison, namespace: namespace, manifest: manifest, relations: namespace.Addresses(), budget: input.budget}, nil
}

func validateNativeDocumentCaptureInput(ctx context.Context, input nativeDocumentCaptureInput) error {
	if ctx == nil || input.budget == nil || input.groupIndex < 0 {
		return fmt.Errorf("native document capture requires context, budget and a nonnegative ordinal")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := input.owner.Validate(); err != nil {
		return err
	}
	if err := ownership.ValidateOperationID(input.operationID); err != nil {
		return err
	}
	return input.limits.Validate()
}

func nativeDocumentGroupMetadata(ctx context.Context, namespace NativeDocumentNamespace, input nativeDocumentCaptureInput) (recoveryDocumentGroup, nativeDocumentRootGrant, error) {
	if err := validateRecoveryOwnershipWorkBudget(len(input.bindings), 0, 0); err != nil {
		return recoveryDocumentGroup{}, nativeDocumentRootGrant{}, err
	}
	grants, err := namespace.roots.qualify(ctx, input.budget)
	if err != nil {
		return recoveryDocumentGroup{}, nativeDocumentRootGrant{}, err
	}
	manifest, present := grants[output.RootProject]
	if !present {
		return recoveryDocumentGroup{}, nativeDocumentRootGrant{}, fmt.Errorf("native document capture requires the operation manifest root")
	}
	observed, err := persistedNativeObservation(namespace.observation)
	if err != nil {
		return recoveryDocumentGroup{}, nativeDocumentRootGrant{}, err
	}
	group := recoveryDocumentGroup{
		CapturedTarget: observed, Writer: namespace.writerRecord,
		SelectedSubjects: []persistedSubjectRef{},
		Aliases:          make([]recoveryDocumentAlias, 0, len(namespace.aliases)),
		BindingIntents:   make([]recoveryDocumentBindingIntent, 0, len(input.bindings)),
	}
	for _, alias := range namespace.aliases {
		if err := ctx.Err(); err != nil {
			return recoveryDocumentGroup{}, nativeDocumentRootGrant{}, err
		}
		role := "global-root"
		if alias.address.Scope() == target.ScopeProject {
			role = "manifest-root"
		}
		selected, err := persistedNativeObservation(alias.observation)
		if err != nil {
			return recoveryDocumentGroup{}, nativeDocumentRootGrant{}, err
		}
		group.Aliases = append(group.Aliases, recoveryDocumentAlias{
			Address:             persistedDocumentAlias(alias.address),
			LogicalRoot:         recoveryDocumentLogicalRoot{Role: role, Provenance: persistedNativeRoot(alias.root.provenance)},
			SelectedObservation: selected, ObservedBefore: observed,
		})
	}
	group.ExclusiveClaims, err = nativeDocumentGroupClaims(namespace, input)
	if err != nil {
		return recoveryDocumentGroup{}, nativeDocumentRootGrant{}, err
	}
	for _, intent := range input.bindings {
		if err := ctx.Err(); err != nil {
			return recoveryDocumentGroup{}, nativeDocumentRootGrant{}, err
		}
		record, err := persistedDocumentBindingIntent(intent)
		if err != nil {
			return recoveryDocumentGroup{}, nativeDocumentRootGrant{}, err
		}
		group.BindingIntents = append(group.BindingIntents, record)
	}
	if _, err := group.canonicalBindingIntents(ctx, input.owner); err != nil {
		return recoveryDocumentGroup{}, nativeDocumentRootGrant{}, err
	}
	return group, manifest, nil
}

func nativeDocumentGroupClaims(namespace NativeDocumentNamespace, input nativeDocumentCaptureInput) (recoveryDocumentClaimSet, error) {
	if input.claims != nil {
		if len(input.provisional) != 0 {
			return recoveryDocumentClaimSet{}, fmt.Errorf("native document capture cannot mix exact and provisional claims")
		}
		record, err := persistedDocumentClaimTransition(*input.claims, input.operationID)
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		observed, err := persistedNativeObservation(namespace.observation)
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		if _, err := record.canonicalObservedExact(input.owner, input.operationID, observed); err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		return record, nil
	}
	if len(input.provisional) != 0 {
		provisional, pending := namespace.observation.Provisional()
		if !pending {
			return recoveryDocumentClaimSet{}, fmt.Errorf("provisional document claims require a provisional captured namespace")
		}
		record, err := persistedDocumentProvisionalClaims(input.provisional)
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		if _, err := record.canonicalProvisional(namespace.Addresses(), input.owner, input.operationID, provisional); err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		return record, nil
	}
	return recoveryDocumentClaimSet{Kind: "absent"}, nil
}
