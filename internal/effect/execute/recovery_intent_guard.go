package execute

import (
	"context"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func recoveryIntentClaimGuard(intents []ownership.ProvisionalAcquireIntent, store ownershipmutation.RegistryStore, authority *mutationAuthority) recoveryHostOwnershipGuard {
	var read func(context.Context) (ownership.Registry, error)
	if store != nil {
		read = store.Load
	}
	return recoveryIntentClaimGuardWithReader(intents, read, authority)
}
