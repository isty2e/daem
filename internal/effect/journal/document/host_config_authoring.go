package document

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// RequireHostConfigAuthoringOnly proves that removing declaration intent has
// no selected Snapshot or own metadata release hidden behind an empty state row.
func RequireHostConfigAuthoringOnly(ctx context.Context, snapshot durable.Snapshot, id entity.ID, destination output.Destination, aliases []aggregate.DocumentAddress, owner stateauthority.Authority, registry ownership.Registry, scope ownership.DocumentScope) error {
	managements, err := HostConfigManagements(ctx, snapshot, id)
	if err != nil {
		return err
	}
	if len(managements) != 0 {
		return fmt.Errorf("authoring-only host-config removal has retained management")
	}
	index, err := IndexSnapshotForClaims(ctx, snapshot, NativeClaims)
	if err != nil {
		return err
	}
	expected, err := index.Footprints(ctx, aliases)
	if err != nil {
		return err
	}
	claims, err := scope.OwnClaims(registry, owner)
	if err != nil {
		return err
	}
	remaining := make(map[output.Footprint]struct{}, len(expected))
	for footprint := range expected {
		remaining[footprint] = struct{}{}
	}
	for _, claim := range claims {
		footprint := claim.Address().Footprint()
		if _, present := remaining[footprint]; !present || claim.State() != ownership.ClaimActive {
			return fmt.Errorf("authoring-only host-config removal has orphan or reserved own metadata")
		}
		delete(remaining, footprint)
	}
	if len(remaining) != 0 {
		return fmt.Errorf("remaining managed document members lack their own claims")
	}
	destinations, err := index.NativeDestinations(ctx)
	if err != nil {
		return err
	}
	_, retainedNative := destinations[destination]
	bindingBefore := ownership.NoDocumentBinding()
	if binding, present := registry.DocumentBinding(owner.Key(), destination); present {
		bindingBefore, err = ownership.PresentDocumentBinding(binding)
		if err != nil {
			return err
		}
	}
	delta, err := ownership.NewOwnDocumentRelease(ownership.OwnDocumentReleaseInput{
		Owner: owner, Scope: scope, BeforeClaims: claims, BindingDestination: destination,
		BindingBefore: bindingBefore, RetainBinding: retainedNative,
	})
	if err != nil {
		return err
	}
	if err := delta.RequireBefore(registry); err != nil {
		return err
	}
	_, changed, err := delta.ApplyAfterCommit(registry)
	if err != nil {
		return err
	}
	if changed {
		return fmt.Errorf("authoring-only host-config removal would release own metadata")
	}
	return ctx.Err()
}
