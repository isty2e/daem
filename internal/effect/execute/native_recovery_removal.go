package execute

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func nativeRecoveryDestination(ctx context.Context, document journal.NativeRecoveryDocument, budget *recovery.PhysicalWorkBudget) (mutationDestination, error) {
	aliases := document.Namespace().Addresses()
	if len(aliases) == 0 {
		return mutationDestination{}, fmt.Errorf("native recovery document has no aliases")
	}
	alias := aliases[0]
	removal, present, err := document.Removal()
	if err != nil {
		return mutationDestination{}, err
	}
	if present {
		intent, err := removal.PhysicalIntent()
		if err != nil {
			return mutationDestination{}, err
		}
		found := false
		for _, candidate := range aliases {
			if candidate.Scope() == intent.Scope() && candidate.AggregateRoot() == intent.Destination() {
				alias = candidate
				found = true
				break
			}
		}
		if !found {
			return mutationDestination{}, fmt.Errorf("native physical removal has no actual alias")
		}
	}
	root, bound, err := document.Namespace().WriterBinding(ctx, budget)
	if err != nil {
		return mutationDestination{}, err
	}
	path, err := bound.LexicalPath()
	if err != nil {
		return mutationDestination{}, err
	}
	return mutationDestination{scope: alias.Scope(), logical: alias.AggregateRoot(), hostPath: path, root: root, destination: bound}, nil
}

func prepareNativeRecoveryRemovals(ctx context.Context, authority *mutationAuthority, operation *journal.NativeRecoveryOperation) error {
	plan, budget := operation.Plan(), operation.Budget()
	complete, err := plan.RemovalDemands()
	if err != nil {
		return err
	}
	if budget.RequireCapturePending() == nil {
		if err := budget.AdmitRemovalDemands(complete); err != nil {
			return err
		}
	}
	intents, err := plan.RemovalIntents()
	if err != nil {
		return err
	}
	physical, err := removalDemandSetFromIntents(intents)
	if err != nil {
		return err
	}
	destinations := make(map[removalRelationKey]mutationDestination, len(intents))
	var reachable []recovery.RemovalDemand
	for _, document := range operation.Documents() {
		removal, present, err := document.Removal()
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		intent, err := removal.PhysicalIntent()
		if err != nil {
			return err
		}
		destination, err := nativeRecoveryDestination(ctx, document, budget)
		if err != nil {
			return err
		}
		key := removalRelationKey{scope: intent.Scope(), destination: intent.Destination()}
		if _, duplicate := destinations[key]; duplicate {
			return fmt.Errorf("native recovery repeats a physical removal relation")
		}
		destinations[key] = destination
		if operation.InitialAssessment().RequiresRollback() && document.Restoration().Changed() && !document.Restoration().Document().Exists() {
			reachable = append(reachable, intent.Demand())
		}
	}
	authority.removalDemands = physical
	authority.removalDestinations = destinations
	authority.physicalWorkBudget = budget
	authority.removalBindingsPrepared = true
	if err := authority.bindRemovalIntentList(intents); err != nil {
		return err
	}
	reachableDemands, err := recovery.NewRemovalDemandSet(reachable)
	if err != nil {
		return err
	}
	return authority.prepareForwardRemovalReservationsFor(ctx, reachableDemands, nil)
}
