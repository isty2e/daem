package record

import (
	"context"
	"fmt"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// New carrier metadata must satisfy the same phase claims and binding join at
// publication as at loading. Historical records keep their existing image seal.
func (journal Native) validateCarrierMetadata(ctx context.Context) error {
	var routes []journaldocument.AliasRoute
	carrier := false
	for _, group := range journal.record.DocumentGroups {
		bound, err := group.namespace.boundRoutes(ctx)
		if err != nil {
			return err
		}
		for _, route := range bound {
			if route.OwnedSource() && group.Removal != nil {
				return fmt.Errorf("owned MCP source cannot carry document removal work")
			}
			carrier = carrier || route.OwnedSource()
		}
		for _, binding := range group.BindingIntents {
			intent, err := binding.canonicalShape()
			if err != nil {
				return err
			}
			carrier = carrier || intent.IsTransfer()
		}
		routes = append(routes, bound...)
	}
	if !carrier {
		return nil
	}
	placement, err := journal.documentPlacement(ctx)
	if err != nil {
		return err
	}
	bindings, err := journal.documentBindings(ctx)
	if err != nil {
		return err
	}
	indexes, err := journaldocument.IndexSnapshotsForClaims(ctx, journal.before, journal.after, journal.namespace.claims)
	if err != nil {
		return err
	}
	var intents []ownership.DocumentBindingIntent
	for index, group := range journal.record.DocumentGroups {
		if _, _, err := journal.documentClaims(ctx, indexes, group, placement.BeforeAliases(index), placement.AfterAliases(index)); err != nil {
			return err
		}
		intents = append(intents, bindings[index]...)
	}
	return indexes.ValidateBoundBindingMembership(ctx, journal.owner, routes, intents)
}

func (facts documentNamespaceFacts) boundRoutes(ctx context.Context) ([]journaldocument.AliasRoute, error) {
	routes := make([]journaldocument.AliasRoute, 0, len(facts.aliases))
	for _, alias := range facts.aliases {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		route, err := journaldocument.NewAliasRoute(alias.address, alias.binding)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, nil
}

func (journal Native) documentPlacement(ctx context.Context) (journaldocument.PhysicalPlacement, error) {
	if ctx == nil {
		return journaldocument.PhysicalPlacement{}, fmt.Errorf("document placement context is required")
	}
	if journal.record.Version == native17Version {
		groups := make([][]journaldocument.AliasRoute, len(journal.record.DocumentGroups))
		for index, group := range journal.record.DocumentGroups {
			routes, err := group.namespace.boundRoutes(ctx)
			if err != nil {
				return journaldocument.PhysicalPlacement{}, err
			}
			groups[index] = routes
		}
		placement, _, err := journaldocument.ResolveAliasPlacement(ctx, groups)
		return placement, err
	}
	groups := make([][]aggregate.DocumentAddress, len(journal.record.DocumentGroups))
	for index, group := range journal.record.DocumentGroups {
		aliases, _, err := group.canonicalReferences(ctx)
		if err != nil {
			return journaldocument.PhysicalPlacement{}, err
		}
		groups[index] = aliases
	}
	return journaldocument.ResolvePhysicalPlacement(ctx, groups, nil)
}
