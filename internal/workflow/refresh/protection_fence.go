package refresh

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/target"
)

type refreshProtectionFence struct {
	requests []mutation.PhysicalAuthorityRequest
	identity string
}

// observeRefreshProtectionFence also covers callers without native declarations:
// removing a declaration cannot authorize bypassing existing safety work.
func observeRefreshProtectionFence(ctx context.Context, paths daempaths.Paths, selected target.Target) (refreshProtectionFence, error) {
	budget := recovery.NewPhysicalCaptureBudget(nil)
	registry, err := ownershipstore.ReadProtectionRegistryBounded(ctx, paths.OwnershipRegistryPath, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return refreshProtectionFence{}, err
	}
	state, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, recovery.MaximumPhysicalPathDepth, budget)
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
	if selected == target.TargetPi {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			address, err := pipackage.SettingsDocumentAddress(scope)
			if err != nil {
				return refreshProtectionFence{}, err
			}
			path, err := managedhostpath.Resolver(paths).Resolve(address.AggregateRoot())
			if err != nil {
				return refreshProtectionFence{}, err
			}
			result.requests = append(result.requests, mutation.PhysicalAuthorityRequest{Path: path, Target: string(selected), Scope: string(scope)})
		}
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
