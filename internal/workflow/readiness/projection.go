package readiness

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type projectionPlanningInput struct {
	environment             desired.Environment
	locked                  lock.LockedSection
	selectedTargets         reconcile.SelectedTargets
	supplyObservations      []observe.ExactSupplyObservation
	managedPathStates       []durable.ManagedPathState
	managedPathEvidence     []observe.ManagedPathEvidence
	aggregateExpected       []lock.LockedSubjectContract
	aggregateDesired        []aggregate.SubjectContribution
	aggregateConstraints    []reconcileprojection.AggregateSubjectConstraint
	aggregateRemovalNotices []reconcileprojection.AggregateRemovalNotice
	aggregateStates         []durable.ManagedAggregateState
	aggregateEvidence       []observe.AggregateEvidence
	aggregateFailures       []observe.AggregateObservationFailure
	aggregatePreconditions  []observe.AggregatePreconditionEvidence
	manageUnmanagedMatches  bool
	owner                   stateauthority.Authority
	ownership               []observe.OwnershipObservation
	codecs                  aggregate.CodecCatalog
	nativeNarrowings        []reconcile.NativeNarrowingAction
	nativeWholeAdoptions    []reconcileprojection.NativeWholeAdoptionPlan
}

func buildProjectionDecisions(input projectionPlanningInput) (
	[]reconcile.ManagedPathDecision,
	[]reconcile.AggregateDecision,
	error,
) {
	expectations, err := managedPathExpectations(input.environment, input.locked)
	if err != nil {
		return nil, nil, fmt.Errorf("derive managed path lock expectations: %w", err)
	}
	managedPaths, err := reconcileprojection.BuildManagedPathDecisions(reconcileprojection.ManagedPathInput{
		Locked: input.locked, Expectations: expectations, SelectedTargets: input.selectedTargets,
		SupplyObservations: input.supplyObservations,
		States:             input.managedPathStates, Evidence: input.managedPathEvidence,
		ManageUnmanagedMatches: input.manageUnmanagedMatches,
		Owner:                  input.owner, Ownership: input.ownership,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build managed path decisions: %w", err)
	}
	for _, adoption := range input.nativeWholeAdoptions {
		if err := adoption.Validate(); err != nil {
			return nil, nil, err
		}
		found := false
		for index, decision := range managedPaths {
			if decision.Subject() == adoption.Decision().Subject() {
				managedPaths[index], found = adoption.Decision(), true
				break
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("native whole adoption has no corresponding managed path decision")
		}
	}
	aggregateLocked := input.locked
	if len(input.nativeNarrowings) != 0 {
		consumed := make(map[topology.SubjectID]struct{})
		for _, action := range input.nativeNarrowings {
			for _, item := range action.DesiredContributions() {
				consumed[item.SubjectID()] = struct{}{}
			}
		}
		var remaining []lock.LockedSubjectContract
		for _, subject := range input.locked.Subjects() {
			if _, found := consumed[subject.SubjectID()]; !found {
				remaining = append(remaining, subject)
			}
		}
		aggregateLocked, err = lock.NewLockedSection(remaining, input.locked.OrderConstraints())
		if err != nil {
			return nil, nil, err
		}
	}
	constraints := append([]reconcileprojection.AggregateSubjectConstraint(nil), input.aggregateConstraints...)
	for _, adoption := range input.nativeWholeAdoptions {
		decision := adoption.Decision()
		if !decision.IsBlocked() {
			continue
		}
		wholeOwner, entityBacked := topologyprojection.EntityID(decision.Subject())
		for _, state := range input.aggregateStates {
			keyOwner, _, native := topologyprojection.HostConfigKeyIdentity(state.Subject())
			if !entityBacked || !native || keyOwner != wholeOwner {
				continue
			}
			constraint, err := reconcileprojection.NewAggregateSubjectConstraint(state.Subject(), decision.Reason(), decision.Detail())
			if err != nil {
				return nil, nil, err
			}
			constraints = append(constraints, constraint)
		}
	}
	aggregates, err := reconcileprojection.BuildAggregateDecisions(reconcileprojection.AggregateInput{
		Locked: aggregateLocked, Expected: input.aggregateExpected, Desired: input.aggregateDesired,
		Constraints:    constraints,
		RemovalNotices: input.aggregateRemovalNotices,
		States:         input.aggregateStates, Evidence: input.aggregateEvidence,
		ObservationFailures:    input.aggregateFailures,
		PreconditionEvidence:   input.aggregatePreconditions,
		SelectedTargets:        input.selectedTargets,
		ManageUnmanagedMatches: input.manageUnmanagedMatches,
		Owner:                  input.owner,
		Ownership:              input.ownership,
		Codecs:                 input.codecs,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build managed aggregate decisions: %w", err)
	}
	return managedPaths, aggregates, nil
}

func managedPathExpectations(
	environment desired.Environment,
	locked lock.LockedSection,
) ([]reconcileprojection.ManagedPathExpectation, error) {
	contracts, err := lockrefine.ExpectedManagedPaths(environment, locked)
	if err != nil {
		return nil, err
	}
	expectations := make([]reconcileprojection.ManagedPathExpectation, 0, len(contracts))
	for _, contract := range contracts {
		expectation, err := reconcileprojection.NewManagedPathExpectation(contract)
		if err != nil {
			return nil, fmt.Errorf("managed path expectation for %q: %w", contract.SubjectID(), err)
		}
		expectations = append(expectations, expectation)
	}
	return expectations, nil
}
