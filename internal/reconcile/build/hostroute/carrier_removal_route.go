package hostroute

import (
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
)

// ResolveCurrentCarrierRemovalRoute derives current removal capability from one
// exact durable acquisition claim and the current carrier profile. It does not
// treat the claim as current host evidence or persist the derived remove route.
func ResolveCurrentCarrierRemovalRoute(
	claim durablecarrier.ManagedCarrierClaim,
) (carrierabsence.RouteAdmission, error) {
	if err := claim.Validate(); err != nil {
		return carrierabsence.RouteAdmission{}, fmt.Errorf("carrier removal claim: %w", err)
	}
	return resolveCurrentCarrierRemovalRoute(
		claim.Identity(),
		claim.InstallRequest(),
	)
}

func resolveCurrentCarrierRemovalRoute(
	identity durablecarrier.ManagedCarrierIdentity,
	installRequest realizationdelegate.Request,
) (carrierabsence.RouteAdmission, error) {
	if err := identity.Validate(); err != nil {
		return carrierabsence.RouteAdmission{}, fmt.Errorf("carrier removal identity: %w", err)
	}
	if err := installRequest.Validate(); err != nil {
		return carrierabsence.RouteAdmission{}, fmt.Errorf("carrier removal acquisition request: %w", err)
	}
	removal, admitted, err := lock.ResolveDelegatedCarrierRemoval(
		identity.Carrier().Key(),
		identity.RelationSubject(),
		identity.ExpectedRelation(),
		installRequest,
	)
	if err != nil {
		return carrierabsence.RouteAdmission{}, fmt.Errorf("resolve current carrier removal: %w", err)
	}
	if !admitted {
		return carrierabsence.UnavailableRoute(), nil
	}
	admission, err := carrierabsence.NewRouteAdmission(carrierabsence.RouteAdmissionInput{
		Operation:              removal.Operation(),
		Request:                removal.Request(),
		PreservesSharedCarrier: removal.PreservesSharedCarrier(),
		RemovedEffects:         removal.RemovedEffects(),
		RetainedEffects:        removal.RetainedEffects(),
		NonClaims:              removal.NonClaims(),
	})
	if err != nil {
		return carrierabsence.RouteAdmission{}, fmt.Errorf("construct current carrier removal route: %w", err)
	}
	return admission, nil
}
