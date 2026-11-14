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

var _ ownershipmutation.DocumentRegistryReader = Store{}

// LoadForDocuments preserves actual owned claim records when a missing document
// retains their exact comparison key and witness. Other authorities remain strict.
func (store Store) LoadForDocuments(ctx context.Context, owner stateauthority.Authority, missing []pathauthority.Provisional, removals []ownership.Claim, maximumPhysicalDepth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	observations, err := newDocumentPathAuthorityObservationSession(ctx, owner, missing, maximumPhysicalDepth, budget)
	if err != nil {
		return ownership.Registry{}, err
	}
	return store.load(ctx, removals, observations)
}

func newDocumentPathAuthorityObservationSession(ctx context.Context, owner stateauthority.Authority, missing []pathauthority.Provisional, maximumPhysicalDepth int, budget rootedpath.PhysicalTraversalBudget) (*pathAuthorityObservationSession, error) {
	if ctx == nil {
		return nil, fmt.Errorf("document registry read context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := owner.Validate(); err != nil {
		return nil, err
	}
	if len(missing) == 0 {
		return nil, fmt.Errorf("document registry read requires observed missing namespaces")
	}
	observations, err := newBoundedPathAuthorityObservationSession(maximumPhysicalDepth, budget)
	if err != nil {
		return nil, err
	}
	observations.documentOwner = owner
	observations.missingDocuments = make(map[string]pathauthority.Provisional, len(missing))
	for _, namespace := range missing {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := namespace.Validate(); err != nil {
			return nil, err
		}
		keys := []string{namespace.CandidateKey()}
		if legacy, present := namespace.LegacyComparisonKey(); present {
			keys = append(keys, legacy)
		}
		for _, key := range keys {
			if previous, found := observations.missingDocuments[key]; found && !previous.Equal(namespace) {
				return nil, fmt.Errorf("document registry read repeats a contradictory missing namespace")
			}
			observations.missingDocuments[key] = namespace
		}
	}
	return observations, nil
}

func (session *pathAuthorityObservationSession) admitsMissingDocumentClaim(claim ownership.Claim) bool {
	namespace, found := session.missingDocuments[claim.Address().Path()]
	return found && claim.OwnedBy(session.documentOwner) && namespace.MatchesMissingExact(claim.Address().PathAuthority())
}
