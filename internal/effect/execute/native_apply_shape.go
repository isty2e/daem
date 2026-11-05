package execute

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
)

// NativeManagedSuffixShape describes residual work, never a live observation or
// executable plan. Bind requires fresh complete input after the preceding effects.
type NativeManagedSuffixShape struct {
	plans       []ApplyEffectPlan
	segment     operationplan.EffectNode
	validations int
	managed     []effectpreparation.ManagedPathEffect
	whole       []effectpreparation.ManagedPathEffect
	aggregates  []effectpreparation.AggregateEffect
	narrowings  []reconcile.NativeNarrowingAction
	adoptions   []NativeWholeAdoption
	orders      []pipackage.OrderObservation
}

// ValidateAggregatePhaseSplit checks both managed projection families without
// manufacturing a post-prefix observation or granting execution authority.
func (preparation NativeApplyStatePreparation) ValidateAggregatePhaseSplit(prefix NativeApplyStatePreparation, residual []effectpreparation.AggregateEffect) error {
	if err := preparation.Validate(); err != nil {
		return err
	}
	if err := prefix.Validate(); err != nil {
		return err
	}
	if len(prefix.orders) != 0 {
		return fmt.Errorf("positive native prefix contains Pi order work")
	}
	if !prefix.before.Equal(preparation.before) || len(prefix.managed)+len(prefix.adoptions)+len(prefix.projectCompletions)+len(preparation.projectCompletions) != 0 {
		return fmt.Errorf("native suffix requires a positive-only prefix from the same complete Before")
	}
	if !slices.EqualFunc(prefix.narrowings, preparation.PositiveNarrowings(), reconcile.NativeNarrowingAction.Equal) {
		return fmt.Errorf("native prefix does not cover its positive ownership transitions")
	}
	for _, effect := range prefix.aggregates {
		if _, native := aggregate.HostConfigSyntaxForCodec(effect.CodecContractID()); !native {
			return fmt.Errorf("positive native prefix contains ordinary aggregate work")
		}
	}
	remaining := preparation
	remaining.aggregates, remaining.narrowings = residual, preparation.narrowingPhase(false)
	after, err := remaining.managedSuccessor(prefix.after)
	if err != nil {
		return err
	}
	if !slices.EqualFunc(after.ManagedPaths(), preparation.after.ManagedPaths(), durable.ManagedPathState.Equal) || !slices.EqualFunc(after.ManagedAggregates(), preparation.after.ManagedAggregates(), durable.ManagedAggregateState.Equal) {
		return fmt.Errorf("native phases do not produce the complete managed successor")
	}
	return nil
}

func ProjectNativeManagedSuffix(input NativeApplyInput, prefix NativeApplyStatePreparation, residual []effectpreparation.AggregateEffect, orders []pipackage.OrderObservation) (NativeManagedSuffixShape, error) {
	err := input.Preparation.ValidateAggregatePhaseSplit(prefix, residual)
	if err != nil {
		return NativeManagedSuffixShape{}, err
	}
	projection := input
	projection.Preparation.aggregates = residual
	projection.Preparation.orders = nil
	projection.Preparation.narrowings = input.Preparation.narrowingPhase(false)
	work, err := nativeApplyWorkFor(projection)
	if err != nil {
		return NativeManagedSuffixShape{}, err
	}
	prefixInput := input
	prefixInput.Preparation = prefix
	prefixWork, err := nativeApplyWorkFor(prefixInput)
	if err != nil {
		return NativeManagedSuffixShape{}, err
	}

	suppressNativePrefixPromotions(&work, prefixWork)
	work.changed = len(work.managed)+len(work.whole)+len(residual)+len(projection.Preparation.narrowings) != 0
	return projectNativeSuffixWork(projection, work, orders)
}

// DelegatePrerequisites retains ordinary/MCP actors in their existing phase,
// leaving native retirement, ownership transitions and order for later.
func (input NativeApplyInput) DelegatePrerequisites() (NativeApplyInput, error) {
	return nativeDelegatePrerequisites(input, input.CurrentState)
}

