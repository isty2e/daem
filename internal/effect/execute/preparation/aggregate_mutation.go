package preparation

import (
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
)

// SameMutation compares logical actors, not unselected codec validation context.
func (effect AggregateEffect) SameMutation(other AggregateEffect) bool {
	if effect.documentAddress != other.documentAddress || effect.codecContractID != other.codecContractID {
		return false
	}
	validationOnly := func(projection AggregateProjectionEffect) bool { return !projection.IsJournaled() }
	leftActors := slices.DeleteFunc(slices.Clone(effect.projections), validationOnly)
	rightActors := slices.DeleteFunc(slices.Clone(other.projections), validationOnly)
	return slices.EqualFunc(leftActors, rightActors, func(a, b AggregateProjectionEffect) bool {
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
