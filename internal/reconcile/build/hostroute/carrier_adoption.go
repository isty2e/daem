package hostroute

import (
	"fmt"
	"sort"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/target"
)

// CarrierAdoptionInput contains the current pure facts needed to classify
// explicit external-carrier adoption. StoreAvailable means both selected-scope
// claim stores required by the assessment were loaded successfully.
type CarrierAdoptionInput struct {
	Locked          lock.File
	SelectedTargets reconcile.SelectedTargets
	Observations    relationobserve.Batch
	CurrentOwner    stateauthority.Authority
	AllClaims       []durablecarrier.ManagedCarrierClaim
	ManageExisting  bool
	StoreAvailable  bool
}

// BuildCarrierAdoptionActions plans one immutable state-only adoption decision
// for every selected locked carrier relation. It performs no host or store I/O.
func BuildCarrierAdoptionActions(
	input CarrierAdoptionInput,
) ([]carrieradoption.Action, error) {
	if err := input.CurrentOwner.Validate(); err != nil {
		return nil, fmt.Errorf("carrier adoption current owner: %w", err)
	}
	for index, claim := range input.AllClaims {
		if err := claim.Validate(); err != nil {
			return nil, fmt.Errorf("carrier adoption claim[%d]: %w", index, err)
		}
	}
	records, err := selectedCarrierRelationRecords(input.Locked, input.SelectedTargets)
	if err != nil {
		return nil, err
	}
	actions := make([]carrieradoption.Action, 0, len(records))
	for _, item := range records {
		correlation, err := carrierAdoptionCorrelation(
			item.contract,
			item.relation,
			input.Observations,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"plan carrier adoption %q observation: %w",
				item.contract.SubjectID().Key(),
				err,
			)
		}
		identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(item.contract)
		if err != nil {
			return nil, fmt.Errorf("plan carrier adoption %q identity: %w", item.contract.SubjectID().Key(), err)
		}
		if !admitted {
			return nil, fmt.Errorf(
				"plan carrier adoption %q requires admitted carrier identity",
				item.contract.SubjectID().Key(),
			)
		}
		acquisition, err := lock.DelegatedOperationRequest(item.contract, lock.OperationInstall)
		if err != nil {
			return nil, fmt.Errorf(
				"plan carrier adoption %q acquisition request: %w",
				item.contract.SubjectID().Key(),
				err,
			)
		}
		removal, err := resolveCurrentCarrierRemovalRoute(identity, acquisition)
		if err != nil {
			return nil, fmt.Errorf(
				"plan carrier adoption %q removal lifecycle: %w",
				item.contract.SubjectID().Key(),
				err,
			)
		}
		store, err := carrierAdoptionClaimStore(identity.Scope())
		if err != nil {
			return nil, fmt.Errorf("plan carrier adoption %q: %w", item.contract.SubjectID().Key(), err)
		}
		installAdmission, err := carrierRelationInstallAdmission(item.contract)
		if err != nil {
			return nil, fmt.Errorf(
				"plan carrier adoption %q install admission: %w",
				item.contract.SubjectID().Key(),
				err,
			)
		}
		installRoute := carrieradoption.InstallRouteRefused
		if installAdmission.AllowsHostRouteInvocation() {
			installRoute = carrieradoption.InstallRouteAdmitted
		}
		lifecycle, err := carrieradoption.NewLifecycle(carrieradoption.LifecycleInput{
			Locked:         item.contract,
			InstallRoute:   installRoute,
			RemovalRoute:   removal,
			ClaimStore:     store,
			StoreAvailable: input.StoreAvailable,
		})
		if err != nil {
			return nil, fmt.Errorf(
				"plan carrier adoption %q lifecycle: %w",
				item.contract.SubjectID().Key(),
				err,
			)
		}
		action, err := carrieradoption.NewAction(carrieradoption.ActionInput{
			Locked:         item.contract,
			Observation:    correlation,
			CurrentOwner:   input.CurrentOwner,
			Claims:         input.AllClaims,
			Lifecycle:      lifecycle,
			ManageExisting: input.ManageExisting,
		})
		if err != nil {
			return nil, fmt.Errorf(
				"plan carrier adoption %q: %w",
				item.contract.SubjectID().Key(),
				err,
			)
		}
		actions = append(actions, action)
	}
	sort.Slice(actions, func(left int, right int) bool {
		return actions[left].Compare(actions[right]) < 0
	})
	return actions, nil
}

func carrierAdoptionCorrelation(
	locked lock.LockedSubjectContract,
	relation realization.DelegatedRelation,
	observations relationobserve.Batch,
) (relationobserve.CorrelationResult, error) {
	key, err := relationobserve.NewCorrelationKey(
		locked.SubjectID(),
		relation.ExpectedRelation(),
	)
	if err != nil {
		return relationobserve.CorrelationResult{}, err
	}
	if correlation, present := observations.Correlation(key); present {
		return correlation, nil
	}
	return relationobserve.Correlate(
		relation.ExpectedRelation(),
		relationobserve.UnsupportedInventory(),
	), nil
}

func carrierAdoptionClaimStore(scope target.Scope) (carrieradoption.ClaimStore, error) {
	switch scope {
	case target.ScopeProject:
		return carrieradoption.ClaimStoreProjectStatefile, nil
	case target.ScopeGlobal:
		return carrieradoption.ClaimStoreGlobalRegistry, nil
	default:
		return "", fmt.Errorf("carrier adoption scope %q has no claim store", scope)
	}
}
