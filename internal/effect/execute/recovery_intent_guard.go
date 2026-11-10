package execute

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/execute/claimoperation"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
)

func recoveryIntentClaimGuard(owner stateauthority.Key, intents []ownership.ProvisionalAcquireIntent, authority *mutationAuthority) hostmutation.RestorationOwnershipGuard {
	var read func(context.Context) (ownership.Registry, error)
	if authority != nil && authority.rootedOwnershipRegistryOption() != nil {
		read = authority.rootedOwnershipRegistryOption().Load
	}
	var budget rootedpath.PhysicalTraversalBudget
	if authority != nil {
		budget = authority.generalTraversalPhase
	}
	return claimoperation.RestorationGuard(owner, intents, read, budget)
}
