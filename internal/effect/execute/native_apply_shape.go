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
}

func ProjectNativeManagedSuffix(input NativeApplyInput, prefix NativeApplyStatePreparation) (NativeManagedSuffixShape, error) {
	work, err := nativeApplyWorkFor(input)
	if err != nil {
		return NativeManagedSuffixShape{}, err
	}
	if err := prefix.Validate(); err != nil {
		return NativeManagedSuffixShape{}, err
	}
	if !prefix.BeforeState().Equal(input.CurrentState) || len(prefix.ManagedPathEffects()) != 0 ||
		len(input.Preparation.ProjectCarrierCompletions()) != 0 || len(work.ordinaryAggregates) != 0 ||
		!slices.EqualFunc(prefix.AfterState().ManagedAggregates(), input.Preparation.AfterState().ManagedAggregates(), durable.ManagedAggregateState.Equal) {
		return NativeManagedSuffixShape{}, fmt.Errorf("native suffix requires unchanged managed-path actors after a positive-only prefix")
	}
	for _, effect := range input.Preparation.AggregateEffects() {
		if _, native := aggregate.HostConfigSyntaxForCodec(effect.CodecContractID()); !native {
			return NativeManagedSuffixShape{}, fmt.Errorf("native managed suffix cannot contain aggregate work")
		}
	}
	prefixInput := input
	prefixInput.Preparation = prefix
	prefixWork, err := nativeApplyWorkFor(prefixInput)
	if err != nil {
		return NativeManagedSuffixShape{}, err
	}

	for index, namespace := range work.documents.Namespaces() {
		work.mutating[index] = false
		if !prefixWork.changed {
			continue
		}
		for prefixIndex, selected := range prefixWork.documents.Namespaces() {
			if slices.Equal(namespace.Addresses(), selected.Addresses()) && prefixWork.promoting[prefixIndex] {
				work.promoting[index] = false
			}
		}
	}
	work.edits = nil
	work.changed = len(work.managed) != 0
	plan, err := prepareNativeApplyEffectPlan(input, work)
	if err != nil {
		return NativeManagedSuffixShape{}, err
	}
	validations, err := maximumNativeWorkValidationCount(input, work)
	if err != nil {
		return NativeManagedSuffixShape{}, err
	}
	return NativeManagedSuffixShape{plan: plan, validations: validations, managed: slices.Clone(work.managed)}, nil
}

func (shape NativeManagedSuffixShape) Segment() operationplan.EffectNode { return shape.plan.Segment() }

func (shape NativeManagedSuffixShape) ForwardValidationCount() int { return shape.validations }

func (shape NativeManagedSuffixShape) Bind(input NativeApplyInput) (ApplyEffectPlan, error) {
	if !shape.plan.valid {
		return ApplyEffectPlan{}, fmt.Errorf("native managed suffix shape is unavailable")
	}
	work, err := nativeApplyWorkFor(input)
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	if len(input.Preparation.AggregateEffects()) != 0 || !slices.EqualFunc(shape.managed, work.managed, sameManagedPathEffect) {
		return ApplyEffectPlan{}, fmt.Errorf("native managed suffix actors changed")
	}
	plan, err := prepareNativeApplyEffectPlan(input, work)
	if err != nil {
		return ApplyEffectPlan{}, err
	}
	if !shape.plan.structure.Equal(plan.structure) {
		return ApplyEffectPlan{}, fmt.Errorf("native managed suffix structure changed")
	}
	return plan, nil
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
