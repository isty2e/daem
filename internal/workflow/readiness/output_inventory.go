package readiness

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// OutputInventoryInput contains the persisted declaration and observation
// capabilities needed to classify selected output occupancy. It deliberately
// omits source, provider, relation, order, and delegate observation.
type OutputInventoryInput struct {
	Paths                   daempaths.Paths
	Resolver                liveobserve.DestinationResolver
	Environment             desired.Environment
	Lockfile                lock.File
	TargetValues            []string
	Codecs                  aggregate.CodecCatalog
	HookContributionEncoder commandhook.ContributionEncoder
	MCPContributionEncoder  lockrefine.MCPContributionEncoder
}

// OutputInventoryAssessment is the narrow read-only projection evidence needed
// by list outputs. Its decisions carry no execution authority.
type OutputInventoryAssessment struct {
	CurrentState durable.Snapshot
	Selection    targetselection.Selection
	ManagedPaths []reconcile.ManagedPathDecision
	Aggregates   []reconcile.AggregateDecision
}

// AssessOutputInventory observes only selected managed paths, aggregate
// documents, durable state, and output ownership. It does not assess broader
// readiness or source freshness.
func AssessOutputInventory(
	ctx context.Context,
	input OutputInventoryInput,
) (OutputInventoryAssessment, error) {
	currentState, err := statefile.LoadOptional(ctx, input.Paths.StatefilePath)
	if err != nil {
		return OutputInventoryAssessment{}, err
	}
	availableTargets := outputInventoryAvailableTargets(
		input.Environment,
		input.Lockfile,
		currentState,
	)
	selection, err := targetselection.ForAvailableTargets(availableTargets, input.TargetValues)
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("%w: %w", targetselection.ErrInvalid, err)
	}
	selectedTargets, err := reconcile.NewSelectedTargets(selection.Targets())
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("normalize selected targets: %w", err)
	}

	managedInputs, err := buildManagedPathPlanningInputs(input.Lockfile, currentState, selection)
	if err != nil {
		return OutputInventoryAssessment{}, err
	}
	aggregateInputs, err := buildManagedAggregatePlanningInputs(
		ctx,
		input.Resolver,
		input.Environment,
		input.Lockfile.Locked,
		currentState,
		selection,
		input.HookContributionEncoder,
		input.MCPContributionEncoder,
		input.Codecs,
	)
	if err != nil {
		return OutputInventoryAssessment{}, err
	}
	managedEvidence, err := liveobserve.ManagedPathEvidence(ctx, input.Resolver, managedInputs.requests)
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("observe output inventory paths: %w", err)
	}
	owner, ownership, err := buildOwnershipObservations(
		ctx,
		input.Paths,
		liveDestinationResolver(input.Resolver),
		managedInputs.ownership,
		aggregateInputs.ownershipProjections(),
		currentState,
		selection,
	)
	if err != nil {
		return OutputInventoryAssessment{}, err
	}

	expectations, err := managedPathExpectations(input.Environment, input.Lockfile.Locked)
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("derive output inventory path expectations: %w", err)
	}
	managedPaths, err := reconcileprojection.BuildManagedPathInventoryDecisions(
		reconcileprojection.ManagedPathInventoryInput{
			Locked:          input.Lockfile.Locked,
			Expectations:    expectations,
			SelectedTargets: selectedTargets,
			States:          managedInputs.states,
			Evidence:        managedEvidence,
			Owner:           owner,
			Ownership:       ownership,
		},
	)
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("classify output inventory paths: %w", err)
	}
	aggregates, err := reconcileprojection.BuildAggregateDecisions(
		reconcileprojection.AggregateInput{
			Locked:               input.Lockfile.Locked,
			Expected:             aggregateInputs.expected,
			Desired:              aggregateInputs.desired,
			States:               aggregateInputs.states,
			Evidence:             aggregateInputs.evidence,
			ObservationFailures:  aggregateInputs.failures,
			PreconditionEvidence: aggregateInputs.preconditions,
			SelectedTargets:      selectedTargets,
			Owner:                owner,
			Ownership:            ownership,
			Codecs:               input.Codecs,
		},
	)
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("classify output inventory aggregates: %w", err)
	}

	return OutputInventoryAssessment{
		CurrentState: currentState,
		Selection:    selection,
		ManagedPaths: managedPaths,
		Aggregates:   aggregates,
	}, nil
}

func outputInventoryAvailableTargets(
	environment desired.Environment,
	locked lock.File,
	state durable.Snapshot,
) []target.Target {
	targets := make(map[target.Target]struct{})
	for _, selected := range fromEnvironment(environment) {
		targets[selected] = struct{}{}
	}
	for _, subject := range locked.Locked.Subjects() {
		realization, realized := subject.Realization()
		if !realized {
			continue
		}
		for _, consumer := range realization.ConsumerTargets() {
			targets[consumer] = struct{}{}
		}
	}
	for _, managedPath := range state.ManagedPaths() {
		for _, consumer := range managedPath.ConsumerTargets() {
			targets[consumer] = struct{}{}
		}
	}
	for _, managedAggregate := range state.ManagedAggregates() {
		targets[managedAggregate.Contribution().Target()] = struct{}{}
	}
	return orderedTargets(targets)
}
