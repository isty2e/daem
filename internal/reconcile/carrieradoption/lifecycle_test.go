package carrieradoption_test

import (
	"testing"

	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/target"
)

func TestCarrierAdoptionLifecycleClassifiesEveryGateInNormativeOrder(t *testing.T) {
	fixture := newAdoptionFixture(t, target.ScopeProject, "alpha@official")
	sharedUnsafe := adoptionRouteWithoutSharedConsumerGate(
		t,
		fixture.lifecycle.RemovalRoute(),
	)

	tests := []struct {
		name         string
		installRoute carrieradoption.InstallRouteStatus
		removal      carrierabsence.RouteAdmission
		store        carrieradoption.ClaimStore
		available    bool
		want         carrieradoption.LifecycleBlocker
	}{
		{
			name:         "route refusal precedes removal and store blockers",
			installRoute: carrieradoption.InstallRouteRefused,
			removal:      carrierabsence.UnavailableRoute(),
			store:        carrieradoption.ClaimStoreGlobalRegistry,
			want:         carrieradoption.BlockInstallRouteNotAdmitted,
		},
		{
			name:         "removal refusal precedes store blockers",
			installRoute: carrieradoption.InstallRouteAdmitted,
			removal:      carrierabsence.UnavailableRoute(),
			store:        carrieradoption.ClaimStoreGlobalRegistry,
			want:         carrieradoption.BlockRemovalRouteUnavailable,
		},
		{
			name:         "shared consumer gap precedes store blockers",
			installRoute: carrieradoption.InstallRouteAdmitted,
			removal:      sharedUnsafe,
			store:        carrieradoption.ClaimStoreGlobalRegistry,
			want:         carrieradoption.BlockSharedConsumerPolicyIncomplete,
		},
		{
			name:         "scope mismatch precedes store availability",
			installRoute: carrieradoption.InstallRouteAdmitted,
			removal:      fixture.lifecycle.RemovalRoute(),
			store:        carrieradoption.ClaimStoreGlobalRegistry,
			want:         carrieradoption.BlockClaimStoreScopeMismatch,
		},
		{
			name:         "unavailable selected store",
			installRoute: carrieradoption.InstallRouteAdmitted,
			removal:      fixture.lifecycle.RemovalRoute(),
			store:        carrieradoption.ClaimStoreProjectStatefile,
			want:         carrieradoption.BlockClaimStoreUnavailable,
		},
		{
			name:         "complete lifecycle",
			installRoute: carrieradoption.InstallRouteAdmitted,
			removal:      fixture.lifecycle.RemovalRoute(),
			store:        carrieradoption.ClaimStoreProjectStatefile,
			available:    true,
			want:         carrieradoption.LifecycleEligible,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle, err := carrieradoption.NewLifecycle(carrieradoption.LifecycleInput{
				Locked:         fixture.contract,
				InstallRoute:   test.installRoute,
				RemovalRoute:   test.removal,
				ClaimStore:     test.store,
				StoreAvailable: test.available,
			})
			if err != nil {
				t.Fatalf("NewLifecycle: %v", err)
			}
			if lifecycle.Blocker() != test.want {
				t.Fatalf("blocker = %q, want %q", lifecycle.Blocker(), test.want)
			}
			if lifecycle.Eligible() != (test.want == carrieradoption.LifecycleEligible) {
				t.Fatalf("eligible = %t for blocker %q", lifecycle.Eligible(), lifecycle.Blocker())
			}
			if err := lifecycle.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}

			if lifecycle.Eligible() {
				return
			}
			for _, manageExisting := range []bool{false, true} {
				action, err := carrieradoption.NewAction(carrieradoption.ActionInput{
					Locked:         fixture.contract,
					Observation:    fixture.exact,
					CurrentOwner:   fixture.owner,
					Lifecycle:      lifecycle,
					ManageExisting: manageExisting,
				})
				if err != nil {
					t.Fatalf("NewAction(manage-existing=%t): %v", manageExisting, err)
				}
				if action.Result() != carrieradoption.ResultPresentUnclaimedIneligible {
					t.Fatalf(
						"result(manage-existing=%t) = %q",
						manageExisting,
						action.Result(),
					)
				}
				if _, proposed := action.ProposedClaim(); proposed {
					t.Fatal("ineligible lifecycle proposed a claim")
				}
			}
		})
	}
}

func adoptionRouteWithoutSharedConsumerGate(
	t *testing.T,
	original carrierabsence.RouteAdmission,
) carrierabsence.RouteAdmission {
	t.Helper()
	operation := original.Operation()
	withoutGate, err := lock.NewOperationContract(lock.OperationContractInput{
		Operation:            operation.Operation(),
		Actuation:            operation.Actuation(),
		Authority:            operation.Authority(),
		Route:                operation.Route(),
		HostCompatibility:    operation.HostCompatibility(),
		EffectEnvelope:       operation.EffectEnvelope(),
		EffectPostconditions: operation.EffectPostconditions().Requirements(),
		Idempotency:          operation.Idempotency(),
		Verification:         operation.Verification(),
		TrustActivation:      operation.TrustActivation(),
		Recovery:             operation.Recovery(),
	})
	if err != nil {
		t.Fatalf("NewOperationContract: %v", err)
	}
	route, err := carrierabsence.NewRouteAdmission(carrierabsence.RouteAdmissionInput{
		Operation:              withoutGate,
		Request:                original.Request(),
		PreservesSharedCarrier: false,
		RemovedEffects:         original.RemovedEffects(),
		RetainedEffects:        original.RetainedEffects(),
		NonClaims:              original.NonClaims(),
	})
	if err != nil {
		t.Fatalf("NewRouteAdmission: %v", err)
	}
	return route
}
