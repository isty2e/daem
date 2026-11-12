package store

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
)

var _ ownershipmutation.DocumentRegistryStore = Store{}

// ConvergeForDocuments uses missing-history comparison for the selected owner,
// not new mutation authority. Both successors share the same rooted snapshot CAS.
func (store Store) ConvergeForDocuments(ctx context.Context, claims ownership.ClaimConvergence, bindings ownership.DocumentBindingConvergence, owner stateauthority.Authority, missing []pathauthority.Provisional, maximumPhysicalDepth int, budget rootedpath.PhysicalTraversalBudget, references ...pathauthority.Reference) (ownership.Registry, error) {
	if ctx == nil {
		return ownership.Registry{}, fmt.Errorf("document registry convergence context is required")
	}
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}
	if store.root == nil {
		return ownership.Registry{}, fmt.Errorf("document registry convergence requires retained rooted authority")
	}
	observations, err := newDocumentPathAuthorityObservationSession(ctx, owner, missing, maximumPhysicalDepth, budget)
	if err != nil {
		return ownership.Registry{}, err
	}
	return store.convergeRegistry(ctx, claims, bindings, observations, references)
}
