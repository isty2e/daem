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
// Claude's global MCP binding. Other bindings retain their owner's resolver.
// Callers still qualify the resulting namespace and current binding cohort.
func ResolveBindingDestination(paths daempaths.Paths, destination output.Destination, physicalPoint string, budget rootedpath.PhysicalTraversalBudget) (string, error) {
	if destination.String() == aggregate.ClaudeGlobalMCPConfigPath {
		if !filepath.IsAbs(physicalPoint) {
			return "", protection.Refusal(protection.DocumentNamespaceUnqualified, fmt.Errorf("MCP binding requires its qualified absolute physical point"))
		}
		return mutation.CanonicalDirectoryEntryPathBounded(physicalPoint, physicalwork.MaximumPhysicalPathDepth, budget)
	}
	return physicalhostpath.NewResolver(paths).Resolve(destination, budget)
}