func nativeDelegatePrerequisites(input NativeApplyInput, baseline durable.Snapshot) (NativeApplyInput, error) {
	if err := input.Preparation.Validate(); err != nil {
		return NativeApplyInput{}, err
	}
	preparation := input.Preparation
	preparation.managed = slices.DeleteFunc(slices.Clone(preparation.managed), func(effect effectpreparation.ManagedPathEffect) bool {
		return aggregate.IsHostConfigDocumentSubject(effect.Subject())
	})
	preparation.aggregates = slices.DeleteFunc(slices.Clone(preparation.aggregates), func(effect effectpreparation.AggregateEffect) bool {
		_, native := aggregate.HostConfigSyntaxForCodec(effect.CodecContractID())
		return native
	})
	preparation.narrowings, preparation.adoptions, preparation.orders, preparation.projectCompletions, preparation.nativeOperations = nil, nil, nil, nil, nil
	after, err := preparation.managedSuccessor(baseline)
	if err != nil {
		return NativeApplyInput{}, err
	}
	if after.Equal(baseline) && len(preparation.managed)+len(preparation.aggregates) != 0 {
		after, err = after.AdvanceNativeDocumentCommit()
		if err != nil {
			return NativeApplyInput{}, err
		}
	}
	preparation.after = after
	input.Preparation = preparation
	return input, nil
}

// ProjectNativeDelegateSuffixes partitions an admitted suffix without inventing
// live intermediate observations. Both shapes require fresh input when bound.
func ProjectNativeDelegateSuffixes(input NativeApplyInput, prefix NativeApplyStatePreparation, residual []effectpreparation.AggregateEffect, orders []pipackage.OrderObservation) (NativeManagedSuffixShape, NativeManagedSuffixShape, error) {
	if err := input.Preparation.ValidateAggregatePhaseSplit(prefix, residual); err != nil {
		return NativeManagedSuffixShape{}, NativeManagedSuffixShape{}, err
	}
	prerequisites, err := nativeDelegatePrerequisites(input, prefix.AfterState())
	if err != nil {
		return NativeManagedSuffixShape{}, NativeManagedSuffixShape{}, err
	}
	late := input
	late.Preparation.managed = slices.DeleteFunc(slices.Clone(input.Preparation.managed), func(effect effectpreparation.ManagedPathEffect) bool {
		return !aggregate.IsHostConfigDocumentSubject(effect.Subject())
	})
	late.Preparation.aggregates = slices.DeleteFunc(slices.Clone(residual), func(effect effectpreparation.AggregateEffect) bool {
		_, native := aggregate.HostConfigSyntaxForCodec(effect.CodecContractID())
		return !native
	})
	late.Preparation.narrowings, late.Preparation.orders = input.Preparation.narrowingPhase(false), nil
	projected, err := late.Preparation.managedSuccessor(prerequisites.Preparation.AfterState())
	if err != nil {
		return NativeManagedSuffixShape{}, NativeManagedSuffixShape{}, err
	}
	if !slices.EqualFunc(projected.ManagedPaths(), input.Preparation.after.ManagedPaths(), durable.ManagedPathState.Equal) ||
		!slices.EqualFunc(projected.ManagedAggregates(), input.Preparation.after.ManagedAggregates(), durable.ManagedAggregateState.Equal) {
		return NativeManagedSuffixShape{}, NativeManagedSuffixShape{}, fmt.Errorf("delegate phases do not preserve the complete managed successor")
	}
	prefixInput := input
	prefixInput.Preparation = prefix
	prefixWork, err := nativeApplyWorkFor(prefixInput)
	if err != nil {
		return NativeManagedSuffixShape{}, NativeManagedSuffixShape{}, err
	}
	firstWork, err := nativeApplyWorkFor(prerequisites)
	if err != nil {
		return NativeManagedSuffixShape{}, NativeManagedSuffixShape{}, err
	}
	suppressNativePrefixPromotions(&firstWork, prefixWork)
	firstWork.changed = len(firstWork.managed)+len(firstWork.ordinaryAggregates) != 0
	first, err := projectNativeSuffixWork(prerequisites, firstWork, nil)
	if err != nil {
		return NativeManagedSuffixShape{}, NativeManagedSuffixShape{}, err
	}
	lastWork, err := nativeApplyWorkFor(late)
	if err != nil {
		return NativeManagedSuffixShape{}, NativeManagedSuffixShape{}, err
	}
	suppressNativePrefixPromotions(&lastWork, prefixWork)
	suppressNativePrefixPromotions(&lastWork, firstWork)
	lastWork.changed = len(lastWork.managed)+len(lastWork.whole)+len(late.Preparation.aggregates)+len(late.Preparation.narrowings) != 0
	last, err := projectNativeSuffixWork(late, lastWork, orders)
	return first, last, err
}

func suppressNativePrefixPromotions(work *nativeApplyWork, prefix nativeApplyWork) {
	if !prefix.changed {
		return
	}
	for index, namespace := range work.documents.Evidence().Namespaces() {
		for earlier, selected := range prefix.documents.Evidence().Namespaces() {
			if slices.Equal(namespace.Addresses(), selected.Addresses()) && prefix.promoting[earlier] {
				work.promoting[index] = false
			}
		}
	}
}

