package journal

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

type wholeDocumentCaptureInput struct {
	mutation    ManagedPathMutation
	payloads    payload.PayloadSet
	owner       stateauthority.Authority
	operationID string
	claims      *ownershipmutation.DocumentClaimTransition
	provisional []documentProvisionalAcquire
	bindings    []ownership.DocumentBindingIntent
	removal     *recovery.RemovalIntent
	groupIndex  int
	limits      configdocument.Limits
	budget      *recovery.PhysicalWorkBudget
}

// prepareWholeDocumentGroup captures persistence facts, not a publication seal.
// The containing operation must admit complete snapshots and all group relations.
func prepareWholeDocumentGroup(ctx context.Context, namespace NativeDocumentNamespace, input wholeDocumentCaptureInput) (recoveryDocumentGroup, DocumentComparison, error) {
	if ctx == nil || input.budget == nil || input.groupIndex < 0 {
		return recoveryDocumentGroup{}, DocumentComparison{}, fmt.Errorf("whole document capture requires context, budget and a nonnegative ordinal")
	}
	if err := ctx.Err(); err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	if err := input.owner.Validate(); err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	if err := ownership.ValidateOperationID(input.operationID); err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	if err := input.limits.Validate(); err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	facts, err := wholeDocumentMutationFacts(input.mutation)
	if err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	address, err := aggregate.NewDocumentAddress(facts.consumerTargets[0], facts.scope, facts.destination)
	if err != nil || !slices.Contains(namespace.Addresses(), address) {
		return recoveryDocumentGroup{}, DocumentComparison{}, fmt.Errorf("whole document mutation has no captured namespace alias")
	}
	group, manifest, err := wholeDocumentGroupMetadata(ctx, namespace, input)
	if err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}

	original, mode, _, err := readNativeRecoveryDocument(ctx, namespace, min(input.limits.MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), input.budget)
	if err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	comparison, err := PrepareWholeDocument(ctx, WholeDocumentPreparationInput{Before: original, BeforeMode: mode, Limits: input.limits}, input.mutation, input.payloads)
	if err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	_, group.Original, group.Expected, err = documentImageReferences(comparison, input.groupIndex)
	if err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	if err := group.validate(manifest); err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	if err := ctx.Err(); err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	return group, comparison, nil
}

func wholeDocumentGroupMetadata(ctx context.Context, namespace NativeDocumentNamespace, input wholeDocumentCaptureInput) (recoveryDocumentGroup, recoveryRootProvenance, error) {
	if err := validateRecoveryOwnershipWorkBudget(len(input.bindings), 0, 0); err != nil {
		return recoveryDocumentGroup{}, recoveryRootProvenance{}, err
	}
	grants, err := namespace.roots.qualify(ctx, input.budget)
	if err != nil {
		return recoveryDocumentGroup{}, recoveryRootProvenance{}, err
	}
	manifest, present := grants[output.RootProject]
	if !present {
		return recoveryDocumentGroup{}, recoveryRootProvenance{}, fmt.Errorf("whole document capture requires the operation manifest root")
	}
	observed, err := persistedNativeObservation(namespace.observation)
	if err != nil {
		return recoveryDocumentGroup{}, recoveryRootProvenance{}, err
	}
	group := recoveryDocumentGroup{
		CapturedTarget: observed, Writer: namespace.writerRecord, Extent: "whole",
		SelectedSubjects: []persistedSubjectRef{},
		Aliases:          make([]recoveryDocumentAlias, 0, len(namespace.aliases)),
		BindingIntents:   make([]recoveryDocumentBindingIntent, 0, len(input.bindings)),
	}
	for _, alias := range namespace.aliases {
		if err := ctx.Err(); err != nil {
			return recoveryDocumentGroup{}, recoveryRootProvenance{}, err
		}
		role := "global-root"
		if alias.address.Scope() == target.ScopeProject {
			role = "manifest-root"
		}
		selected, err := persistedNativeObservation(alias.observation)
		if err != nil {
			return recoveryDocumentGroup{}, recoveryRootProvenance{}, err
		}
		group.Aliases = append(group.Aliases, recoveryDocumentAlias{
			Address:             persistedDocumentAlias(alias.address),
			LogicalRoot:         recoveryDocumentLogicalRoot{Role: role, Provenance: persistedNativeRoot(alias.root.provenance)},
			SelectedObservation: selected, ObservedBefore: observed,
		})
	}
	group.ExclusiveClaims, err = wholeDocumentGroupClaims(namespace, input)
	if err != nil {
		return recoveryDocumentGroup{}, recoveryRootProvenance{}, err
	}
	for _, intent := range input.bindings {
		if err := ctx.Err(); err != nil {
			return recoveryDocumentGroup{}, recoveryRootProvenance{}, err
		}
		record, err := persistedDocumentBindingIntent(intent)
		if err != nil {
			return recoveryDocumentGroup{}, recoveryRootProvenance{}, err
		}
		group.BindingIntents = append(group.BindingIntents, record)
	}
	if _, err := group.canonicalBindingIntents(ctx, input.owner); err != nil {
		return recoveryDocumentGroup{}, recoveryRootProvenance{}, err
	}
	if input.removal != nil {
		if input.removal.Scope() != input.mutation.facts().scope || input.removal.Destination() != input.mutation.facts().destination {
			return recoveryDocumentGroup{}, recoveryRootProvenance{}, fmt.Errorf("whole document removal differs from its mutation")
		}
		records, err := persistedRecoveryRemovalIntents([]recovery.RemovalIntent{*input.removal})
		if err != nil {
			return recoveryDocumentGroup{}, recoveryRootProvenance{}, err
		}
		group.Removal = &records[0]
	}
	return group, persistedNativeRoot(manifest.provenance), nil
}

func wholeDocumentGroupClaims(namespace NativeDocumentNamespace, input wholeDocumentCaptureInput) (recoveryDocumentClaimSet, error) {
	if input.claims != nil {
		if len(input.provisional) != 0 {
			return recoveryDocumentClaimSet{}, fmt.Errorf("whole document capture cannot mix exact and provisional claims")
		}
		exact, known := namespace.observation.Exact()
		if !known {
			return recoveryDocumentClaimSet{}, fmt.Errorf("exact document claims require an exact captured namespace")
		}
		record, err := persistedDocumentClaimTransition(*input.claims, input.operationID)
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		if _, err := record.canonicalExact(input.owner, input.operationID, exact); err != nil {
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
