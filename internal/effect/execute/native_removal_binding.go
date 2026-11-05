package execute

import (
	"context"

	"github.com/isty2e/daem/internal/effect/execute/removaloperation"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
)

func bindNativeRemovalIntents(ctx context.Context, authority *mutationAuthority, operation *journal.NativeRecoveryOperation) (recovery.RemovalDemandSet, error) {
	plan, budget := operation.Plan(), operation.Budget()
	complete, err := plan.RemovalDemands()
	if err != nil {
		return recovery.RemovalDemandSet{}, err
	}
	if budget.RequireCapturePending() == nil {
		if err := budget.AdmitRemovalDemands(complete); err != nil {
			return recovery.RemovalDemandSet{}, err
		}
	}
	intents, err := plan.RemovalIntents()
	if err != nil {
		return recovery.RemovalDemandSet{}, err
	}
	physical, err := removaloperation.DemandsForIntents(intents)
	if err != nil {
		return recovery.RemovalDemandSet{}, err
	}
	destinations := make([]mutationnamespace.Destination, 0, len(intents))
	for _, intent := range intents {
		if err := authority.destinations.Bind(intent.Scope(), intent.Destination()); err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		destination, err := authority.destinations.Resolve(intent.Scope(), intent.Destination())
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		destinations = append(destinations, destination)
	}
	var native []mutationnamespace.Destination
	for _, document := range operation.Documents() {
		removal, present, err := document.Removal()
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		if !present {
			continue
		}
		if _, err := removal.PhysicalIntent(); err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		destination, err := nativeRecoveryDestination(ctx, authority.destinations, document, budget)
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		native = append(native, destination)
	}
	authority.physicalWorkBudget = budget
	return physical, authority.removals.PrepareNativeSource(physical, destinations, native, budget, intents)
}
