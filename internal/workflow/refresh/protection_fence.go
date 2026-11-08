package refresh

import (
	"context"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

type refreshProtectionFence struct {
	requests []mutation.PhysicalAuthorityRequest
	identity string
}

// observeRefreshProtectionFence also covers callers without native declarations:
// removing a declaration cannot authorize bypassing existing safety work.
func observeRefreshProtectionFence(ctx context.Context, paths daempaths.Paths, profile relationhost.NativeSettingsProfile) (refreshProtectionFence, error) {
	budget := physicalwork.NewCaptureBudget(nil)
	registry, err := ownershipstore.ReadProtectionRegistryBounded(ctx, paths.OwnershipRegistryPath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return refreshProtectionFence{}, err
	}
	state, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return refreshProtectionFence{}, err
	}
	owner, err := stateauthority.New(state.Exact(), paths.ManifestPath)
	if err != nil {
		return refreshProtectionFence{}, err
	}
	if err := registry.RequireOwnerSettled(owner.Key()); err != nil {
		return refreshProtectionFence{}, err
	}
	var result refreshProtectionFence
	opaque, err := profile.FencePaths(paths, budget)
	if err != nil {
		return refreshProtectionFence{}, err
	}
	for _, namespace := range opaque {
		result.requests = append(result.requests, mutation.PhysicalAuthorityRequest{Path: namespace.Path(), Target: string(namespace.Target()), Scope: string(namespace.Scope())})
	}
	physical, err := mutation.NewPhysicalAuthoritySet(result.requests...)
	if err != nil {
		return refreshProtectionFence{}, err
	}
	scope, err := recoverygate.DocumentScope(physical)
	if err != nil {
		return refreshProtectionFence{}, err
	}
	if err := scope.RequireWritable(registry); err != nil {
		return refreshProtectionFence{}, err
	}
	view, err := registry.Documents(scope)
	if err != nil {
		return refreshProtectionFence{}, err
	}
	result.identity, err = view.Fingerprint()
	return result, err
}
