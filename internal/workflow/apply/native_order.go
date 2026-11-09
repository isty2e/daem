package apply

import (
	"context"
	"fmt"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/effect/execute/configrelation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
)

func prepareNativeOrderConvergence(ctx context.Context, paths daempaths.Paths, locked lock.File, reconciliation reconcile.Result, orders []relationhost.SelectedDocumentEdit, options runOptions) (relationOrderRunResult, error) {
	classes := make(map[hostrelation.OrderClassID]struct{}, len(orders))
	expectedMutations := 0
	for _, order := range orders {
		classes[order.Sequence().ClassID()] = struct{}{}
		if order.Changed() {
			expectedMutations++
		}
	}
	var selected []reconcile.RelationOrderDecision
	for _, decision := range reconciliation.RelationOrders() {
		if _, owned := classes[decision.ClassID()]; owned {
			selected = append(selected, decision)
		}
	}
	reconciliation, err := reconciliation.WithRelationOrders(selected)
	if err != nil {
		return relationOrderRunResult{}, err
	}
	prepared, err := prepareRelationOrderConvergence(ctx, paths, locked, reconciliation, options)
	if err != nil {
		return prepared, err
	}
	matched := 0
	for _, class := range prepared.observed {
		actualEdits, err := class.observation.SelectedEdits(reconciliation.CarrierRetirementClaims())
		if err != nil {
			return prepared, err
		}
		for _, actual := range actualEdits {
			if !actual.Changed() {
				continue
			}
			found := false
			for _, planned := range orders {
				if planned.Sequence().SequenceID() != actual.Sequence().SequenceID() {
					continue
				}
				if !planned.SameMutation(actual) {
					return prepared, fmt.Errorf("native selected mutation differs from its reserved actor")
				}
				found, matched = true, matched+1
			}
			if !found {
				return prepared, fmt.Errorf("native selected mutation has no reserved actor")
			}
		}
	}
	if matched != expectedMutations {
		return prepared, fmt.Errorf("native selected actors changed before publication")
	}
	return prepared, nil
}

func recordNativeOrderEvents(result *relationOrderRunResult, kind configrelation.OrderSequenceEventKind, changed bool, cause error, options runOptions) {
	indexes := make(map[hostrelation.PhysicalSequenceID]int, len(result.results))
	for index, row := range result.results {
		indexes[row.sequenceID] = index
	}
	for _, class := range result.observed {
		for _, decision := range class.decisions {
			if !decision.RequiresMutation() {
				continue
			}
			recordRelationOrderEvent(configrelation.OrderSequenceEvent{
				Kind: kind, SequenceID: decision.SequenceID(), Changed: changed, Err: cause,
			}, class.decisions, result.results, indexes, options.ExecuteEvents)
		}
	}
}
