package clipresent

import (
	"cmp"
	"slices"

	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

type humanResourceOrder struct {
	reference  string
	targetRank int
	scope      target.Scope
}

func humanResourceOrderFor(reference string, consumers []target.Target, scope target.Scope) humanResourceOrder {
	supported := target.SupportedTargets()
	rank := len(supported)
	if len(consumers) == 0 {
		rank = -1
	}
	for index, selected := range supported {
		if slices.Contains(consumers, selected) {
			rank = index
			break
		}
	}
	return humanResourceOrder{reference: reference, targetRank: rank, scope: scope}
}

func (left humanResourceOrder) compare(right humanResourceOrder) int {
	if left.reference == "" && right.reference == "" {
		return 0
	}
	if left.reference == "" {
		return 1
	}
	if right.reference == "" {
		return -1
	}
	return cmp.Or(
		cmp.Compare(left.reference, right.reference),
		cmp.Compare(left.targetRank, right.targetRank),
		cmp.Compare(left.scope, right.scope),
	)
}

func sortedHumanRows[T any](values []T, orderFor func(T) humanResourceOrder) []T {
	type row struct {
		value T
		order humanResourceOrder
	}
	rows := make([]row, len(values))
	for index, value := range values {
		rows[index] = row{value: value, order: orderFor(value)}
	}
	slices.SortStableFunc(rows, func(left, right row) int { return left.order.compare(right.order) })
	result := make([]T, len(rows))
	for index, row := range rows {
		result[index] = row.value
	}
	return result
}

func humanDecisionOrder(decision reconcile.Decision) humanResourceOrder {
	if managed, ok := decision.ManagedPath(); ok {
		label, reference := projectionIdentityLabel(managed.Subject())
		if label != "resource" {
			reference = ""
		}
		return humanResourceOrderFor(reference, managedPathConsumers(managed), managed.Scope())
	}
	aggregate, _ := decision.Aggregate()
	label, reference := projectionIdentityLabel(aggregate.Subject())
	if label != "resource" {
		reference = ""
	}
	return humanResourceOrderFor(reference, []target.Target{aggregate.Target()}, aggregate.Scope())
}

func managedPathConsumers(decision reconcile.ManagedPathDecision) []target.Target {
	consumers := decision.ConsumerTargets()
	if len(consumers) == 0 {
		if previous, present := decision.PreviousState(); present {
			consumers = previous.ConsumerTargets()
		}
	}
	return consumers
}
