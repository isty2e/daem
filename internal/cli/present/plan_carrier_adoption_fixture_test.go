package clipresent

import (
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
)

type presentCarrierAdoptionFixture struct {
	contract  lock.LockedSubjectContract
	owner     stateauthority.Authority
	exact     observerelation.CorrelationResult
	lifecycle carrieradoption.Lifecycle
}

func (fixture presentCarrierAdoptionFixture) action(
	t *testing.T,
	manageExisting bool,
	lifecycle carrieradoption.Lifecycle,
	claims []durablecarrier.ManagedCarrierClaim,
) carrieradoption.Action {
	t.Helper()
	return fixture.actionWithObservation(
		t,
		fixture.exact,
		manageExisting,
		lifecycle,
		claims,
	)
}

func (fixture presentCarrierAdoptionFixture) actionWithObservation(
	t *testing.T,
	observation observerelation.CorrelationResult,
	manageExisting bool,
	lifecycle carrieradoption.Lifecycle,
	claims []durablecarrier.ManagedCarrierClaim,
) carrieradoption.Action {
	t.Helper()
	action, err := carrieradoption.NewAction(carrieradoption.ActionInput{
		Locked:         fixture.contract,
		Observation:    observation,
		CurrentOwner:   fixture.owner,
		Claims:         claims,
		Lifecycle:      lifecycle,
		ManageExisting: manageExisting,
	})
	if err != nil {
		t.Fatalf("carrieradoption.NewAction: %v", err)
	}
	return action
}

func (fixture presentCarrierAdoptionFixture) claim(
	t *testing.T,
	owner stateauthority.Authority,
	provenance durablecarrier.ClaimProvenance,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	identity := presentManagedCarrierIdentity(t, fixture.contract)
	request, err := lock.DelegatedOperationRequest(fixture.contract, lock.OperationInstall)
	if err != nil {
		t.Fatalf("DelegatedOperationRequest: %v", err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(owner, identity, request, provenance)
	if err != nil {
		t.Fatalf("NewManagedCarrierClaim: %v", err)
	}
	return claim
}

func (fixture presentCarrierAdoptionFixture) lifecycleWithStoreAvailability(
	t *testing.T,
	available bool,
) carrieradoption.Lifecycle {
	t.Helper()
	lifecycle, err := carrieradoption.NewLifecycle(carrieradoption.LifecycleInput{
		Locked:         fixture.contract,
		InstallRoute:   carrieradoption.InstallRouteAdmitted,
		RemovalRoute:   fixture.lifecycle.RemovalRoute(),
		ClaimStore:     carrieradoption.ClaimStoreProjectStatefile,
		StoreAvailable: available,
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	return lifecycle
}
