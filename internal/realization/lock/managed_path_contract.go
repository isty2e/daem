package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/topology"
)

const (
	managedPathFreshEvidencePrecondition = "fresh_path_evidence"
	managedPathOwnershipPrecondition     = "ownership_authority"
	managedPathManagedStatePrecondition  = "managed_state_authority"
)

// ManagedPathSubjectInput carries one realized path and its profile-selected
// direct write/removal routes.
type ManagedPathSubjectInput struct {
	EntityID      entity.ID
	SubjectID     topology.SubjectID
	Realization   realization.RealizationSpec
	WriteRouteID  string
	RemoveRouteID string
}

// NewManagedPathSubjectContract constructs one direct managed-path lock contract.
func NewManagedPathSubjectContract(input ManagedPathSubjectInput) (LockedSubjectContract, error) {
	projection, ok := input.Realization.ManagedPathProjection()
	if !ok {
		return LockedSubjectContract{}, fmt.Errorf("managed path subject requires managed path realization")
	}
	if input.SubjectID.Kind() != topology.SubjectProjection {
		return LockedSubjectContract{}, fmt.Errorf("managed path subject requires projection SubjectID")
	}
	replay, err := NewReplayCoverage(ReplayExact, ReplayExact, ReplayNotApplicable, nil)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	operations, err := managedPathOperationContracts(
		input.WriteRouteID,
		input.RemoveRouteID,
		projection.AdapterContractVersion(),
	)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	return NewLockedSubjectContract(LockedSubjectContractInput{
		EntityID:           input.EntityID,
		SubjectID:          input.SubjectID,
		Realization:        &input.Realization,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentApply,
		Replay:             replay,
		OperationContracts: operations,
	})
}

func managedPathOperationContracts(
	writeRouteID string,
	removeRouteID string,
	adapterContractVersion string,
) ([]OperationContract, error) {
	write, err := NewOperationContract(OperationContractInput{
		Operation: OperationWriteProjection,
		Actuation: ActuationDirectProjection,
		Authority: AuthorityManage,
		Route: RouteContractRef{
			RouteID:                writeRouteID,
			AdapterContractVersion: adapterContractVersion,
		},
		Preconditions:   []string{managedPathFreshEvidencePrecondition, managedPathOwnershipPrecondition},
		EffectEnvelope:  EffectEnvelopeComplete,
		Idempotency:     ConditionallyIdempotent,
		Verification:    VerificationExactProjection,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoveryBoundedCompensation,
	})
	if err != nil {
		return nil, err
	}
	remove, err := NewOperationContract(OperationContractInput{
		Operation: OperationRemoveProjection,
		Actuation: ActuationDirectProjection,
		Authority: AuthorityRemove,
		Route: RouteContractRef{
			RouteID:                removeRouteID,
			AdapterContractVersion: adapterContractVersion,
		},
		Preconditions: []string{
			managedPathFreshEvidencePrecondition,
			managedPathManagedStatePrecondition,
			managedPathOwnershipPrecondition,
		},
		EffectEnvelope:  EffectEnvelopeComplete,
		Idempotency:     ConditionallyIdempotent,
		Verification:    VerificationExactProjection,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoveryBoundedCompensation,
	})
	if err != nil {
		return nil, err
	}
	return []OperationContract{write, remove}, nil
}
