package execute

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type NativeApplyStateInput struct {
	Before       durable.Snapshot
	ManagedPaths []reconcile.ManagedPathDecision
	Aggregates   []reconcile.AggregateDecision
	Narrowings   []reconcile.NativeNarrowingAction
	WholeSources []lock.LockedSubjectContract
	StateCodec   durable.SnapshotCodec
}

// NativeWholeAdoption binds an actual matching whole Record to state-only key
// retirement. Root, source freshness and registry authority remain operation-owned.
type NativeWholeAdoption struct {
	source  lock.LockedSubjectContract
	record  ManagedPathEffect
	retired []durable.ManagedAggregateState
}

func (adoption NativeWholeAdoption) SourceContract() lock.LockedSubjectContract {
	return adoption.source
}

func (adoption NativeWholeAdoption) RecordEffect() ManagedPathEffect { return adoption.record }

func (adoption NativeWholeAdoption) RetiredKeys() []durable.ManagedAggregateState {
	return append([]durable.ManagedAggregateState(nil), adoption.retired...)
}

// NativeApplyStatePreparation keeps derived state and its actual logical effects
// together. It is not a containing operation seal or permission to publish state.
type NativeApplyStatePreparation struct {
	before           durable.Snapshot
	after            durable.Snapshot
	managed          []ManagedPathEffect
	aggregates       []AggregateEffect
	nativeOperations []profile.NativeAggregateOperationContract
	narrowings       []reconcile.NativeNarrowingAction
	adoptions        []NativeWholeAdoption
	initialized      bool
}

func (preparation NativeApplyStatePreparation) Validate() error {
	if !preparation.initialized {
		return fmt.Errorf("native Apply state preparation is required")
	}
	return nil
}

func (preparation NativeApplyStatePreparation) BeforeState() durable.Snapshot {
	return preparation.before
}

func (preparation NativeApplyStatePreparation) AfterState() durable.Snapshot {
	return preparation.after
}

func (preparation NativeApplyStatePreparation) ManagedPathEffects() []ManagedPathEffect {
	return append([]ManagedPathEffect(nil), preparation.managed...)
}

func (preparation NativeApplyStatePreparation) AggregateEffects() []AggregateEffect {
	return append([]AggregateEffect(nil), preparation.aggregates...)
}

// NativeAggregateOperations retains static admission for every selected native
// aggregate, including no-ops. It supplies no fresh observation or runtime grant.
func (preparation NativeApplyStatePreparation) NativeAggregateOperations() []profile.NativeAggregateOperationContract {
	return append([]profile.NativeAggregateOperationContract(nil), preparation.nativeOperations...)
}

func (preparation NativeApplyStatePreparation) Narrowings() []reconcile.NativeNarrowingAction {
	return append([]reconcile.NativeNarrowingAction(nil), preparation.narrowings...)
}

func (preparation NativeApplyStatePreparation) WholeAdoptions() []NativeWholeAdoption {
	return append([]NativeWholeAdoption(nil), preparation.adoptions...)
}

