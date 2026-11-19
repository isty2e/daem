package namespace

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

var ErrRemovalProtection = errors.New("cannot verify protection after removal")

// RemovalPreflight retains only the routes and protection selectors needed to
// check a prospective removal after the original observations close.
type RemovalPreflight struct {
	aliases   []protectionAlias
	selectors ReservationSelectors
}

func (namespace DocumentNamespace) RemovalPreflight() RemovalPreflight {
	return RemovalPreflight{aliases: protectionAliases(namespace.aliases), selectors: namespace.protection}
}

func (preflight RemovalPreflight) RequireRecoverablePath(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) error {
	if len(preflight.aliases) == 0 || ctx == nil || budget == nil {
		return fmt.Errorf("document removal preflight requires captured routes and a traversal budget")
	}
	var projected []protectionAlias
	for _, alias := range preflight.aliases {
		if err := ctx.Err(); err != nil {
			return err
		}
		missing, needed, err := alias.observation.ProjectRemovalSpellingBounded(alias.route, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		if needed {
			exact, _ := alias.observation.Exact()
			if !missing.MatchesRetainedExact(exact) {
				return mutation.ErrRemovalSpellingMismatch
			}
			alias.observation = missing
			projected = append(projected, alias)
		}
	}
	if len(projected) != 0 {
		if err := newPhysicalObserver(preflight.selectors, budget).requireUnreserved(ctx, projected); err != nil {
			return fmt.Errorf("%w: %w", ErrRemovalProtection, err)
		}
	}
	return ctx.Err()
}
