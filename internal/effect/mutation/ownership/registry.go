package ownership

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
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
// Convergence selects successor members from an operation's observed reference
// pool; supplied references cannot change facts or replace retained locators.
type RegistryStore interface {
	RegistryReader
	Path() string
	Converge(context.Context, outputownership.ClaimConvergence, ...pathauthority.Reference) (outputownership.Registry, error)
	ConvergeRegistry(
		context.Context,
		outputownership.ClaimConvergence,
		outputownership.DocumentBindingConvergence,
		...pathauthority.Reference,
	) (outputownership.Registry, error)
}

// RootedRegistryBinder binds one registry store to retained physical-root
// authority and a mandatory operation-wide traversal budget.
type RootedRegistryBinder func(
	root *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (RegistryStore, error)

// OwnReleaseRegistryStore cannot acquire claims or host-write authority. Its
// caller owns the settled release-only intent and the complete lease lifetime.
type OwnReleaseRegistryStore interface {
	LoadRecordedRegistry(context.Context) (outputownership.Registry, error)
	ConvergeOwnReleaseAfterCommit(context.Context, outputownership.OwnDocumentRelease) (outputownership.Registry, error)
}

type OwnReleaseRegistryBinder func(*rootedpath.CapturedRoot, rootedpath.Destination, int, rootedpath.PhysicalTraversalBudget) (OwnReleaseRegistryStore, error)
