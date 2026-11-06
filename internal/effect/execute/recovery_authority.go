package execute

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/execute/removaloperation"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/target"
)

func newRecoveryMutationAuthority(
	paths Paths,
	plan recovery.Plan,
	resolver mutationnamespace.Resolver,
	filesystem mutationfs.Store,
	ownershipRegistryBinder ownershipmutation.RootedRegistryBinder,
) (*mutationAuthority, error) {
	demands, err := removaloperation.DemandsForIntents(plan.RemovalIntents())
	if err != nil {
		return nil, err
	}
	physicalWorkBudget, err := physicalwork.NewBudget(demands.Len())
	if err != nil {
		return nil, err
	}
	authority, err := captureMutationAuthorityWithPhysicalWorkBudget(
		paths,
		true,
		nil,
		resolver,
		filesystem,
		physicalWorkBudget,
	)
	if err != nil {
		return nil, err
	}
	authority.ownershipRegistryBinder = ownershipRegistryBinder
	if err := authority.removals.PrepareDemands(demands, physicalWorkBudget); err != nil {
		_ = authority.close()
		return nil, err
	}
	for _, action := range plan.GuardedActions() {
		destination, err := hostmutation.ParseRestorationDestination(action.Scope, action.Destination)
		if err != nil {
			_ = authority.close()
			return nil, err
		}
		targets := append([]target.Target(nil), action.ConsumerTargets...)
		if action.Target != "" {
			targets = []target.Target{action.Target}
		}
		if err := authority.bindPhysicalAuthority(action.Scope, destination, targets); err != nil {
			_ = authority.close()
			return nil, err
		}
	}
	return authority, nil
}

func requireRecoveryGlobalBindings(authority *mutationAuthority, actions []recovery.Action) error {
	if authority == nil {
		return fmt.Errorf("recovery mutation authority is required")
	}
	for _, action := range actions {
		if action.Scope != target.ScopeGlobal {
			continue
		}
		destination, err := hostmutation.ParseRestorationDestination(action.Scope, action.Destination)
		if err != nil {
			return err
		}
		if !authority.destinations.IsGlobalBound(destination) {
			return fmt.Errorf("global recovery destination %q was not bound before effects", action.Destination)
		}
	}
	return nil
}
