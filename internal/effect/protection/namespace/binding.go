package namespace

import (
	"fmt"
	"path/filepath"

	physicalhostpath "github.com/isty2e/daem/internal/assurance/observe/hostpath"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/protection"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// ResolveBindingDestination uses the selected or recorded physical point for
// Claude's global MCP binding. Settings bindings use an independent host lookup.
// Callers still qualify the resulting namespace and current binding cohort.
func ResolveBindingDestination(paths daempaths.Paths, destination output.Destination, physicalPoint string, budget rootedpath.PhysicalTraversalBudget) (string, error) {
	if destination.String() == aggregate.ClaudeGlobalMCPConfigPath {
		if !filepath.IsAbs(physicalPoint) {
			return "", protection.Refusal(protection.DocumentNamespaceUnqualified, fmt.Errorf("MCP binding requires its qualified absolute physical point"))
		}
		return mutation.CanonicalDirectoryEntryPathBounded(physicalPoint, physicalwork.MaximumPhysicalPathDepth, budget)
	}
	resolver := physicalhostpath.NewResolver(paths)
	ordinary, err := resolver.Resolve(destination, budget)
	if err != nil || destination.String() != aggregate.ClaudeGlobalHookConfigPath || paths.ClaudeHookSettings != "" || ordinary == physicalPoint {
		return ordinary, err
	}

	// With no hook plan, try the current profile before the historical home
	// binding. The recorded point distinguishes independently selected routes;
	// the caller still has to qualify the selected route against the binding.
	profile, err := resolver.ClaudeUserSettings(budget)
	if err != nil || profile == ordinary || profile == physicalPoint {
		return profile, err
	}
	resolved, observed, err := mutation.ObserveFutureReferentAuthorityBounded(profile, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return "", err
	}
	if exact, ok := observed.Exact(); resolved == physicalPoint || ok && exact.Key() == physicalPoint {
		return profile, nil
	}
	return ordinary, nil
}
