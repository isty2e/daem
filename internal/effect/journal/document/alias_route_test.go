package document

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

func TestAliasRoutesPreserveBothSelectorsAndResolvePhaseOwnership(t *testing.T) {
	selection, _, _ := carrierSelection(t)
	address := selection.DocumentAddress()
	input := configuration.AliasInput{Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot()}
	sourceBinding, err := configuration.OwnedMCPSourceBinding(input)
	if err != nil {
		t.Fatal(err)
	}
	currentBinding, err := configuration.RootBindingForAlias(input)
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewAliasRoute(address, sourceBinding)
	if err != nil {
		t.Fatal(err)
	}
	current, err := NewAliasRoute(address, currentBinding)
	if err != nil {
		t.Fatal(err)
	}
	for sourceIndex := range 2 {
		targetIndex := 1 - sourceIndex
		groups := make([][]AliasRoute, 2)
		groups[sourceIndex], groups[targetIndex] = []AliasRoute{source}, []AliasRoute{current}
		placement, transfers, err := ResolveAliasPlacement(t.Context(), groups)
		if err != nil || len(transfers) != 1 {
			t.Fatal("complete routes did not reconstruct their one physical transfer", err)
		}
		before, foundBefore := placement.BeforeGroup(address)
		after, foundAfter := placement.AfterGroup(address)
		if !foundBefore || !foundAfter || before != sourceIndex || after != targetIndex {
			t.Fatal("group order replaced the source/current meanings", before, after)
		}
	}
	for _, routes := range [][]AliasRoute{{source, current}, {current, source}} {
		addresses, err := AliasRouteAddresses(t.Context(), routes)
		if err != nil || !slices.Equal(addresses, []aggregate.DocumentAddress{address}) {
			t.Fatal("coalescence lost one logical address", err)
		}
		placement, transfers, err := ResolveAliasPlacement(t.Context(), [][]AliasRoute{routes})
		if err != nil || len(transfers) != 0 || !slices.Equal(placement.BeforeAliases(0), placement.AfterAliases(0)) {
			t.Fatal("coalesced selectors became a synthetic transfer", err)
		}
	}
	for _, groups := range [][][]AliasRoute{
		{{source}, {source}},
		{{current}, {current}},
		{{source, current}, {current}},
		{{source, source}},
		{{current, current}},
		{{{}}},
		{nil},
	} {
		placement, transfers, err := ResolveAliasPlacement(t.Context(), groups)
		_, partial := placement.BeforeGroup(address)
		if err == nil || partial || len(transfers) != 0 {
			t.Fatal("ambiguous selectors yielded usable partial placement", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := ResolveAliasPlacement(ctx, [][]AliasRoute{{source}}); !errors.Is(err, context.Canceled) {
		t.Fatal("selector reconstruction ignored cancellation", err)
	}
	other := documentClaimsForTest(t).alias
	if _, err := NewAliasRoute(other, sourceBinding); err == nil {
		t.Fatal("a Claude source selector changed the meaning of an unrelated alias")
	}
}
