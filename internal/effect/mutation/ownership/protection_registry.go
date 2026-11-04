package ownership

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

// ProtectionRegistryReader discovers safety work without qualifying unrelated
// claim paths or consulting the operation's current source declarations.
type ProtectionRegistryReader interface {
	LoadPendingProtections(context.Context) ([]outputownership.PendingProtection, error)
}

// ProtectionRegistryStore publishes only exact arm/bind/clear transitions.
// Filesystem authority, quiescence and safety settlement remain caller-owned.
type ProtectionRegistryStore interface {
	ProtectionRegistryReader
	TransitionProtection(context.Context, stateauthority.Authority, outputownership.ProtectionTransition) (outputownership.Registry, error)
}
