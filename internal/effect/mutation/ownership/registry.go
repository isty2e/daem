package ownership

import (
	"context"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

// RegistryReader supplies current ownership claims for effect and recovery decisions.
type RegistryReader interface {
	Load(context.Context) (outputownership.Registry, error)
	LoadForClaimRemovals(
		ctx context.Context,
		expected []outputownership.Claim,
		maximumPhysicalDepth int,
		budget rootedpath.PhysicalTraversalBudget,
	) (outputownership.Registry, error)
}

// RegistryStore applies exact ownership transitions to one durable registry.
type RegistryStore interface {
	RegistryReader
	Path() string
	Converge(context.Context, outputownership.ClaimConvergence) (outputownership.Registry, error)
}

// RootedRegistryBinder binds one registry store to retained physical-root
// authority and a mandatory operation-wide traversal budget.
type RootedRegistryBinder func(
	root *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (RegistryStore, error)
