package hostroute

import (
	"fmt"
	"sort"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
)

type carrierRelationRecord struct {
	contract lock.LockedSubjectContract
	relation realization.DelegatedRelation
}

// RelationInput separates current passive evidence from durable management
// facts. Neither pending installs nor claims may alter passive correlation.
type RelationInput struct {
	Locked          lock.File
	SelectedTargets reconciliation.SelectedTargets
	Observations    relationobserve.Batch
	CurrentOwner    durablecarrier.StateAuthority
	PendingInstalls []durablecarrier.PendingCarrierInstall
	ManagedClaims   []durablecarrier.ManagedCarrierClaim
}

// BuildRelationActions classifies selected locked delegated carrier
// relations using passive relation inventory only. It never invokes host routes.
func BuildRelationActions(input RelationInput) ([]reconciliation.RelationAction, error) {
	if err := validateRelationManagementFacts(input.PendingInstalls, input.ManagedClaims); err != nil {
		return nil, err
	}
	if err := input.CurrentOwner.Validate(); err != nil {
		return nil, fmt.Errorf("relation current owner: %w", err)
	}
	records, err := selectedCarrierRelationRecords(input.Locked, input.SelectedTargets)
	if err != nil {
		return nil, err
	}
	actions := make([]reconciliation.RelationAction, 0, len(records))
	for _, item := range records {
		admission, err := carrierRelationInstallAdmission(item.contract)
		if err != nil {
			return nil, fmt.Errorf("plan carrier relation %q: %w", item.contract.SubjectID().Key(), err)
		}
		correlation, err := carrierRelationCorrelation(
			item.contract.SubjectID(),
			item.relation,
			input.Observations,
			admission.ObservationPolicy(),
		)
		if err != nil {
			return nil, fmt.Errorf("plan carrier relation %q observation identity: %w", item.contract.SubjectID().Key(), err)
		}
		action, err := buildCarrierRelationAction(
			item,
			correlation,
			admission,
			matchesPendingInstall(item.contract, input.CurrentOwner, input.PendingInstalls),
			matchesManagedClaim(item.contract, input.CurrentOwner, input.ManagedClaims),
		)
		if err != nil {
			return nil, fmt.Errorf("plan carrier relation %q: %w", item.contract.SubjectID().Key(), err)
		}
		actions = append(actions, action)
	}
	sort.SliceStable(actions, func(left int, right int) bool {
		return actions[left].Compare(actions[right]) < 0
	})
	return actions, nil
}

func selectedCarrierRelationRecords(
	locked lock.File,
	selectedTargets reconciliation.SelectedTargets,
) ([]carrierRelationRecord, error) {
	records := make([]carrierRelationRecord, 0)
	for _, contract := range locked.Locked.Subjects() {
		_, ok, err := lock.DelegatedRelationCarrier(contract)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		realization, _ := contract.Realization()
		relation, _ := realization.DelegatedRelation()
		if !selectedTargets.Contains(relation.Target()) {
			continue
		}
		records = append(records, carrierRelationRecord{
			contract: contract,
			relation: relation,
		})
	}
	return records, nil
}

func carrierRelationCorrelation(
	lockedSubject topology.SubjectID,
	relation realization.DelegatedRelation,
	observations relationobserve.Batch,
	observationPolicy reconciliation.RelationObservationPolicy,
) (relationobserve.CorrelationResult, error) {
	if observationPolicy == reconciliation.ObservationAttemptWhenUnsupported {
		return relationobserve.Correlate(
			relation.ExpectedRelation(),
			relationobserve.UnsupportedInventory(),
		), nil
	}
	key, err := relationobserve.NewCorrelationKey(
		lockedSubject,
		relation.ExpectedRelation(),
	)
	if err != nil {
		return relationobserve.CorrelationResult{}, err
	}
	if correlation, ok := observations.Correlation(key); ok {
		return correlation, nil
	}
	return relationobserve.Correlate(
		relation.ExpectedRelation(),
		relationobserve.UnsupportedInventory(),
	), nil
}

