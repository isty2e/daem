package ownership

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

// DocumentRegistryReader admits matching stored claims for an owner's currently
// missing documents. This comparison read grants no promotion or write authority.
type DocumentRegistryReader interface {
	LoadForDocuments(context.Context, stateauthority.Authority, []pathauthority.Provisional, int, rootedpath.PhysicalTraversalBudget) (outputownership.Registry, error)
}
