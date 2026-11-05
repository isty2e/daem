package execute

import (
	"context"
	"fmt"

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
	physical, err := removalDemandSetFromIntents(intents)
	if err != nil {
		return recovery.RemovalDemandSet{}, err
	}
	destinations := make(map[removalRelationKey]mutationnamespace.Destination, len(intents))
	for _, intent := range intents {
		if err := authority.destinations.Bind(intent.Scope(), intent.Destination()); err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		destination, err := authority.destinations.Resolve(intent.Scope(), intent.Destination())
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		destinations[removalRelationKey{scope: intent.Scope(), destination: intent.Destination()}] = destination
	}
	seenNative := make(map[removalRelationKey]struct{})
	for _, document := range operation.Documents() {
		removal, present, err := document.Removal()
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		if !present {
			continue
		}
		intent, err := removal.PhysicalIntent()
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		destination, err := nativeRecoveryDestination(ctx, authority.destinations, document, budget)
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		key := removalRelationKey{scope: intent.Scope(), destination: intent.Destination()}
		if _, duplicate := seenNative[key]; duplicate {
			return recovery.RemovalDemandSet{}, fmt.Errorf("native recovery repeats a physical removal relation")
		}
		seenNative[key], destinations[key] = struct{}{}, destination
	}
	authority.removalDemands, authority.removalDestinations = physical, destinations
	authority.physicalWorkBudget, authority.removalBindingsPrepared = budget, true
	return physical, authority.bindRemovalIntentList(intents)
}
