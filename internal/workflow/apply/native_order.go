package apply

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/effect/execute/configrelation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type relationOrderSequenceKey struct {
	classID    hostrelation.OrderClassID
	sequenceID hostrelation.PhysicalSequenceID
}

func selectedOrderSequenceKey(edit relationhost.SelectedDocumentEdit) relationOrderSequenceKey {
	return relationOrderSequenceKey{edit.Sequence().ClassID(), edit.Sequence().SequenceID()}
}

func prepareNativeOrderConvergence(ctx context.Context, paths daempaths.Paths, locked lock.File, reconciliation reconcile.Result, orders []relationhost.SelectedDocumentEdit, options runOptions) (relationOrderRunResult, error) {
	wanted := make(map[relationOrderSequenceKey]relationhost.SelectedDocumentEdit, len(orders))
	classes := make(map[hostrelation.OrderClassID]relationhost.SelectedDocumentEdit)
	hasOrder := false
	for _, order := range orders {
		key := selectedOrderSequenceKey(order)
		if _, duplicate := wanted[key]; duplicate {
			return relationOrderRunResult{}, fmt.Errorf("native selected sequence actor is duplicated")
		}
		wanted[key] = order
		classes[key.classID] = order
		_, ordered := order.SelectedSequence().Constraint()
		hasOrder = hasOrder || ordered
	}
	if hasOrder {
		if err := options.orderRiskBaseline.validate(); err != nil {
			return relationOrderRunResult{}, err
		}
	}
	var decisions []reconcile.RelationOrderDecision
	var observed []observedOrderClass
	matched := 0
	for _, constraint := range locked.Locked.OrderConstraints() {
		order, owned := classes[constraint.ClassID()]
		if !owned {
			continue
		}
		input := relationhost.OrderInput{Paths: paths, Lockfile: locked, Constraint: constraint}
		if order.DocumentAddress().Target() == target.TargetPi && options.nativeInput != nil {
			alias, err := pipackage.SettingsDocumentAddress(order.Scope())
			if err != nil {
				return relationOrderRunResult{}, err
			}
			if document, _, retained := options.nativeInput.Documents.Evidence().Document(alias); retained {
				input.PiDocument = &document
			}
		}
		observation, err := relationhost.ObserveOrder(input)
		if err != nil {
			var blocked []reconcile.RelationOrderDecision
			for _, reserved := range orders {
				if reserved.Sequence().ClassID() != constraint.ClassID() {
					continue
				}
				decision, buildErr := reconcile.NewBlockedRelationOrderDecision(reconcile.BlockedRelationOrderDecisionInput{
					Target: reserved.DocumentAddress().Target(), Scope: reserved.Scope(), Constraint: constraint,
					SequenceID: reserved.Sequence().SequenceID(), Reason: reconcile.RelationOrderObservationFailureReason(err), Detail: err.Error(),
				})
				if buildErr != nil {
					return relationOrderRunResult{}, buildErr
				}
				blocked = append(blocked, decision)
			}
			return prepareObservedRelationOrders(ctx, reconciliation, blocked, nil, options)
		}
		actual, err := observation.SelectedEdits(reconciliation.CarrierRetirementClaims())
		if err != nil {
			return relationOrderRunResult{}, err
		}
		classDecisions, count, err := matchNativeSelectedEdits(wanted, actual, reconciliation)
		if err != nil {
			return relationOrderRunResult{}, err
		}
		matched += count
		decisions = append(decisions, classDecisions...)
		observed = append(observed, observedOrderClass{observation: observation, decisions: classDecisions})
		delete(classes, constraint.ClassID())
	}
	for _, order := range orders {
		classID := order.Sequence().ClassID()
		if _, pending := classes[classID]; !pending {
			continue
		}
		if _, ordered := order.SelectedSequence().Constraint(); ordered || order.DocumentAddress().Target() != target.TargetOpenCode {
			return relationOrderRunResult{}, fmt.Errorf("native selected order class has no locked constraint")
		}
		actual, err := relationhost.ObserveOpenCodeRetirementEdits(paths, locked, order.Scope(), reconciliation.CarrierRetirementClaims())
		if err != nil {
			return relationOrderRunResult{}, err
		}
		classDecisions, count, err := matchNativeSelectedEdits(wanted, actual, reconciliation)
		if err != nil {
			return relationOrderRunResult{}, err
		}
		matched += count
		decisions = append(decisions, classDecisions...)
		delete(classes, classID)
	}
	if matched != len(wanted) {
		return relationOrderRunResult{}, fmt.Errorf("native selected actors changed before publication")
	}
	prepared, err := prepareObservedRelationOrders(ctx, reconciliation, decisions, observed, options)
	for _, order := range orders {
		prepared.nativeSequences = append(prepared.nativeSequences, selectedOrderSequenceKey(order))
	}
	return prepared, err
}

func requireNativeSelectedActors(checks, actors []relationhost.SelectedDocumentEdit) error {
	matched := 0
	for _, check := range checks {
		if !check.Changed() {
			continue
		}
		found := false
		for _, actor := range actors {
			if check.SameMutation(actor) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("native selected mutation has no bound publication actor")
		}
		matched++
	}
	if matched != len(actors) {
		return fmt.Errorf("bound native publication has an unqualified selected actor")
	}
	return nil
}

func matchNativeSelectedEdits(
	wanted map[relationOrderSequenceKey]relationhost.SelectedDocumentEdit,
	actual []relationhost.SelectedDocumentEdit,
	reconciliation reconcile.Result,
) ([]reconcile.RelationOrderDecision, int, error) {
	var decisions []reconcile.RelationOrderDecision
	matched := 0
	for _, edit := range actual {
		planned, reserved := wanted[selectedOrderSequenceKey(edit)]
		if !reserved {
			continue
		}
		if !planned.SameMutation(edit) {
			return nil, 0, fmt.Errorf("native selected mutation differs from its reserved actor")
		}
		matched++
		decision, admitted, err := readiness.DecideSelectedExtensionOrder(edit, reconciliation.Relations())
		if err != nil {
			return nil, 0, err
		}
		if admitted {
			decisions = append(decisions, decision)
		}
	}
	return decisions, matched, nil
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
