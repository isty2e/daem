package carrierabsence

import (
	"strings"
	"testing"

	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
)

func TestRouteAdmissionEqualityIncludesDisclosuresAndPreconditions(t *testing.T) {
	t.Run("removed effects", func(t *testing.T) {
		left := testRouteAdmission(t, nil, []string{"managed_relation"})
		right := testRouteAdmission(t, nil, []string{"selected_carrier_artifacts"})
		if left.equal(right) {
			t.Fatal("route admissions with different removed effects compared equal")
		}
	})

	t.Run("operation preconditions", func(t *testing.T) {
		left := testRouteAdmission(t, nil, []string{"managed_relation"})
		right := testRouteAdmission(t, []string{"host_ready"}, []string{"managed_relation"})
		if left.equal(right) {
			t.Fatal("route admissions with different operation preconditions compared equal")
		}
	})
}

func testRouteAdmission(
	t *testing.T,
	preconditions []string,
	removedEffects []string,
) RouteAdmission {
	t.Helper()
	operation, err := lock.NewOperationContract(lock.OperationContractInput{
		Operation:     lock.OperationRemove,
		Actuation:     lock.ActuationDelegatedHostRoute,
		Authority:     lock.AuthorityRemove,
		Preconditions: preconditions,
		Route: lock.RouteContractRef{
			RouteID:                "test.remove",
			AdapterContractVersion: "test-v1",
		},
		EffectEnvelope:  lock.EffectEnvelopeComplete,
		Idempotency:     lock.ConditionallyIdempotent,
		Verification:    lock.VerificationHostRelation,
		TrustActivation: lock.TrustActivationNotRequired,
		Recovery:        lock.OperationRecoverySafeRetry,
	})
	if err != nil {
		t.Fatalf("NewOperationContract: %v", err)
	}
	request, err := realizationdelegate.NewRequest(
		"test.remove",
		"test-v1",
		"sha256:"+strings.Repeat("0", 64),
	)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	admission, err := NewRouteAdmission(RouteAdmissionInput{
		Operation:      operation,
		Request:        request,
		RemovedEffects: removedEffects,
	})
	if err != nil {
		t.Fatalf("NewRouteAdmission: %v", err)
	}
	return admission
}