func buildCarrierRelationAction(
	item carrierRelationRecord,
	correlation relationobserve.CorrelationResult,
	admission reconciliation.RelationRouteAdmissionDecision,
	pendingInstall bool,
	managedClaim bool,
) (reconciliation.RelationAction, error) {
	realization, ok := item.contract.Realization()
	if !ok {
		return reconciliation.RelationAction{}, fmt.Errorf("carrier relation plan requires delegated relation realization")
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		return reconciliation.RelationAction{}, fmt.Errorf("carrier relation plan requires delegated relation realization")
	}
	carrierIdentity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(item.contract)
	if err != nil {
		return reconciliation.RelationAction{}, err
	}
	if !admitted {
		return reconciliation.RelationAction{}, fmt.Errorf("carrier relation plan requires admitted carrier identity")
	}
	return reconciliation.NewRelationAction(reconciliation.RelationActionInput{
		CarrierIdentity:       carrierIdentity,
		RouteRequest:          relation.RouteRequest(),
		Correlation:           correlation,
		RouteAdmission:        admission,
		PendingInstallPresent: pendingInstall,
		ManagedClaimPresent:   managedClaim,
	})
}

func validateRelationManagementFacts(
	pending []durablecarrier.PendingCarrierInstall,
	claims []durablecarrier.ManagedCarrierClaim,
) error {
	for index, candidate := range pending {
		if err := candidate.Validate(); err != nil {
			return fmt.Errorf("relation pending install[%d]: %w", index, err)
		}
	}
	for index, candidate := range claims {
		if err := candidate.Validate(); err != nil {
			return fmt.Errorf("relation managed claim[%d]: %w", index, err)
		}
	}
	return nil
}

func matchesPendingInstall(
	contract lock.LockedSubjectContract,
	currentOwner durablecarrier.StateAuthority,
	pending []durablecarrier.PendingCarrierInstall,
) bool {
	for _, candidate := range pending {
		if candidate.Owner().Equal(currentOwner) &&
			candidate.MatchesLockedRecord(contract) {
			return true
		}
	}
	return false
}

func matchesManagedClaim(
	contract lock.LockedSubjectContract,
	currentOwner durablecarrier.StateAuthority,
	claims []durablecarrier.ManagedCarrierClaim,
) bool {
	for _, candidate := range claims {
		if candidate.Owner().Equal(currentOwner) &&
			candidate.MatchesLockedRecord(contract) {
			return true
		}
	}
	return false
}

func carrierRelationInstallAdmission(contract lock.LockedSubjectContract) (reconciliation.RelationRouteAdmissionDecision, error) {
	install, ok := contract.OperationContract(lock.OperationInstall)
	if !ok {
		return reconciliation.RelationRouteAdmissionDecision{}, fmt.Errorf("carrier relation plan requires install operation contract")
	}
	observe, hasObserve := contract.OperationContract(lock.OperationObserve)
	policy, err := carrierRelationObservationPolicy(install, observe, hasObserve)
	if err != nil {
		return reconciliation.RelationRouteAdmissionDecision{}, err
	}
	// This cites the selected RA-01 fact. BuildRelationActions only plans
	// relation facts; workflow/apply owns any later host invocation.
	return reconciliation.NewRelationRouteAdmissionDecision(reconciliation.RelationRouteAdmissionSpec{
		Row:               reconciliation.RouteAdmissionRowInstallCarrier,
		RequestedOutcome:  reconciliation.AdmissionOutcomeOrdinaryMutation,
		SelectedOutcome:   reconciliation.AdmissionOutcomeHostDelegated,
		ObservationPolicy: policy,
	})
}

func carrierRelationObservationPolicy(
	install lock.OperationContract,
	observe lock.OperationContract,
	hasObserve bool,
) (reconciliation.RelationObservationPolicy, error) {
	switch {
	case hasObserve &&
		observe.Verification() == lock.VerificationHostRelation &&
		install.Verification() == lock.VerificationHostRelation:
		return reconciliation.ObservationRequireCurrent, nil
	case !hasObserve && install.Verification() == lock.VerificationInsufficient:
		return reconciliation.ObservationAttemptWhenUnsupported, nil
	default:
		return "", fmt.Errorf(
			"carrier relation install/observe contract shape is unsupported: has_observe=%t observe_verification=%q install_verification=%q",
			hasObserve,
			observe.Verification(),
			install.Verification(),
		)
	}
}