func PrepareNativeApplyState(ctx context.Context, input NativeApplyStateInput) (NativeApplyStatePreparation, error) {
	if ctx == nil || input.StateCodec == nil {
		return NativeApplyStatePreparation{}, fmt.Errorf("native state preparation requires context and the selected snapshot codec")
	}
	if err := ctx.Err(); err != nil {
		return NativeApplyStatePreparation{}, err
	}
	if _, err := input.StateCodec.Encode(input.Before); err != nil {
		return NativeApplyStatePreparation{}, err
	}
	managed, err := ManagedPathEffects(input.ManagedPaths)
	if err != nil {
		return NativeApplyStatePreparation{}, err
	}
	aggregates, err := AggregateEffectsForNativeOperation(input.Aggregates)
	if err != nil {
		return NativeApplyStatePreparation{}, err
	}
	nativeOperations := make([]profile.NativeAggregateOperationContract, 0)
	for _, decision := range input.Aggregates {
		if _, native := aggregate.HostConfigSyntaxForCodec(decision.CodecContractID()); !native {
			continue
		}
		operation, present := decision.NativeOperationContract()
		if !present {
			return NativeApplyStatePreparation{}, fmt.Errorf("native aggregate preparation requires static admission")
		}
		selection, err := decision.BeforeSnapshot().Selection()
		if err != nil {
			return NativeApplyStatePreparation{}, err
		}
		if err := operation.ValidateSelection(selection); err != nil {
			return NativeApplyStatePreparation{}, err
		}
		nativeOperations = append(nativeOperations, operation)
	}
	sources, err := nativeWholeStateSources(input.WholeSources)
	if err != nil {
		return NativeApplyStatePreparation{}, err
	}
	indexes := indexNativeApplyState(input.Before)
	if err := validateNativeManagedStateInputs(indexes, managed); err != nil {
		return NativeApplyStatePreparation{}, err
	}
	if err := validateNativeAggregateStateInputs(indexes, aggregates); err != nil {
		return NativeApplyStatePreparation{}, err
	}
	adoptions, err := prepareNativeWholeAdoptions(ctx, indexes, input.ManagedPaths, managed, sources)
	if err != nil {
		return NativeApplyStatePreparation{}, err
	}
	retiredKeys := make(map[topology.SubjectID]struct{})
	for _, adoption := range adoptions {
		for _, state := range adoption.retired {
			if _, duplicate := retiredKeys[state.Subject()]; duplicate {
				return NativeApplyStatePreparation{}, fmt.Errorf("native key state is retired by more than one whole adoption")
			}
			retiredKeys[state.Subject()] = struct{}{}
		}
	}
	retiredPaths, desired, err := nativeNarrowingStateDelta(ctx, indexes, input.Narrowings, sources)
	if err != nil {
		return NativeApplyStatePreparation{}, err
	}
	if err := rejectNativeStateEffectOverlap(managed, aggregates, retiredPaths, retiredKeys, desired); err != nil {
		return NativeApplyStatePreparation{}, err
	}

	paths := input.Before.ManagedPaths()
	remainingPaths := make([]durable.ManagedPathState, 0, len(paths))
	for _, state := range paths {
		if _, retired := retiredPaths[state.Subject()]; !retired {
			remainingPaths = append(remainingPaths, state)
		}
	}
	keys := input.Before.ManagedAggregates()
	remainingKeys := make([]durable.ManagedAggregateState, 0, len(keys))
	for _, state := range keys {
		if _, retired := retiredKeys[state.Subject()]; !retired {
			remainingKeys = append(remainingKeys, state)
		}
	}
	after, err := input.Before.WithManagedPaths(remainingPaths)
	if err != nil {
		return NativeApplyStatePreparation{}, err
	}
	after, err = after.WithManagedAggregates(remainingKeys)
	if err != nil {
		return NativeApplyStatePreparation{}, err
	}
	after, err = snapshotAfterManagedPathEffects(after, managed)
	if err != nil {
		return NativeApplyStatePreparation{}, err
	}
	after, err = snapshotAfterAggregateEffects(after, aggregates)
	if err != nil {
		return NativeApplyStatePreparation{}, err
	}
	newKeys := after.ManagedAggregates()
	for _, item := range desired {
		state, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
		if err != nil {
			return NativeApplyStatePreparation{}, err
		}
		newKeys = append(newKeys, state)
	}
	after, err = after.WithManagedAggregates(newKeys)
	if err != nil {
		return NativeApplyStatePreparation{}, err
	}
	if after.Equal(input.Before) && len(managed)+len(aggregates) != 0 {
		after, err = after.AdvanceNativeDocumentCommit()
		if err != nil {
			return NativeApplyStatePreparation{}, err
		}
	}
	if _, err := input.StateCodec.Encode(after); err != nil {
		return NativeApplyStatePreparation{}, err
	}
	if err := ctx.Err(); err != nil {
		return NativeApplyStatePreparation{}, err
	}
	return NativeApplyStatePreparation{
		before: input.Before, after: after, managed: managed, aggregates: aggregates,
		nativeOperations: nativeOperations,
		narrowings:       append([]reconcile.NativeNarrowingAction(nil), input.Narrowings...), adoptions: adoptions, initialized: true,
	}, nil
}

type nativeWholeStateSource struct {
	source     lock.LockedSubjectContract
	projection lock.LockedSubjectContract
}

type nativeApplyStateIndexes struct {
	paths     map[topology.SubjectID]durable.ManagedPathState
	keys      map[topology.SubjectID]durable.ManagedAggregateState
	documents map[aggregate.DocumentAddress][]durable.ManagedAggregateState
}

func indexNativeApplyState(before durable.Snapshot) nativeApplyStateIndexes {
	indexes := nativeApplyStateIndexes{
		paths:     make(map[topology.SubjectID]durable.ManagedPathState),
		keys:      make(map[topology.SubjectID]durable.ManagedAggregateState),
		documents: make(map[aggregate.DocumentAddress][]durable.ManagedAggregateState),
	}
	for _, state := range before.ManagedPaths() {
		indexes.paths[state.Subject()] = state
	}
	for _, state := range before.ManagedAggregates() {
		indexes.keys[state.Subject()] = state
		document := state.Contribution().Address().Document()
		indexes.documents[document] = append(indexes.documents[document], state)
	}
	return indexes
}

