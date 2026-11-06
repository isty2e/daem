package execute

import (
	"context"

	"github.com/isty2e/daem/internal/effect/execute/claimoperation"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
)

func recoveryIntentClaimGuard(intents []ownership.ProvisionalAcquireIntent, store ownershipmutation.RegistryStore, authority *mutationAuthority) hostmutation.RestorationOwnershipGuard {
	var read func(context.Context) (ownership.Registry, error)
	if store != nil {
		read = store.Load
	}
	var budget rootedpath.PhysicalTraversalBudget
	if authority != nil {
		budget = authority.generalTraversalPhase
	}
	return claimoperation.ProvisionalRestorationGuard(intents, read, budget)
}
