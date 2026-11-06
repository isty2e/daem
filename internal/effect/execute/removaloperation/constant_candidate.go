package removaloperation

import (
	"context"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func ConstantForwardCandidate(relation Relation, state recovery.RemovalState, entries int, bytes int64) ForwardCandidate {
	return ForwardCandidate{
		Relation: relation, State: state,
		Measure: func(context.Context, workmeasure.ArtifactWork) (workmeasure.ArtifactWork, error) {
			return workmeasure.NewArtifactWork(entries, bytes)
		},
	}
}

func DemandsContainState(demands recovery.RemovalDemandSet, relation Relation, state recovery.RemovalState) bool {
	for _, demand := range demands.Demands() {
		if demand.Scope() != relation.Scope || demand.Destination() != relation.Destination {
			continue
		}
		for _, admitted := range demand.States() {
			if admitted.Equal(state) {
				return true
			}
		}
	}
	return false
}
