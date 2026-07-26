package lockfile

import (
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/realization/lock"
)

func operationContractsFromDTO(operations []operationContractDTO) ([]lock.OperationContract, error) {
	if operations == nil {
		return nil, nil
	}
	converted := make([]lock.OperationContract, 0, len(operations))
	for _, operation := range operations {
		contract, err := lock.NewOperationContract(lock.OperationContractInput{
			Operation: lock.OperationKind(operation.Operation),
			Actuation: lock.ActuationKind(operation.Actuation),
			Authority: lock.AuthorityKind(operation.Authority),
			Route: lock.RouteContractRef{
				RouteID:                operation.RouteID,
				AdapterContractVersion: operation.RouteAdapterContractVersion,
			},
			HostCompatibility: lock.HostCompatibilityConstraint{
				HostVersionConstraint: operation.HostVersionConstraint,
				ConfigFormatVersion:   operation.ConfigFormatVersion,
			},
			Preconditions:        operation.Preconditions,
			EffectEnvelope:       lock.EffectEnvelopeClass(operation.EffectEnvelope),
			EffectPostconditions: effectPostconditionRequirementsFromStrings(operation.EffectPostconditions),
			Idempotency:          lock.IdempotencyContract(operation.Idempotency),
			Verification:         lock.VerificationContract(operation.Verification),
			TrustActivation:      lock.TrustActivationRequirement(operation.TrustActivation),
			Recovery:             lock.OperationRecoveryClass(operation.Recovery),
		})
		if err != nil {
			return nil, err
		}
		converted = append(converted, contract)
	}
	return converted, nil
}

func operationContractsToDTO(contract lock.LockedSubjectContract) []operationContractDTO {
	kinds := contract.OperationKinds()
	if len(kinds) == 0 {
		return nil
	}
	converted := make([]operationContractDTO, 0, len(kinds))
	for _, kind := range kinds {
		operationContract, ok := contract.OperationContract(kind)
		if !ok {
			continue
		}
		route := operationContract.Route()
		hostCompatibility := operationContract.HostCompatibility()
		converted = append(converted, operationContractDTO{
			Operation:                   string(operationContract.Operation()),
			Actuation:                   string(operationContract.Actuation()),
			Authority:                   string(operationContract.Authority()),
			RouteID:                     route.RouteID,
			RouteAdapterContractVersion: route.AdapterContractVersion,
			HostVersionConstraint:       hostCompatibility.HostVersionConstraint,
			ConfigFormatVersion:         hostCompatibility.ConfigFormatVersion,
			Preconditions:               operationContract.Preconditions(),
			EffectEnvelope:              string(operationContract.EffectEnvelope()),
			EffectPostconditions:        effectPostconditionRequirementStrings(operationContract.EffectPostconditions()),
			Idempotency:                 string(operationContract.Idempotency()),
			Verification:                string(operationContract.Verification()),
			TrustActivation:             string(operationContract.TrustActivation()),
			Recovery:                    string(operationContract.Recovery()),
		})
	}
	return converted
}

func effectPostconditionRequirementsFromStrings(
	values []string,
) []effectpostcondition.Requirement {
	requirements := make([]effectpostcondition.Requirement, len(values))
	for index, value := range values {
		requirements[index] = effectpostcondition.Requirement(value)
	}
	return requirements
}

func effectPostconditionRequirementStrings(
	set effectpostcondition.Set,
) []string {
	requirements := set.Requirements()
	if len(requirements) == 0 {
		return nil
	}
	values := make([]string, len(requirements))
	for index, requirement := range requirements {
		values[index] = string(requirement)
	}
	return values
}
