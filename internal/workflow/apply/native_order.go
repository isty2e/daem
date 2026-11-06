package apply

import (
	"bytes"
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/effect/execute/configrelation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
)

func prepareNativeOrderConvergence(ctx context.Context, paths daempaths.Paths, locked lock.File, reconciliation reconcile.Result, orders []pipackage.OrderObservation, options runOptions) (relationOrderRunResult, error) {
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
		if !relationOrderMutationRequired(class.decisions) {
			continue
		}
		actual, pi := class.observation.Pi()
		if !pi {
			return prepared, fmt.Errorf("native order preparation changed its host family")
		}
		found := false
		for _, planned := range orders {
			if planned.Sequence().SequenceID() != actual.Sequence().SequenceID() {
				continue
			}
			input, err := actual.CaptureInput()
			if err != nil {
				return prepared, err
			}
			if err := planned.VerifyBaseline(input.Document.Content(), input.Document.Exists()); err != nil {
				return prepared, err
			}
			left, leftExists := planned.Candidate()
			right, rightExists := actual.Candidate()
			if planned.SettingsPath() != actual.SettingsPath() || leftExists != rightExists || !bytes.Equal(left, right) {
				return prepared, fmt.Errorf("native Pi order differs from its reserved candidate")
			}
			found = true
			matched++
		}
		if !found {
			return prepared, fmt.Errorf("native Pi order has no reserved actor")
		}
	}
	if matched != expectedMutations {
		return prepared, fmt.Errorf("native Pi order actors changed before publication")
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
