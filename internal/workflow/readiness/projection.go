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
	aggregates, err := reconcileprojection.BuildAggregateDecisions(reconcileprojection.AggregateInput{
		Locked: input.locked, Expected: input.aggregateExpected, Desired: input.aggregateDesired,
		Constraints:    input.aggregateConstraints,
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
