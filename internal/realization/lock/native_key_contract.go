package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// NewHostConfigKeySubjectContract binds one source-derived atomic contribution
// to its declaration and static native placement. Source custody is separate.
func NewHostConfigKeySubjectContract(id entity.ID, subject topology.SubjectID, contribution aggregate.ManagedContribution, placement configuration.Placement) (LockedSubjectContract, error) {
	declared, key, native := topologyprojection.HostConfigKeyIdentity(subject)
	if id.Kind() != entity.KindHostConfig || !native || declared != id {
		return LockedSubjectContract{}, fmt.Errorf("native key subject does not match its declaration")
	}
	if _, err := aggregate.NewSubjectContribution(subject, contribution); err != nil {
		return LockedSubjectContract{}, err
	}
	expected, err := profile.HostConfigProjectionContract(placement, contribution.Footprint())
	if err != nil {
		return LockedSubjectContract{}, err
	}
	actualKey, selected := contribution.Footprint().Key()
	if !selected || actualKey != key || !expected.Equal(contribution.Contract()) {
		return LockedSubjectContract{}, fmt.Errorf("native key contribution does not match its placement and subject")
	}
	spec, err := realization.NewManagedAggregateContribution(contribution)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	write, err := profile.HostConfigOperationRoute(placement, profile.OperationWrite)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	remove, err := profile.HostConfigOperationRoute(placement, profile.OperationRemove)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	operations, err := directProjectionOperationContracts(write.RouteID(), remove.RouteID(), string(contribution.CodecContractID()))
	if err != nil {
		return LockedSubjectContract{}, err
	}
	observe, err := NewOperationContract(OperationContractInput{
		Operation: OperationObserve, Actuation: ActuationNoMutation, Authority: AuthorityObserve,
		EffectEnvelope: EffectEnvelopeNotApplicable, Idempotency: IdempotencyNotApplicable,
		Verification: VerificationExactProjection, TrustActivation: TrustActivationNotRequired,
		Recovery: OperationRecoveryNotApplicable,
	})
	if err != nil {
		return LockedSubjectContract{}, err
	}
	replay, err := NewReplayCoverage(ReplayExact, ReplayExact, ReplayNotApplicable, nil)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	return NewLockedSubjectContract(LockedSubjectContractInput{
		EntityID: id, SubjectID: subject, Realization: &spec,
		Ownership: OwnershipManifest, OnAbsent: OnAbsentApply, Replay: replay,
		OperationContracts: append(operations, observe),
	})
}

func validateNativeKeySubject(contract LockedSubjectContract, contribution aggregate.ManagedContribution) error {
	placement, err := profile.HostConfigPlacementForContract(contribution.Contract())
	if err != nil {
		return err
	}
	expected, err := NewHostConfigKeySubjectContract(contract.EntityID(), contract.SubjectID(), contribution, placement)
	if err != nil {
		return err
	}
	if !contract.Equal(expected) {
		return fmt.Errorf("native key subject is not its canonical operation contract")
	}
	return nil
}
