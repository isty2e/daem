package hostmutation

import (
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

func ProvisionalAcquireKeysForManagedPath(effect effectpreparation.ManagedPathEffect, phase ManagedPathPhase) []ownership.LogicalKey {
	if phase != ManagedPathPublishPhase || effect.Scope() != target.ScopeGlobal ||
		effect.Kind() == effectpreparation.ManagedPathEffectRemove || effect.Kind() == effectpreparation.ManagedPathEffectRecord {
		return nil
	}
	previous, present := effect.PreviousState()
	if present && previous.Scope() == effect.Scope() && previous.Destination() == effect.Destination() {
		return nil
	}
	return []ownership.LogicalKey{{Destination: effect.Destination(), Footprint: output.WholePathFootprint()}}
}

func ProvisionalAcquireKeysForAggregate(effect effectpreparation.AggregateEffect) []ownership.LogicalKey {
	if effect.Scope() != target.ScopeGlobal || effect.Kind() == effectpreparation.AggregateEffectRecord {
		return nil
	}
	projections := effect.ProjectionEffects()
	keys := make([]ownership.LogicalKey, 0, len(projections))
	for _, projection := range projections {
		if len(projection.Subjects()) == 0 || len(projection.PreviousStates()) != 0 {
			continue
		}
		keys = append(keys, ownership.LogicalKey{Destination: effect.Destination(), Footprint: projection.Contract().Address().Footprint()})
	}
	return keys
}
