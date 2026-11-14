// Package carrieradoption owns the closed, pure decision for acquiring one
// exact external managed-carrier claim. It does not observe hosts, persist
// claims, or execute lifecycle routes.
package carrieradoption

import (
	"fmt"
	"slices"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

// InstallRouteStatus records whether current route admission permits the
// locked acquisition route. It does not claim that host execution converges.
type InstallRouteStatus string

const (
	InstallRouteAdmitted InstallRouteStatus = "admitted"
	InstallRouteRefused  InstallRouteStatus = "refused"
)

// LifecycleBlocker identifies the first incomplete future-lifecycle gate.
type LifecycleBlocker string

const (
	LifecycleEligible                   LifecycleBlocker = ""
	BlockInstallRouteNotAdmitted        LifecycleBlocker = "install_route_not_admitted"
	BlockRemovalRouteUnavailable        LifecycleBlocker = "removal_route_unavailable"
	BlockSharedConsumerPolicyIncomplete LifecycleBlocker = "shared_consumer_policy_incomplete"
	BlockClaimStoreUnavailable          LifecycleBlocker = "claim_store_unavailable"
)

// LifecycleInput contains already-locked operation facts and the currently
// resolved removal route. StoreAvailable means the selected project statefile
// or global registry was read successfully for this assessment.
type LifecycleInput struct {
	Locked         lock.LockedSubjectContract
	InstallRoute   InstallRouteStatus
	RemovalRoute   carrierabsence.RouteAdmission
	StoreAvailable bool
}

// Lifecycle is the immutable install/removal/store basis inherited by a claim.
// An ineligible value remains a valid planner fact and carries one stable
// blocker rather than degrading to a constructor error.
type Lifecycle struct {
	locked         lock.LockedSubjectContract
	install        lock.OperationContract
	installRoute   InstallRouteStatus
	removal        carrierabsence.RouteAdmission
	storeAvailable bool
	blocker        LifecycleBlocker
}

// NewLifecycle validates and classifies the complete lifecycle basis in stable
// gate order.
func NewLifecycle(input LifecycleInput) (Lifecycle, error) {
	key, admitted, err := lock.DelegatedRelationCarrierKey(input.Locked)
	if err != nil {
		return Lifecycle{}, fmt.Errorf("carrier adoption lifecycle lock: %w", err)
	} else if !admitted {
		return Lifecycle{}, fmt.Errorf("carrier adoption lifecycle requires a locked carrier relation")
	}
	if err := input.RemovalRoute.Validate(); err != nil {
		return Lifecycle{}, fmt.Errorf("carrier adoption removal route: %w", err)
	}
	if err := validateInstallRouteStatus(input.InstallRoute); err != nil {
		return Lifecycle{}, err
	}
	if _, known := durablecarrier.ClaimStoreForScope(key.Scope()); !known {
		return Lifecycle{}, fmt.Errorf("carrier adoption scope %q has no claim store", key.Scope())
	}

	install, hasInstall := input.Locked.OperationContract(lock.OperationInstall)
	if !hasInstall {
		return Lifecycle{}, fmt.Errorf(
			"admitted carrier relation has no install operation contract",
		)
	}
	lifecycle := Lifecycle{
		locked:         input.Locked,
		install:        install,
		installRoute:   input.InstallRoute,
		removal:        input.RemovalRoute,
		storeAvailable: input.StoreAvailable,
	}
	lifecycle.blocker = classifyLifecycle(
		install,
		input.InstallRoute,
		input.RemovalRoute,
		input.StoreAvailable,
	)
	return lifecycle, nil
}

// Validate rejects forged or non-canonical lifecycle values.
func (lifecycle Lifecycle) Validate() error {
	rebuilt, err := NewLifecycle(LifecycleInput{
		Locked:         lifecycle.locked,
		InstallRoute:   lifecycle.installRoute,
		RemovalRoute:   lifecycle.removal,
		StoreAvailable: lifecycle.storeAvailable,
	})
	if err != nil {
		return err
	}
	if lifecycle.blocker != rebuilt.blocker ||
		lifecycle.installRoute != rebuilt.installRoute ||
		!equalOperationContract(lifecycle.install, rebuilt.install) {
		return fmt.Errorf("carrier adoption lifecycle does not match canonical classification")
	}
	return nil
}

// Eligible reports whether the exact relation may acquire full managed-carrier
// lifecycle authority.
func (lifecycle Lifecycle) Eligible() bool { return lifecycle.blocker == LifecycleEligible }

// Blocker returns the first incomplete lifecycle gate.
func (lifecycle Lifecycle) Blocker() LifecycleBlocker { return lifecycle.blocker }

// InstallOperation returns the locked acquisition operation inherited by the claim.
func (lifecycle Lifecycle) InstallOperation() lock.OperationContract { return lifecycle.install }

// InstallRouteStatus returns current acquisition-route admission.
func (lifecycle Lifecycle) InstallRouteStatus() InstallRouteStatus {
	return lifecycle.installRoute
}

// RemovalRoute returns the admitted future removal envelope.
func (lifecycle Lifecycle) RemovalRoute() carrierabsence.RouteAdmission {
	return lifecycle.removal
}

// ClaimStore returns the scope-selected durable claim store.
func (lifecycle Lifecycle) ClaimStore() durablecarrier.ClaimStore {
	store, _ := durablecarrier.ClaimStoreForScope(carrierScope(lifecycle.locked))
	return store
}

// StoreAvailable reports whether the selected store participated in current assessment.
func (lifecycle Lifecycle) StoreAvailable() bool { return lifecycle.storeAvailable }

func classifyLifecycle(
	install lock.OperationContract,
	installRoute InstallRouteStatus,
	removal carrierabsence.RouteAdmission,
	storeAvailable bool,
) LifecycleBlocker {
	if installRoute != InstallRouteAdmitted {
		return BlockInstallRouteNotAdmitted
	}
	if install.Operation() != lock.OperationInstall ||
		install.Actuation() != lock.ActuationDelegatedHostRoute ||
		install.Authority() != lock.AuthorityManage ||
		install.Route().RouteID == "" ||
		install.Route().AdapterContractVersion == "" {
		return BlockInstallRouteNotAdmitted
	}
	if removal.Status() != carrierabsence.RouteAdmitted {
		return BlockRemovalRouteUnavailable
	}
	remove := removal.Operation()
	if !removal.PreservesSharedCarrier() &&
		!containsString(remove.Preconditions(), "no_remaining_daem_known_consumers") {
		return BlockSharedConsumerPolicyIncomplete
	}
	if !storeAvailable {
		return BlockClaimStoreUnavailable
	}
	return LifecycleEligible
}

func validateInstallRouteStatus(status InstallRouteStatus) error {
	switch status {
	case InstallRouteAdmitted, InstallRouteRefused:
		return nil
	default:
		return fmt.Errorf("carrier adoption install route status %q is unsupported", status)
	}
}

func carrierScope(locked lock.LockedSubjectContract) target.Scope {
	realization, ok := locked.Realization()
	if !ok {
		return ""
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		return ""
	}
	return relation.Scope()
}

func containsString(values []string, expected string) bool {
	return slices.Contains(values, expected)
}

func equalOperationContract(left lock.OperationContract, right lock.OperationContract) bool {
	return left.Operation() == right.Operation() &&
		left.Actuation() == right.Actuation() &&
		left.Authority() == right.Authority() &&
		left.Route() == right.Route() &&
		left.HostCompatibility() == right.HostCompatibility() &&
		slices.Equal(left.Preconditions(), right.Preconditions()) &&
		left.EffectEnvelope() == right.EffectEnvelope() &&
		left.EffectPostconditions().Equal(right.EffectPostconditions()) &&
		left.Idempotency() == right.Idempotency() &&
		left.Verification() == right.Verification() &&
		left.TrustActivation() == right.TrustActivation() &&
		left.Recovery() == right.Recovery()
}
