package execute

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile"
)

// NativeManagedSuffixShape describes residual work, never a live observation or
// executable plan. Bind requires fresh complete input after the preceding effects.
type NativeManagedSuffixShape struct {
	plans       []applyprogram.Plan
	segment     operationplan.EffectNode
	validations int
	managed     []effectpreparation.ManagedPathEffect
	whole       []effectpreparation.ManagedPathEffect
	aggregates  []effectpreparation.AggregateEffect
	narrowings  []reconcile.NativeNarrowingAction
	adoptions   []effectpreparation.NativeWholeAdoption
	orders      []pipackage.OrderObservation
}

func ProjectNativeManagedSuffix(input NativeApplyInput, prefix effectpreparation.NativeApplyStatePreparation, residual []effectpreparation.AggregateEffect, orders []pipackage.OrderObservation) (NativeManagedSuffixShape, error) {
	remaining, err := input.Preparation.ManagedRemainder(prefix, residual)
	if err != nil {
		return NativeManagedSuffixShape{}, err
	}
	projection := input
	projection.Preparation = remaining
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
	work.changed = len(work.managed)+len(work.whole)+len(residual)+len(projection.Preparation.Narrowings()) != 0
	return projectNativeSuffixWork(projection, work, orders)
}

// DelegatePrerequisites retains ordinary/MCP actors in their existing phase,
// leaving native retirement, ownership transitions and order for later.
func (input NativeApplyInput) DelegatePrerequisites() (NativeApplyInput, error) {
	preparation, err := input.Preparation.DelegatePrerequisites(input.CurrentState)
	if err != nil {
		return NativeApplyInput{}, err
	}
	input.Preparation = preparation
	return input, nil
}

// ProjectNativeDelegateSuffixes partitions an admitted suffix without inventing
// live intermediate observations. Both shapes require fresh input when bound.
func ProjectNativeDelegateSuffixes(input NativeApplyInput, prefix effectpreparation.NativeApplyStatePreparation, residual []effectpreparation.AggregateEffect, orders []pipackage.OrderObservation) (NativeManagedSuffixShape, NativeManagedSuffixShape, error) {
	prerequisiteState, lateState, err := input.Preparation.DelegatePhases(prefix, residual)
	if err != nil {
		return NativeManagedSuffixShape{}, NativeManagedSuffixShape{}, err
	}
	prerequisites, late := input, input
	prerequisites.Preparation, late.Preparation = prerequisiteState, lateState
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
	lastWork.changed = len(lastWork.managed)+len(lastWork.whole)+len(late.Preparation.AggregateEffects())+len(late.Preparation.Narrowings()) != 0
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

func (shape NativeManagedSuffixShape) Bind(input NativeApplyInput) (applyprogram.Plan, error) {
	if len(shape.plans) == 0 {
		return applyprogram.Plan{}, fmt.Errorf("native suffix shape is unavailable")
	}
	work, err := nativeApplyWorkFor(input)
	if err != nil {
		return applyprogram.Plan{}, err
	}
	if !slices.EqualFunc(shape.managed, work.managed, effectpreparation.ManagedPathEffect.Equal) ||
		!slices.EqualFunc(shape.whole, work.whole, effectpreparation.ManagedPathEffect.Equal) ||
		!slices.EqualFunc(shape.narrowings, input.Preparation.Narrowings(), reconcile.NativeNarrowingAction.Equal) ||
		!slices.EqualFunc(shape.adoptions, input.Preparation.WholeAdoptions(), effectpreparation.NativeWholeAdoption.Equal) ||
		!slices.EqualFunc(shape.aggregates, input.Preparation.AggregateEffects(), effectpreparation.AggregateEffect.SameMutation) {
		return applyprogram.Plan{}, fmt.Errorf("native suffix actors changed")
	}
	for _, order := range input.Preparation.PiOrders() {
		if !slices.ContainsFunc(shape.orders, order.SameSelection) {
			return applyprogram.Plan{}, fmt.Errorf("native suffix order selection changed")
		}
	}

	plan, err := prepareNativeApplyEffectPlan(input, work)
	if err != nil {
		return applyprogram.Plan{}, err
	}
	for _, reserved := range shape.plans {
		if reserved.SameStructure(plan) {
			return plan, nil
		}
	}
	return applyprogram.Plan{}, fmt.Errorf("native suffix structure changed")
}