func projectNativeSuffixWork(input NativeApplyInput, work nativeApplyWork, orders []pipackage.OrderObservation) (NativeManagedSuffixShape, error) {
	groups := make([]int, 0, len(orders))
	for _, order := range orders {
		if _, err := order.CaptureInput(); err != nil {
			return NativeManagedSuffixShape{}, err
		}
		alias, err := pipackage.SettingsDocumentAddress(order.Scope())
		if err != nil {
			return NativeManagedSuffixShape{}, err
		}
		found := false
		for index, namespace := range work.documents.Evidence().Namespaces() {
			if slices.Contains(namespace.Addresses(), alias) {
				found = true
				if !work.mutating[index] && !slices.Contains(groups, index) {
					groups = append(groups, index)
				}
			}
		}
		if !found {
			return NativeManagedSuffixShape{}, fmt.Errorf("Pi order suffix has no retained native settings member")
		}
	}

	variants := []nativeApplyWork{work}
	for _, group := range groups {
		for _, previous := range slices.Clone(variants) {
			previous.mutating = slices.Clone(previous.mutating)
			previous.mutating[group], previous.changed = true, true
			variants = append(variants, previous)
		}
	}

	shape := NativeManagedSuffixShape{
		managed: slices.Clone(work.managed), whole: slices.Clone(work.whole), aggregates: input.Preparation.AggregateEffects(), orders: slices.Clone(orders),
		narrowings: input.Preparation.Narrowings(), adoptions: input.Preparation.WholeAdoptions(),
	}
	var builder operationplan.EffectStructureBuilder
	segments := make([]operationplan.EffectNode, 0, len(variants))
	for index, variant := range variants {
		plan, err := prepareNativeApplyEffectPlan(input, variant)
		if err != nil {
			return NativeManagedSuffixShape{}, err
		}
		validations, err := maximumNativeWorkValidationCount(input, variant)
		if err != nil {
			return NativeManagedSuffixShape{}, err
		}
		shape.plans = append(shape.plans, plan)
		shape.validations = max(shape.validations, validations)
		segments = append(segments, builder.Namespace(fmt.Sprintf("native-suffix/variant/%d", index), plan.Segment()))
	}

	shape.segment = segments[0]
	if len(segments) > 1 {
		shape.segment = builder.Choice("native-suffix/variant", segments...)
	}
	if _, err := builder.Compile(builder.ForwardPhase("native-suffix", shape.segment)); err != nil {
		return NativeManagedSuffixShape{}, err
	}
	return shape, nil
}

func (shape NativeManagedSuffixShape) Segment() operationplan.EffectNode { return shape.segment }

func (shape NativeManagedSuffixShape) ForwardValidationCount() int { return shape.validations }

func (shape NativeManagedSuffixShape) Bind(input NativeApplyInput) (ApplyEffectPlan, error) {
	if len(shape.plans) == 0 {
		return ApplyEffectPlan{}, fmt.Errorf("native suffix shape is unavailable")
	}
	work, err := nativeApplyWorkFor(input)
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	if !slices.EqualFunc(shape.managed, work.managed, effectpreparation.ManagedPathEffect.Equal) ||
		!slices.EqualFunc(shape.whole, work.whole, effectpreparation.ManagedPathEffect.Equal) ||
		!slices.EqualFunc(shape.narrowings, input.Preparation.narrowings, reconcile.NativeNarrowingAction.Equal) ||
		!slices.EqualFunc(shape.adoptions, input.Preparation.adoptions, sameNativeWholeAdoption) ||
		!slices.EqualFunc(shape.aggregates, input.Preparation.AggregateEffects(), effectpreparation.AggregateEffect.SameMutation) {
		return ApplyEffectPlan{}, fmt.Errorf("native suffix actors changed")
	}
	for _, order := range input.Preparation.PiOrders() {
		if !slices.ContainsFunc(shape.orders, order.SameSelection) {
			return ApplyEffectPlan{}, fmt.Errorf("native suffix order selection changed")
		}
	}

	plan, err := prepareNativeApplyEffectPlan(input, work)
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	for _, reserved := range shape.plans {
		if reserved.structure.Equal(plan.structure) {
			return plan, nil
		}
	}
	return ApplyEffectPlan{}, fmt.Errorf("native suffix structure changed")
}

func sameNativeWholeAdoption(left, right NativeWholeAdoption) bool {
	return left.source.Equal(right.source) && effectpreparation.ManagedPathEffect.Equal(left.record, right.record) &&
		slices.EqualFunc(left.retired, right.retired, durable.ManagedAggregateState.Equal)
}
