package readiness

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/build/projection"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type NativeSourcePreparationInput struct {
	Environment            desired.Environment
	Selection              targetselection.Selection
	SourceEpoch            lockobserve.SourceEpoch
	Documents              journal.NativeDocumentObservationBatch
	DocumentLimits         configdocument.Limits
	ValueLimits            configdocument.Limits
	ManageUnmanagedMatches bool
}

// NativeSourcePreparation partitions declaration-mode transitions before generic
// removal planning. It is not a complete assessment or execution authority.
type NativeSourcePreparation struct {
	sources     []lockobserve.NativeSourceObservation
	narrowings  []reconcile.NativeNarrowingAction
	remaining   []durable.ManagedPathState
	adoptions   []projection.NativeWholeAdoptionPlan
	aggregates  []durable.ManagedAggregateState
	initialized bool
}

func (preparation NativeSourcePreparation) Validate() error {
	if !preparation.initialized {
		return fmt.Errorf("native source preparation is required")
	}
	return nil
}

func (preparation NativeSourcePreparation) Sources() []lockobserve.NativeSourceObservation {
	return slices.Clone(preparation.sources)
}

func (preparation NativeSourcePreparation) Narrowings() []reconcile.NativeNarrowingAction {
	return slices.Clone(preparation.narrowings)
}

func (preparation NativeSourcePreparation) WholeAdoptions() []projection.NativeWholeAdoptionPlan {
	return slices.Clone(preparation.adoptions)
}

// RemainingManagedAggregates excludes successful state-only whole adoptions
// from generic key removal. The document batch retains the complete Before.
func (preparation NativeSourcePreparation) RemainingManagedAggregates() []durable.ManagedAggregateState {
	return slices.Clone(preparation.aggregates)
}

// RemainingManagedPaths excludes only baselines consumed by typed narrowings.
// The complete before Snapshot remains in the containing observation batch.
func (preparation NativeSourcePreparation) RemainingManagedPaths() []durable.ManagedPathState {
	return slices.Clone(preparation.remaining)
}

func PrepareNativeSources(ctx context.Context, input NativeSourcePreparationInput) (NativeSourcePreparation, error) {
	if err := input.Documents.ValidateBefore(ctx); err != nil {
		return NativeSourcePreparation{}, err
	}
	if err := input.Environment.Validate(); err != nil {
		return NativeSourcePreparation{}, err
	}
	if err := input.DocumentLimits.Validate(); err != nil {
		return NativeSourcePreparation{}, err
	}
	if err := input.ValueLimits.Validate(); err != nil {
		return NativeSourcePreparation{}, err
	}

	var sources []lockobserve.NativeSourceObservation
	for _, config := range input.Environment.HostConfigs() {
		if !input.Selection.Includes(config.Target()) {
			continue
		}
		observed, err := input.SourceEpoch.NativeSource(ctx, config)
		if err != nil {
			return NativeSourcePreparation{}, err
		}
		sources = append(sources, observed)
	}
	slices.SortFunc(sources, func(left, right lockobserve.NativeSourceObservation) int {
		return topology.CompareSubjectID(left.SourceContract().SubjectID(), right.SourceContract().SubjectID())
	})

	before := input.Documents.BeforeState().ManagedPaths()
	byDeclaration := make(map[entity.ID][]durable.ManagedPathState)
	for _, state := range before {
		if !aggregate.IsHostConfigDocumentSubject(state.Subject()) {
			continue
		}
		id, entityBacked := topologyprojection.EntityID(state.Subject())
		if entityBacked {
			byDeclaration[id] = append(byDeclaration[id], state)
		}
	}
	consumed := make(map[topology.SubjectID]struct{})
	var actions []reconcile.NativeNarrowingAction
	for _, source := range sources {
		placement, _ := source.SourceContract().HostConfigPlacement()
		if placement.Ownership() != hostconfig.OwnershipKeys {
			continue
		}
		baselines := byDeclaration[source.SourceContract().EntityID()]
		if len(baselines) == 0 {
			continue
		}
		if len(baselines) != 1 {
			return NativeSourcePreparation{}, fmt.Errorf("native declaration has more than one prior whole-document baseline")
		}
		baseline := baselines[0]
		alias, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
		if err != nil {
			return NativeSourcePreparation{}, err
		}
		current, _, observed := input.Documents.Document(alias)
		if !observed {
			return NativeSourcePreparation{}, fmt.Errorf("native narrowing has no retained document observation")
		}
		action, err := BuildNativeNarrowingAction(ctx, NativeNarrowingActionInput{
			Source: source, Baseline: baseline, Current: current,
			DocumentLimits: input.DocumentLimits, ValueLimits: input.ValueLimits,
		})
		if err != nil {
			return NativeSourcePreparation{}, err
		}
		if _, duplicate := consumed[baseline.Subject()]; duplicate {
			return NativeSourcePreparation{}, fmt.Errorf("native narrowing repeats a prior baseline")
		}
		consumed[baseline.Subject()] = struct{}{}
		actions = append(actions, action)
	}
	remaining := make([]durable.ManagedPathState, 0, len(before)-len(consumed))
	for _, state := range before {
		if _, narrowed := consumed[state.Subject()]; !narrowed {
			remaining = append(remaining, state)
		}
	}
	beforeKeys := input.Documents.BeforeState().ManagedAggregates()
	retiredKeys := make(map[topology.SubjectID]struct{})
	var adoptions []projection.NativeWholeAdoptionPlan
	for _, source := range sources {
		placement, _ := source.SourceContract().HostConfigPlacement()
		if placement.Ownership() != hostconfig.OwnershipDocument || !slices.ContainsFunc(beforeKeys, func(state durable.ManagedAggregateState) bool {
			owner, _, native := topologyprojection.HostConfigKeyIdentity(state.Subject())
			return native && owner == source.SourceContract().EntityID()
		}) {
			continue
		}
		adoption, err := PrepareNativeWholeAdoption(ctx, NativeWholeAdoptionInput{
			Source: source, Documents: input.Documents, ManageUnmanagedMatches: input.ManageUnmanagedMatches,
		})
		if err != nil {
			return NativeSourcePreparation{}, err
		}
		for _, subject := range adoption.RetiredSubjects() {
			if _, duplicate := retiredKeys[subject]; duplicate {
				return NativeSourcePreparation{}, fmt.Errorf("native whole adoptions repeat a retiring key")
			}
			retiredKeys[subject] = struct{}{}
		}
		adoptions = append(adoptions, adoption)
	}
	remainingKeys := make([]durable.ManagedAggregateState, 0, len(beforeKeys)-len(retiredKeys))
	for _, state := range beforeKeys {
		if _, retired := retiredKeys[state.Subject()]; !retired {
			remainingKeys = append(remainingKeys, state)
		}
	}
	if err := input.Documents.ValidateBefore(ctx); err != nil {
		return NativeSourcePreparation{}, err
	}
	return NativeSourcePreparation{
		sources: sources, narrowings: actions, remaining: remaining,
		adoptions: adoptions, aggregates: remainingKeys, initialized: true,
	}, nil
}
