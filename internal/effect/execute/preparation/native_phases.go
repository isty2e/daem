package preparation

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
)

// ValidateAggregatePhaseSplit checks complete state equations, not live observations.
func (preparation NativeApplyStatePreparation) ValidateAggregatePhaseSplit(prefix NativeApplyStatePreparation, residual []AggregateEffect) error {
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

func (preparation NativeApplyStatePreparation) ManagedRemainder(prefix NativeApplyStatePreparation, residual []AggregateEffect) (NativeApplyStatePreparation, error) {
	if err := preparation.ValidateAggregatePhaseSplit(prefix, residual); err != nil {
		return NativeApplyStatePreparation{}, err
	}
	remaining := preparation
	remaining.aggregates = slices.Clone(residual)
	remaining.orders = nil
	remaining.narrowings = preparation.narrowingPhase(false)
	return remaining, nil
}

// DelegatePrerequisites retains ordinary actors without inventing a live baseline.
func (preparation NativeApplyStatePreparation) DelegatePrerequisites(baseline durable.Snapshot) (NativeApplyStatePreparation, error) {
	if err := preparation.Validate(); err != nil {
		return NativeApplyStatePreparation{}, err
	}
	preparation.managed = slices.DeleteFunc(slices.Clone(preparation.managed), func(effect ManagedPathEffect) bool {
		return aggregate.IsHostConfigDocumentSubject(effect.Subject())
	})
	preparation.aggregates = slices.DeleteFunc(slices.Clone(preparation.aggregates), func(effect AggregateEffect) bool {
		_, native := aggregate.HostConfigSyntaxForCodec(effect.CodecContractID())
		return native
	})
	preparation.narrowings, preparation.adoptions, preparation.orders, preparation.projectCompletions, preparation.nativeOperations = nil, nil, nil, nil, nil
	after, err := preparation.managedSuccessor(baseline)
	if err != nil {
		return NativeApplyStatePreparation{}, err
	}
	if after.Equal(baseline) && len(preparation.managed)+len(preparation.aggregates) != 0 {
		after, err = after.AdvanceNativeDocumentCommit()
		if err != nil {
			return NativeApplyStatePreparation{}, err
		}
	}
	preparation.after = after
	return preparation, nil
}

// DelegatePhases preserves the full successor while separating prerequisite actors.
func (preparation NativeApplyStatePreparation) DelegatePhases(prefix NativeApplyStatePreparation, residual []AggregateEffect) (NativeApplyStatePreparation, NativeApplyStatePreparation, error) {
	if err := preparation.ValidateAggregatePhaseSplit(prefix, residual); err != nil {
		return NativeApplyStatePreparation{}, NativeApplyStatePreparation{}, err
	}
	prerequisites, err := preparation.DelegatePrerequisites(prefix.after)
	if err != nil {
		return NativeApplyStatePreparation{}, NativeApplyStatePreparation{}, err
	}
	late := preparation
	late.managed = slices.DeleteFunc(slices.Clone(preparation.managed), func(effect ManagedPathEffect) bool {
		return !aggregate.IsHostConfigDocumentSubject(effect.Subject())
	})
	late.aggregates = slices.DeleteFunc(slices.Clone(residual), func(effect AggregateEffect) bool {
		_, native := aggregate.HostConfigSyntaxForCodec(effect.CodecContractID())
		return !native
	})
	late.narrowings, late.orders = preparation.narrowingPhase(false), nil
	projected, err := late.managedSuccessor(prerequisites.after)
	if err != nil {
		return NativeApplyStatePreparation{}, NativeApplyStatePreparation{}, err
	}
	if !slices.EqualFunc(projected.ManagedPaths(), preparation.after.ManagedPaths(), durable.ManagedPathState.Equal) || !slices.EqualFunc(projected.ManagedAggregates(), preparation.after.ManagedAggregates(), durable.ManagedAggregateState.Equal) {
		return NativeApplyStatePreparation{}, NativeApplyStatePreparation{}, fmt.Errorf("delegate phases do not preserve the complete managed successor")
	}
	return prerequisites, late, nil
}

func (adoption NativeWholeAdoption) Equal(other NativeWholeAdoption) bool {
	return adoption.source.Equal(other.source) && adoption.record.Equal(other.record) &&
		slices.EqualFunc(adoption.retired, other.retired, durable.ManagedAggregateState.Equal)
}
