package claimoperation

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

type provisionalRestorationOutputKey struct {
	destination string
	footprint   output.Footprint
}

func ProvisionalRestorationGuard(intents []ownership.ProvisionalAcquireIntent, read func(context.Context) (ownership.Registry, error), budget rootedpath.PhysicalTraversalBudget) hostmutation.RestorationOwnershipGuard {
	if len(intents) == 0 {
		return nil
	}
	byOutput := make(map[provisionalRestorationOutputKey]ownership.ProvisionalAcquireIntent, len(intents))
	for _, intent := range intents {
		byOutput[provisionalRestorationOutputKey{destination: intent.Destination().String(), footprint: intent.Footprint()}] = intent
	}
	return func(ctx context.Context, action hostmutation.RestorationAction, destination mutationnamespace.Destination) error {
		footprint, err := output.FootprintFromLegacy(output.ContentPath(action.ContentPath))
		if err != nil {
			return fmt.Errorf("provisional recovery action scope: %w", err)
		}
		intent, present := byOutput[provisionalRestorationOutputKey{destination: action.Destination, footprint: footprint}]
		if !present {
			return nil
		}
		if read == nil {
			return fmt.Errorf("ownership registry is required for provisional recovery rollback")
		}
		observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(destination.HostPath(), physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		registry, err := read(ctx)
		if err != nil {
			return err
		}
		if exact, ok := observation.Exact(); ok {
			address, err := ownership.NewManagedAddressFromLegacy(exact, action.ContentPath)
			if err != nil {
				return err
			}
			if err := intent.AdmitAddress(address); err != nil {
				return fmt.Errorf("provisional recovery path authority changed: %w", err)
			}
			if claim, conflict := registry.Conflict(address); conflict {
				actual, _ := ownership.PresentClaim(claim)
				return &ownership.StaleClaimError{Address: address, Expected: ownership.NoClaim(), Actual: actual}
			}
			return nil
		}
		provisional, ok := observation.Provisional()
		if !ok || !provisional.Equal(intent.Path()) {
			return fmt.Errorf("provisional recovery path authority changed before rollback")
		}
		if claim, conflict := registry.ProvisionalAncestorConflict(provisional); conflict {
			return fmt.Errorf("provisional recovery output overlaps a durable claim owned by manifest %q", claim.Owner().ManifestPath())
		}
		return fmt.Errorf("provisional recovery output is no longer exactly visible before rollback")
	}
}
