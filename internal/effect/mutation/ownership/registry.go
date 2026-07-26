package ownership

import (
	"context"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

// RegistryReader supplies current ownership claims for effect and recovery decisions.
type RegistryReader func(context.Context) (outputownership.Registry, error)

// RegistryStore applies exact ownership transitions to one durable registry.
type RegistryStore interface {
	Load(context.Context) (outputownership.Registry, error)
	Path() string
	Apply(
		context.Context,
		outputownership.ManagedAddress,
		outputownership.ClaimValue,
		outputownership.ClaimValue,
	) (outputownership.Registry, error)
}

// RootedRegistryBinder binds one registry store to retained physical-root authority.
type RootedRegistryBinder func(*rootedpath.CapturedRoot, rootedpath.Destination) (RegistryStore, error)
