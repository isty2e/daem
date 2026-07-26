package lockfile

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/effectpostcondition"
)

func TestOperationContractDTOCarriesClosedEffectPostconditions(t *testing.T) {
	contracts, err := operationContractsFromDTO([]operationContractDTO{
		{
			Operation:                   "remove",
			Actuation:                   "delegated_host_route",
			Authority:                   "remove",
			RouteID:                     "pi.package-carrier.remove",
			RouteAdapterContractVersion: "pi-package-remove-v1",
			EffectEnvelope:              "complete",
			EffectPostconditions: []string{
				string(effectpostcondition.CarrierArtifactsAbsent),
			},
			Idempotency:     "conditionally_idempotent",
			Verification:    "host_relation",
			TrustActivation: "not_required",
			Recovery:        "safe_retry",
		},
	})
	if err != nil {
		t.Fatalf("operationContractsFromDTO returned error: %v", err)
	}
	if len(contracts) != 1 {
		t.Fatalf("contracts = %#v", contracts)
	}
	requirements := contracts[0].EffectPostconditions().Requirements()
	if len(requirements) != 1 ||
		requirements[0] != effectpostcondition.CarrierArtifactsAbsent {
		t.Fatalf("effect postconditions = %#v", requirements)
	}
	if got := effectPostconditionRequirementStrings(
		contracts[0].EffectPostconditions(),
	); len(got) != 1 || got[0] != string(effectpostcondition.CarrierArtifactsAbsent) {
		t.Fatalf("encoded effect postconditions = %#v", got)
	}
}

func TestOperationContractDTORejectsUnknownEffectPostcondition(t *testing.T) {
	_, err := operationContractsFromDTO([]operationContractDTO{
		{
			Operation:                   "remove",
			Actuation:                   "delegated_host_route",
			Authority:                   "remove",
			RouteID:                     "pi.package-carrier.remove",
			RouteAdapterContractVersion: "pi-package-remove-v1",
			EffectEnvelope:              "complete",
			EffectPostconditions:        []string{"host_private_path_absent"},
			Idempotency:                 "conditionally_idempotent",
			Verification:                "host_relation",
			TrustActivation:             "not_required",
			Recovery:                    "safe_retry",
		},
	})
	if err == nil {
		t.Fatal("operationContractsFromDTO succeeded")
	}
}
