package document

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// AliasRoute preserves a versioned selector separately from its logical address.
// Physical equality and authority still require independently qualified roots.
type AliasRoute struct {
	address aggregate.DocumentAddress
	binding configuration.AliasBinding
}

func NewAliasRoute(address aggregate.DocumentAddress, binding configuration.AliasBinding) (AliasRoute, error) {
	if err := address.Validate(); err != nil {
		return AliasRoute{}, err
	}
	_, err := configuration.AdmitRecordedAliasBinding(configuration.AliasInput{
		Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot(),
	}, binding.RootReference(), binding.Coordinate(), binding.PolicyID())
	if err != nil {
		return AliasRoute{}, err
	}
	return AliasRoute{address: address, binding: binding}, nil
}

func (route AliasRoute) Address() aggregate.DocumentAddress  { return route.address }
func (route AliasRoute) Binding() configuration.AliasBinding { return route.binding }

func (route AliasRoute) OwnedSource() bool {
	return configuration.NativeRootIsOwnedPoint(route.binding.RootReference())
}

func (route AliasRoute) CurrentCarrier() bool {
	return configuration.NativeRootIsCurrentMCPPoint(route.binding.RootReference())
}

// AliasRouteAddresses admits duplicate logical addresses only for both witnesses
// of a coalesced Claude MCP source/current pair. It does not coalesce endpoints.
func AliasRouteAddresses(ctx context.Context, routes []AliasRoute) ([]aggregate.DocumentAddress, error) {
	if ctx == nil || len(routes) == 0 {
		return nil, fmt.Errorf("bound aliases require context and a nonempty route set")
	}
	byAddress := make(map[aggregate.DocumentAddress][]AliasRoute)
	var addresses []aggregate.DocumentAddress
	for _, route := range routes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := NewAliasRoute(route.address, route.binding); err != nil {
			return nil, err
		}
		earlier := byAddress[route.address]
		if len(earlier) == 0 {
			addresses = append(addresses, route.address)
		} else if len(earlier) != 1 || !carrierRoutePair(earlier[0], route) {
			return nil, fmt.Errorf("logical alias repeats without its unique source/current selector pair")
		}
		byAddress[route.address] = append(earlier, route)
	}
	return addresses, ctx.Err()
}

func carrierRoutePair(left, right AliasRoute) bool {
	return left.address == right.address && (left.OwnedSource() && right.CurrentCarrier() || right.OwnedSource() && left.CurrentCarrier())
}

// ResolveAliasPlacement reconstructs phase ownership from complete versioned
// routes. No caller supplies a second interpretation of source/target roles.
func ResolveAliasPlacement(ctx context.Context, groups [][]AliasRoute) (PhysicalPlacement, []CarrierTransfer, error) {
	addresses := make([][]aggregate.DocumentAddress, len(groups))
	type occurrence struct {
		group int
		route AliasRoute
	}
	byAddress := make(map[aggregate.DocumentAddress][]occurrence)
	seen := make(map[AliasRoute]struct{})
	for index, routes := range groups {
		logical, err := AliasRouteAddresses(ctx, routes)
		if err != nil {
			return PhysicalPlacement{}, nil, err
		}
		addresses[index] = logical
		for _, route := range routes {
			if _, repeated := seen[route]; repeated {
				return PhysicalPlacement{}, nil, fmt.Errorf("one selector route belongs to multiple physical groups")
			}
			seen[route] = struct{}{}
			byAddress[route.address] = append(byAddress[route.address], occurrence{group: index, route: route})
		}
	}
	var transfers []CarrierTransfer
	for _, occurrences := range byAddress {
		if err := ctx.Err(); err != nil {
			return PhysicalPlacement{}, nil, err
		}
		if len(occurrences) == 1 {
			continue
		}
		if len(occurrences) != 2 || !carrierRoutePair(occurrences[0].route, occurrences[1].route) {
			return PhysicalPlacement{}, nil, fmt.Errorf("shared logical alias requires its complete source/current pair")
		}
		left, right := occurrences[0], occurrences[1]
		if left.group == right.group {
			continue
		}
		if !left.route.OwnedSource() {
			left, right = right, left
		}
		transfer, err := NewCarrierTransfer(left.route.address, left.group, right.group)
		if err != nil {
			return PhysicalPlacement{}, nil, err
		}
		transfers = append(transfers, transfer)
	}
	placement, err := ResolvePhysicalPlacement(ctx, addresses, transfers)
	if err != nil {
		return PhysicalPlacement{}, nil, err
	}
	return placement, transfers, nil
}
