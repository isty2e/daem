package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/topology"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

const (
	hookFreshEvidencePrecondition = "fresh_aggregate_evidence"
	hookOwnershipPrecondition     = "ownership_authority"
	hookManagedStatePrecondition  = "managed_state_authority"
)

// NewHookContributionSubjectContract constructs the canonical static lock
// contract for one already-lowered Hook contribution.
func NewHookContributionSubjectContract(
	entityID entity.ID,
	subject topology.SubjectID,
	contribution aggregate.ManagedContribution,
	placement aggregate.HookPlacement,
) (LockedSubjectContract, error) {
	realization, err := realization.NewManagedAggregateContribution(aggregate.ManagedContributionInput{
		PlacementID:           contribution.PlacementID(),
		Target:                contribution.Target(),
		Scope:                 contribution.Scope(),
		AggregateRoot:         contribution.AggregateRoot(),
		ContentPath:           contribution.ContentPath(),
		MergeUnit:             contribution.MergeUnit(),
		Cardinality:           contribution.Cardinality(),
		SiblingRetention:      contribution.SiblingRetention(),
		SiblingPreservation:   contribution.SiblingPreservation(),
		Equivalence:           contribution.Equivalence(),
		CanonicalContribution: contribution.CanonicalContribution(),
		CodecContractID:       contribution.CodecContractID(),
		ComparedFields:        contribution.ComparedFields(),
	})
	if err != nil {
		return LockedSubjectContract{}, err
	}
	replay, err := NewReplayCoverage(ReplayExact, ReplayExact, ReplayNotApplicable, nil)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	operations, err := hookOperationContracts(placement)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	return NewLockedSubjectContract(LockedSubjectContractInput{
		EntityID: entityID, SubjectID: subject, Realization: &realization,
		Ownership: OwnershipManifest, OnAbsent: OnAbsentApply, Replay: replay,
		OperationContracts: operations,
	})
}

func hookOperationContracts(placement aggregate.HookPlacement) ([]OperationContract, error) {
	writeRoute, err := hookRouteContract(placement, profile.OperationWrite)
	if err != nil {
		return nil, err
	}
	removeRoute, err := hookRouteContract(placement, profile.OperationRemove)
	if err != nil {
		return nil, err
	}
	observe, err := NewOperationContract(OperationContractInput{
		Operation: OperationObserve, Actuation: ActuationNoMutation, Authority: AuthorityObserve,
		EffectEnvelope: EffectEnvelopeNotApplicable, Idempotency: IdempotencyNotApplicable,
		Verification: VerificationExactProjection, TrustActivation: TrustActivationNotRequired,
		Recovery: OperationRecoveryNotApplicable,
	})
	if err != nil {
		return nil, err
	}
	write, err := NewOperationContract(OperationContractInput{
		Operation: OperationWriteProjection, Actuation: ActuationDirectProjection, Authority: AuthorityManage,
		Route:          writeRoute,
		Preconditions:  []string{hookFreshEvidencePrecondition, hookOwnershipPrecondition},
		EffectEnvelope: EffectEnvelopeComplete, Idempotency: ConditionallyIdempotent,
		Verification: VerificationExactProjection, TrustActivation: TrustActivationNotRequired,
		Recovery: OperationRecoveryAtomic,
	})
	if err != nil {
		return nil, err
	}
	remove, err := NewOperationContract(OperationContractInput{
		Operation: OperationRemoveProjection, Actuation: ActuationDirectProjection, Authority: AuthorityRemove,
		Route:          removeRoute,
		Preconditions:  []string{hookFreshEvidencePrecondition, hookManagedStatePrecondition, hookOwnershipPrecondition},
		EffectEnvelope: EffectEnvelopeComplete, Idempotency: ConditionallyIdempotent,
		Verification: VerificationExactProjection, TrustActivation: TrustActivationNotRequired,
		Recovery: OperationRecoveryAtomic,
	})
	if err != nil {
		return nil, err
	}
	return []OperationContract{observe, write, remove}, nil
}

func hookRouteContract(placement aggregate.HookPlacement, operation profile.Operation) (RouteContractRef, error) {
	route, ok := profile.Profile(placement.Target()).OperationRoute(entity.KindHook, string(placement.ID()), operation)
	if !ok {
		return RouteContractRef{}, fmt.Errorf("Hook placement %q has no unique %s route", placement.ID(), operation)
	}
	return RouteContractRef{
		RouteID:                route.RouteID(),
		AdapterContractVersion: route.AdapterContractVersion(),
	}, nil
}

func validateAdmittedHookProjection(contract LockedSubjectContract) (bool, error) {
	realization, realized := contract.Realization()
	if !realized {
		return false, nil
	}
	contribution, aggregateRealization := realization.ManagedAggregateContribution()
	if !aggregateRealization || contract.EntityID().Kind() != entity.KindHook {
		return false, nil
	}
	placement, admitted := aggregate.HookPlacementFor(contribution.Target(), contribution.Scope())
	if !admitted {
		return false, nil
	}
	placementMatches := string(placement.ID()) == contribution.PlacementID()
	codecMatches := placement.CodecContractID() == contribution.CodecContractID()
	if !placementMatches && !codecMatches {
		return false, nil
	}
	if !placementMatches || !codecMatches {
		return true, fmt.Errorf("Hook contribution placement does not match the static profile")
	}
	expectedSubject, err := topologyhook.ProjectionSubjectID(contract.EntityID(), contribution.Target(), contribution.Scope())
	if err != nil {
		return true, err
	}
	expectedContribution, err := placement.Contribution(contribution.CanonicalContribution())
	if err != nil {
		return true, err
	}
	expected, err := NewHookContributionSubjectContract(contract.EntityID(), expectedSubject, expectedContribution, placement)
	if err != nil {
		return true, err
	}
	if !contract.Equal(expected) {
		return true, fmt.Errorf("Hook contribution contract does not match canonical profile refinement")
	}
	return true, nil
}
