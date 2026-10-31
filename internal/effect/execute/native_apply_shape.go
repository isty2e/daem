package execute

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// NativeManagedSuffixShape describes residual work, never a live observation or
// executable plan. Bind requires fresh complete input after the preceding effects.
type NativeManagedSuffixShape struct {
	plan        ApplyEffectPlan
	validations int
	managed     []ManagedPathEffect
	aggregates  []AggregateEffect
}

// AggregateEffectsAfterPositivePrefix partitions effects without manufacturing a
// post-prefix observation or granting execution authority.
func (preparation NativeApplyStatePreparation) AggregateEffectsAfterPositivePrefix(prefix NativeApplyStatePreparation) ([]AggregateEffect, error) {
	if err := preparation.Validate(); err != nil {
		return nil, err
	}
	if err := prefix.Validate(); err != nil {
		return nil, err
	}
	if !prefix.before.Equal(preparation.before) || len(prefix.managed)+len(prefix.narrowings)+len(prefix.adoptions)+len(prefix.projectCompletions)+len(preparation.projectCompletions) != 0 {
		return nil, fmt.Errorf("native suffix requires a positive-only prefix from the same complete Before")
	}
	for _, effect := range prefix.aggregates {
		if _, native := aggregate.HostConfigSyntaxForCodec(effect.CodecContractID()); !native {
			return nil, fmt.Errorf("positive native prefix contains ordinary aggregate work")
		}
	}
	var residual []AggregateEffect
	for _, effect := range preparation.aggregates {
		if _, native := aggregate.HostConfigSyntaxForCodec(effect.CodecContractID()); !native {
			residual = append(residual, effect)
		}
	}
	after, err := snapshotAfterAggregateEffects(prefix.after, residual)
	if err != nil {
		return nil, err
	}
	if !slices.EqualFunc(after.ManagedAggregates(), preparation.after.ManagedAggregates(), durable.ManagedAggregateState.Equal) {
		return nil, fmt.Errorf("positive native prefix does not settle every native aggregate effect")
	}
	return residual, nil
}

func ProjectNativeManagedSuffix(input NativeApplyInput, prefix NativeApplyStatePreparation) (NativeManagedSuffixShape, error) {
	residual, err := input.Preparation.AggregateEffectsAfterPositivePrefix(prefix)
	if err != nil {
		return NativeManagedSuffixShape{}, err
	}
	projection := input
	projection.Preparation.aggregates = residual
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

	for index, namespace := range work.documents.Namespaces() {
		if !prefixWork.changed {
			continue
		}
		for prefixIndex, selected := range prefixWork.documents.Namespaces() {
			if slices.Equal(namespace.Addresses(), selected.Addresses()) && prefixWork.promoting[prefixIndex] {
				work.promoting[index] = false
			}
		}
	}
	work.changed = len(work.managed)+len(residual) != 0
	plan, err := prepareNativeApplyEffectPlan(input, work)
	if err != nil {
		return NativeManagedSuffixShape{}, err
	}
	validations, err := maximumNativeWorkValidationCount(input, work)
	if err != nil {
		return NativeManagedSuffixShape{}, err
	}
	return NativeManagedSuffixShape{plan: plan, validations: validations, managed: slices.Clone(work.managed), aggregates: residual}, nil
}

func (shape NativeManagedSuffixShape) Segment() operationplan.EffectNode { return shape.plan.Segment() }

func (shape NativeManagedSuffixShape) ForwardValidationCount() int { return shape.validations }

func (shape NativeManagedSuffixShape) Bind(input NativeApplyInput) (ApplyEffectPlan, error) {
	if !shape.plan.valid {
		return ApplyEffectPlan{}, fmt.Errorf("native suffix shape is unavailable")
	}
	work, err := nativeApplyWorkFor(input)
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	if !slices.EqualFunc(shape.managed, work.managed, sameManagedPathEffect) ||
		!slices.EqualFunc(shape.aggregates, input.Preparation.AggregateEffects(), sameAggregateSuffixEffect) {
		return ApplyEffectPlan{}, fmt.Errorf("native suffix actors changed")
	}
	plan, err := prepareNativeApplyEffectPlan(input, work)
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	if !shape.plan.structure.Equal(plan.structure) {
		return ApplyEffectPlan{}, fmt.Errorf("native suffix structure changed")
	}
	return plan, nil
}

func sameAggregateSuffixEffect(left, right AggregateEffect) bool {
	if left.documentAddress != right.documentAddress || left.codecContractID != right.codecContractID {
		return false
	}
	return slices.EqualFunc(left.projections, right.projections, func(a, b AggregateProjectionEffect) bool {
		if a.kind != b.kind || !a.contract.Equal(b.contract) ||
			!a.before.SelectedValueEqual(b.before) || !a.expected.SelectedValueEqual(b.expected) ||
			!slices.EqualFunc(a.previous, b.previous, durable.ManagedAggregateState.Equal) ||
			!slices.EqualFunc(a.subjects, b.subjects, func(a, b AggregateSubjectEffect) bool {
				return a.kind == b.kind && a.subject == b.subject && a.contract.Equal(b.contract)
			}) {
			return false
		}
		if a.desired == nil || b.desired == nil {
			return a.desired == nil && b.desired == nil
		}
		return a.desired.Equal(*b.desired)
	})
}

func sameManagedPathEffect(left, right ManagedPathEffect) bool {
	a, b := left.facts(), right.facts()
	if left.Kind() != right.Kind() || a.subject != b.subject || !slices.Equal(a.consumerTargets, b.consumerTargets) ||
		a.scope != b.scope || a.destination != b.destination || a.desiredHash != b.desiredHash || a.liveHash != b.liveHash ||
		a.contentKind != b.contentKind || a.permissionPolicy != b.permissionPolicy ||
		a.desiredFileMode != b.desiredFileMode || a.liveFileMode != b.liveFileMode {
		return false
	}
	if a.previous == nil || b.previous == nil {
		return a.previous == nil && b.previous == nil
	}
	return a.previous.Equal(*b.previous)
}
