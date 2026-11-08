// Package physicalhostpath observes managed destinations with the caller's
// physical traversal allocation, without granting mutation authority.
package physicalhostpath

import (
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	daempaths "github.com/isty2e/daem/internal/paths"
)

type Resolver struct {
	paths daempaths.Paths
}

func NewResolver(paths daempaths.Paths) Resolver {
	return Resolver{paths: paths}
}

func (resolver Resolver) Resolve(destination output.Destination, budget rootedpath.PhysicalTraversalBudget) (string, error) {
	return resolver.ForBudget(budget).Resolve(destination)
}

// ForBudget adapts read-only consumers to the allocation already owned by their
// operation. Mutation binding instead supplies its current phase to Resolve.
func (resolver Resolver) ForBudget(budget rootedpath.PhysicalTraversalBudget) hostpath.Resolver {
	return managedhostpath.Resolver(resolver.paths, func(selector string) (string, error) {
		return rootedpath.ResolveRootSelectorBounded(selector, physicalwork.MaximumPhysicalPathDepth, budget)
	})
}