func validateNativeManagedStateInputs(before nativeApplyStateIndexes, effects []ManagedPathEffect) error {
	touched := make(map[topology.SubjectID]struct{})
	for _, effect := range effects {
		previous, hasPrevious := effect.PreviousState()
		if hasPrevious {
			state, present := before.paths[previous.Subject()]
			if !present || !state.Equal(previous) {
				return fmt.Errorf("managed path effect has no exact previous state")
			}
		}
		if _, present := before.paths[effect.Subject()]; present && (!hasPrevious || previous.Subject() != effect.Subject()) {
			return fmt.Errorf("managed path effect would overwrite unselected previous ownership")
		}
		subjects := []topology.SubjectID{effect.Subject()}
		if hasPrevious && previous.Subject() != effect.Subject() {
			subjects = append(subjects, previous.Subject())
		}
		for _, subject := range subjects {
			if _, duplicate := touched[subject]; duplicate {
				return fmt.Errorf("managed path state is touched by more than one effect")
			}
			touched[subject] = struct{}{}
		}
	}
	return nil
}

func validateNativeAggregateStateInputs(before nativeApplyStateIndexes, effects []AggregateEffect) error {
	touched := make(map[topology.SubjectID]struct{})
	for _, effect := range effects {
		subjects := make(map[topology.SubjectID]struct{})
		for _, previous := range effect.PreviousStates() {
			state, present := before.keys[previous.Subject()]
			if !present || !state.Equal(previous) {
				return fmt.Errorf("aggregate effect has no exact previous state")
			}
			if _, duplicate := subjects[previous.Subject()]; duplicate {
				return fmt.Errorf("aggregate effect repeats previous ownership")
			}
			subjects[previous.Subject()] = struct{}{}
		}
		for _, item := range effect.DesiredContributions() {
			_, selected := subjects[item.SubjectID()]
			if _, present := before.keys[item.SubjectID()]; present && !selected {
				return fmt.Errorf("aggregate effect would overwrite unselected previous ownership")
			}
			subjects[item.SubjectID()] = struct{}{}
		}
		for subject := range subjects {
			if _, duplicate := touched[subject]; duplicate {
				return fmt.Errorf("aggregate state is touched by more than one effect")
			}
			touched[subject] = struct{}{}
		}
	}
	return nil
}

func nativeWholeStateSources(sources []lock.LockedSubjectContract) (map[entity.ID]nativeWholeStateSource, error) {
	result := make(map[entity.ID]nativeWholeStateSource, len(sources))
	for _, source := range sources {
		placement, present := source.HostConfigPlacement()
		if !present {
			return nil, fmt.Errorf("native whole source placement is required")
		}
		projection, err := lock.NewHostConfigDocumentSubjectContract(source.EntityID(), placement)
		if err != nil {
			return nil, err
		}
		if _, err := lock.NewNativeLockedSection([]lock.LockedSubjectContract{source, projection}, nil); err != nil {
			return nil, err
		}
		if _, duplicate := result[source.EntityID()]; duplicate {
			return nil, fmt.Errorf("native whole source repeats its declaration")
		}
		result[source.EntityID()] = nativeWholeStateSource{source: source, projection: projection}
	}
	return result, nil
}

