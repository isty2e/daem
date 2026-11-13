package readiness

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/reconcile"
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
	PersistenceEpoch        *PersistenceEpoch
	Native                  *NativeAssessmentInput
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
	var currentState durable.Snapshot
	var err error
	if input.PersistenceEpoch != nil {
		currentState, err = input.PersistenceEpoch.Snapshot()
	} else {
		currentState, err = statefile.LoadOptional(ctx, input.Paths.StatefilePath)
	}
	if err != nil {
		return OutputInventoryAssessment{}, err
	}
	selection, err := SelectOutputInventoryTargets(input.Environment, input.Lockfile, currentState, input.TargetValues)
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("%w: %w", targetselection.ErrInvalid, err)
	}
	selectedTargets, err := reconcile.NewSelectedTargets(selection.Targets())
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("normalize selected targets: %w", err)
	}
	var native []nativeAssessmentSources
	if input.Native != nil {
		if err := input.Native.validatePersistenceEpoch(ctx, input.PersistenceEpoch, currentState); err != nil {
			return OutputInventoryAssessment{}, err
		}
		native = []nativeAssessmentSources{{input: input.Native}}
		for _, contract := range input.Lockfile.Locked.Subjects() {
			contribution, present, err := contract.ManagedAggregateContribution()
			if err != nil {
				return OutputInventoryAssessment{}, err
			}
			if !present || !selection.Includes(contribution.Contribution().Target()) {
				continue
			}
			if _, nativeCodec := aggregate.HostConfigSyntaxForCodec(contribution.Contribution().CodecContractID()); nativeCodec {
				native[0].lockedInventory = append(native[0].lockedInventory, contract)
			}
		}
	}

	managedInputs, err := buildManagedPathPlanningInputs(input.Lockfile, currentState.ManagedPaths(), selection)
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
		native...,
	)
	if err != nil {
		return OutputInventoryAssessment{}, err
	}
	managedEvidence, err := observeManagedPaths(ctx, input.Resolver, managedInputs.requests, native...)
	if err != nil {
		return OutputInventoryAssessment{}, fmt.Errorf("observe output inventory paths: %w", err)
	}
	var ownershipResult ownershipobserve.Result
	if input.Native != nil {
		ownershipResult, err = nativeOwnershipObservations(ctx, input.Paths, liveDestinationResolver(input.Resolver), managedInputs, aggregateInputs, currentState, selection, input.Native)
	} else {
		ownershipResult, err = buildOwnershipObservations(
			ctx,
			input.Paths,
			liveDestinationResolver(input.Resolver),
			managedInputs.ownership,
			aggregateInputs.ownershipProjections(),
			currentState,
			selection,
		)
	}
	if err != nil {
		return OutputInventoryAssessment{}, err
	}

	return planOutputInventory(outputInventoryPlanInput{
		environment:     input.Environment,
		locked:          input.Lockfile,
		selection:       selection,
		selectedTargets: selectedTargets,
		currentState:    currentState,
		managedInputs:   managedInputs,
		aggregateInputs: aggregateInputs,
		managedEvidence: managedEvidence,
		owner:           ownershipResult.Owner,
		ownership:       ownershipResult.Observations,
		codecs:          input.Codecs,
	})
}

// SelectOutputInventoryTargets uses only declared, locked and retained targets.
func SelectOutputInventoryTargets(environment desired.Environment, locked lock.File, state durable.Snapshot, values []string) (targetselection.Selection, error) {
	return targetselection.ForAvailableTargets(outputInventoryAvailableTargets(environment, locked, state), values)
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