func prepareNativeWholeAdoptions(ctx context.Context, before nativeApplyStateIndexes, decisions []reconcile.ManagedPathDecision, effects []ManagedPathEffect, sources map[entity.ID]nativeWholeStateSource) ([]NativeWholeAdoption, error) {
	var adoptions []NativeWholeAdoption
	effectIndex := 0
	for _, decision := range decisions {
		if decision.IsNoOp() {
			continue
		}
		effect := effects[effectIndex]
		effectIndex++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id, entityBacked := topologyprojection.EntityID(effect.Subject())
		if !entityBacked || id.Kind() != entity.KindHostConfig || effect.Kind() == ManagedPathEffectRemove {
			continue
		}
		source, present := sources[id]
		if !present || source.projection.SubjectID() != effect.Subject() {
			return nil, fmt.Errorf("native whole effect lacks its admitted source projection")
		}
		placement, _ := source.source.HostConfigPlacement()
		materialized, materializedFile := source.source.MaterializedFileIdentity()
		realization, _ := source.projection.Realization()
		projection, _ := realization.ManagedPathProjection()
		mode, exactMode := projection.ExactPermissionMode()
		consumers := effect.ConsumerTargets()
		if !materializedFile || materialized.ContentHash() != effect.DesiredHash() || len(consumers) != 1 || consumers[0] != placement.Target() ||
			effect.Scope() != placement.Scope() || effect.Destination() != placement.Destination() ||
			effect.ContentKind() != projection.ContentKind() || effect.PermissionPolicy() != projection.PermissionPolicy() ||
			!exactMode || effect.DesiredFileMode() != mode.FileMode() {
			return nil, fmt.Errorf("native whole effect differs from its source placement or materialized identity")
		}
		var owned []durable.ManagedAggregateState
		document, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
		if err != nil {
			return nil, err
		}
		for _, state := range before.documents[document] {
			owner, _, native := topologyprojection.HostConfigKeyIdentity(state.Subject())
			if !native || owner != id {
				return nil, fmt.Errorf("native whole adoption would cover another projection's ownership")
			}
			owned = append(owned, state)
		}
		_, hasPrevious := effect.PreviousState()
		adopting := effect.Kind() == ManagedPathEffectRecord && !hasPrevious && decision.Reason() == reconcile.ReasonManagedExisting
		if len(owned) != 0 && !adopting {
			return nil, fmt.Errorf("keys-to-whole transition requires an exact manage-existing record")
		}
		if effect.Kind() == ManagedPathEffectRecord && !hasPrevious {
			if !adopting || effect.LiveHash() != effect.DesiredHash() || !effect.PermissionPolicy().AcceptsMode(effect.DesiredFileMode(), effect.LiveFileMode()) {
				return nil, fmt.Errorf("native whole adoption requires exact record identity, mode and approval")
			}
			adoptions = append(adoptions, NativeWholeAdoption{source: source.source, record: effect, retired: owned})
		}
	}
	return adoptions, nil
}

func nativeNarrowingStateDelta(ctx context.Context, before nativeApplyStateIndexes, actions []reconcile.NativeNarrowingAction, wholeSources map[entity.ID]nativeWholeStateSource) (map[topology.SubjectID]struct{}, []aggregate.SubjectContribution, error) {
	retired := make(map[topology.SubjectID]struct{}, len(actions))
	desiredSubjects := make(map[topology.SubjectID]struct{})
	var desired []aggregate.SubjectContribution
	for _, action := range actions {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if err := action.Validate(); err != nil {
			return nil, nil, err
		}
		if _, whole := wholeSources[action.SourceContract().EntityID()]; whole {
			return nil, nil, fmt.Errorf("native declaration has both whole and narrowing source intent")
		}
		baseline := action.Baseline()
		state, present := before.paths[baseline.Subject()]
		if !present || !state.Equal(baseline) {
			return nil, nil, fmt.Errorf("native narrowing has no exact previous whole state")
		}
		if _, duplicate := retired[baseline.Subject()]; duplicate {
			return nil, nil, fmt.Errorf("native whole state is narrowed more than once")
		}
		retired[baseline.Subject()] = struct{}{}
		for _, item := range action.DesiredContributions() {
			if _, duplicate := desiredSubjects[item.SubjectID()]; duplicate {
				return nil, nil, fmt.Errorf("native narrowing repeats a desired key subject")
			}
			if _, present := before.keys[item.SubjectID()]; present {
				return nil, nil, fmt.Errorf("native narrowing would overwrite previous key ownership")
			}
			desiredSubjects[item.SubjectID()] = struct{}{}
			desired = append(desired, item)
		}
	}
	return retired, desired, nil
}

func rejectNativeStateEffectOverlap(managed []ManagedPathEffect, aggregates []AggregateEffect, retiredPaths, retiredKeys map[topology.SubjectID]struct{}, desired []aggregate.SubjectContribution) error {
	for _, effect := range managed {
		_, overlaps := retiredPaths[effect.Subject()]
		previous, present := effect.PreviousState()
		_, consumesPrevious := retiredPaths[previous.Subject()]
		if overlaps || present && consumesPrevious {
			return fmt.Errorf("managed path effect also consumes narrowed whole ownership")
		}
	}
	selected := make(map[topology.SubjectID]struct{}, len(desired)+len(retiredKeys))
	for subject := range retiredKeys {
		selected[subject] = struct{}{}
	}
	for _, item := range desired {
		selected[item.SubjectID()] = struct{}{}
	}
	for _, effect := range aggregates {
		for _, state := range effect.PreviousStates() {
			if _, overlaps := selected[state.Subject()]; overlaps {
				return fmt.Errorf("aggregate effect also consumes native mode-transition ownership")
			}
		}
		for _, item := range effect.DesiredContributions() {
			if _, overlaps := selected[item.SubjectID()]; overlaps {
				return fmt.Errorf("aggregate effect also produces native mode-transition ownership")
			}
		}
	}
	return nil
}
