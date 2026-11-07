package ownership

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

func TestRegistryOverlappingRetainsAllWholeRequestBlockers(t *testing.T) {
	root := t.TempDir()
	own := mustAuthority(t, filepath.Join(root, "own-state"), filepath.Join(root, "own.toml"))
	foreign := mustAuthority(t, filepath.Join(root, "foreign-state"), filepath.Join(root, "foreign.toml"))
	first, err := NewActiveClaim(mustAddress(t, filepath.Join(root, "config"), "/a"), own)
	if err != nil {
		t.Fatal(err)
	}
	last, err := NewReservedClaim(mustAddress(t, filepath.Join(root, "config"), "/z"), foreign, "pending")
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewActiveClaim(mustAddress(t, filepath.Join(root, "elsewhere"), ""), own)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry([]Claim{last, other, first})
	if err != nil {
		t.Fatal(err)
	}
	whole := mustAddress(t, filepath.Join(root, "config"), "")
	if blocker, found := registry.Conflict(whole); !found || !blocker.Equal(first) {
		t.Fatal("first-only lookup no longer has its existing meaning")
	}
	all, err := registry.Overlapping(whole)
	if err != nil || len(all) != 2 || !all[0].Equal(first) || !all[1].Equal(last) {
		t.Fatal("whole query hid a later foreign/reserved overlap", err)
	}
	all[0] = Claim{}
	again, err := registry.Overlapping(whole)
	if err != nil || !again[0].Equal(first) {
		t.Fatal("overlap result mutated canonical Registry")
	}
	key, err := registry.Overlapping(first.Address())
	if err != nil || len(key) != 1 || !key[0].Equal(first) {
		t.Fatal("per-key overlap included a disjoint claim", err)
	}
	ancestor, err := registry.Overlapping(mustAddress(t, root, ""))
	if err != nil || len(ancestor) != 3 {
		t.Fatal("physical descendants were lost from the whole ancestor request", err)
	}
	if _, err := (Registry{}).Overlapping(whole); err == nil {
		t.Fatal("unobserved Registry became known-empty overlap evidence")
	}
	if _, err := registry.Overlapping(ManagedAddress{}); err == nil {
		t.Fatal("invalid requested address became known-empty overlap evidence")
	}
	if empty, err := EmptyRegistry().Overlapping(whole); err != nil || len(empty) != 0 {
		t.Fatal("actual empty Registry was refused", err)
	}
}

func TestRegistryProvisionalAncestorClaimsRetainsAllBlockers(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "Café.json")
	candidate, err := pathauthority.NewProvisional(
		physical, pathtest.DarwinCaseSensitive(physical).Witness(),
		root, pathtest.DarwinCaseSensitive(root).Witness(),
	)
	if err != nil {
		t.Fatal(err)
	}
	own := mustAuthority(t, filepath.Join(root, "own-state"), filepath.Join(root, "own.toml"))
	foreign := mustAuthority(t, filepath.Join(root, "foreign-state"), filepath.Join(root, "foreign.toml"))
	first, err := NewActiveClaim(mustAddress(t, physical, "/a"), own)
	if err != nil {
		t.Fatal(err)
	}
	last, err := NewReservedClaim(mustAddress(t, physical, "/z"), foreign, "pending")
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewActiveClaim(mustAddress(t, physical+"-other", ""), foreign)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry([]Claim{last, other, first})
	if err != nil {
		t.Fatal(err)
	}

	all, err := registry.ProvisionalAncestorClaims(candidate)
	if err != nil || len(all) != 2 || !all[0].Equal(first) || !all[1].Equal(last) {
		t.Fatal("provisional query hid a later foreign/reserved claim or included a sibling", err)
	}
	all[0] = Claim{}
	again, err := registry.ProvisionalAncestorClaims(candidate)
	if err != nil || len(again) != 2 || !again[0].Equal(first) {
		t.Fatal("provisional query mutated canonical Registry", err)
	}

	ancestor, err := NewActiveClaim(mustAddress(t, root, ""), foreign)
	if err != nil {
		t.Fatal(err)
	}
	ancestorRegistry, err := NewRegistry([]Claim{ancestor})
	if err != nil {
		t.Fatal(err)
	}
	containing, err := ancestorRegistry.ProvisionalAncestorClaims(candidate)
	if err != nil || len(containing) != 1 || !containing[0].Equal(ancestor) {
		t.Fatal("provisional query lost an exact ancestor claim", err)
	}

	descendant, err := NewActiveClaim(mustAddress(t, filepath.Join(physical, "child"), ""), foreign)
	if err != nil {
		t.Fatal(err)
	}
	descendantRegistry, err := NewRegistry([]Claim{descendant})
	if err != nil {
		t.Fatal(err)
	}
	if claims, err := descendantRegistry.ProvisionalAncestorClaims(candidate); err != nil || len(claims) != 0 {
		t.Fatal("candidate spelling became an exact containing path", err)
	}
	if _, err := (Registry{}).ProvisionalAncestorClaims(candidate); err == nil {
		t.Fatal("unobserved Registry became known-empty conflict evidence")
	}
	if _, err := registry.ProvisionalAncestorClaims(pathauthority.Provisional{}); err == nil {
		t.Fatal("invalid provisional observation became known-empty conflict evidence")
	}
	if claims, err := EmptyRegistry().ProvisionalAncestorClaims(candidate); err != nil || len(claims) != 0 {
		t.Fatal("actual empty Registry was refused", err)
	}
}

func TestRegistryWriteExclusionUsesActualEffectSemantics(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "Café.json")
	candidate, err := pathauthority.NewProvisional(physical, pathtest.DarwinCaseSensitive(physical).Witness(), root, pathtest.DarwinCaseSensitive(root).Witness())
	if err != nil {
		t.Fatal(err)
	}
	own := mustAuthority(t, filepath.Join(root, "own-state"), filepath.Join(root, "own.toml"))
	foreign := mustAuthority(t, filepath.Join(root, "foreign-state"), filepath.Join(root, "foreign.toml"))
	ownKey := mustAddress(t, physical, "/a")
	foreignKey := mustAddress(t, physical, "/z")
	whole := mustAddress(t, physical, "")
	ancestor := mustAddress(t, root, "")
	exact := whole.PathAuthority()
	clearValue := mustAddress(t, filepath.Join(root, "clear-value.json"), "/value")
	clearExistence := mustAddress(t, filepath.Join(root, "clear-existence.json"), "").PathAuthority()
	clearMode := mustAddress(t, filepath.Join(root, "clear-mode.json"), "").PathAuthority()
	clearPath := filepath.Join(root, "Clearé.json")
	clearCandidate, err := pathauthority.NewProvisional(clearPath, pathtest.DarwinCaseSensitive(clearPath).Witness(), root, pathtest.DarwinCaseSensitive(root).Witness())
	if err != nil {
		t.Fatal(err)
	}
	ownClaim, err := NewActiveClaim(ownKey, own)
	if err != nil {
		t.Fatal(err)
	}
	foreignClaim, err := NewReservedClaim(foreignKey, foreign, "pending")
	if err != nil {
		t.Fatal(err)
	}
	foreignWhole, err := NewActiveClaim(whole, foreign)
	if err != nil {
		t.Fatal(err)
	}
	foreignAncestor, err := NewActiveClaim(ancestor, foreign)
	if err != nil {
		t.Fatal(err)
	}
	ownWhole, err := NewActiveClaim(whole, own)
	if err != nil {
		t.Fatal(err)
	}
	ownAncestor, err := NewActiveClaim(ancestor, own)
	if err != nil {
		t.Fatal(err)
	}
	reservedWhole, err := NewReservedClaim(whole, foreign, "reserved-whole")
	if err != nil {
		t.Fatal(err)
	}
	descendant, err := NewActiveClaim(mustAddress(t, filepath.Join(physical, "child"), ""), foreign)
	if err != nil {
		t.Fatal(err)
	}
	changedProvenance, err := stateauthority.New(own.StatefileAuthority(), foreign.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	ownWithChangedProvenance, err := NewActiveClaim(whole, changedProvenance)
	if err != nil {
		t.Fatal(err)
	}
	sameProvenance, err := stateauthority.New(foreign.StatefileAuthority(), own.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	foreignWithSameProvenance, err := NewActiveClaim(whole, sameProvenance)
	if err != nil {
		t.Fatal(err)
	}
	shared := []Claim{foreignClaim, ownClaim}

	for _, test := range []struct {
		name      string
		claims    []Claim
		values    []ManagedAddress
		existence []pathauthority.Exact
		modes     []pathauthority.Exact
		missing   []pathauthority.Provisional
		conflict  Claim
	}{
		{name: "own key", claims: shared, values: []ManagedAddress{ownKey}},
		{name: "foreign key and mode", claims: shared, values: []ManagedAddress{foreignKey}, modes: []pathauthority.Exact{exact}, conflict: foreignClaim},
		{name: "whole value", claims: shared, values: []ManagedAddress{whole}, conflict: foreignClaim},
		{name: "creation or deletion", claims: shared, existence: []pathauthority.Exact{exact}, conflict: foreignClaim},
		{name: "mode with disjoint keys", claims: shared, modes: []pathauthority.Exact{exact}},
		{name: "own key and mode", claims: shared, values: []ManagedAddress{ownKey}, modes: []pathauthority.Exact{exact}},
		{name: "own whole and mode", claims: []Claim{ownWhole}, modes: []pathauthority.Exact{exact}},
		{name: "own ancestor and mode", claims: []Claim{ownAncestor}, modes: []pathauthority.Exact{exact}},
		{name: "mode with foreign whole", claims: []Claim{foreignWhole}, modes: []pathauthority.Exact{exact}, conflict: foreignWhole},
		{name: "mode with reserved foreign whole", claims: []Claim{reservedWhole}, modes: []pathauthority.Exact{exact}, conflict: reservedWhole},
		{name: "mode with foreign ancestor", claims: []Claim{foreignAncestor}, modes: []pathauthority.Exact{exact}, conflict: foreignAncestor},
		{name: "mode with descendant-only whole", claims: []Claim{descendant}, modes: []pathauthority.Exact{exact}},
		{name: "provisional later foreign claim", claims: shared, missing: []pathauthority.Provisional{candidate}, conflict: foreignClaim},
		{name: "provisional foreign ancestor", claims: []Claim{foreignAncestor}, missing: []pathauthority.Provisional{candidate}, conflict: foreignAncestor},
		{name: "later value request", claims: shared, values: []ManagedAddress{clearValue, foreignKey}, conflict: foreignClaim},
		{name: "later existence request", claims: shared, existence: []pathauthority.Exact{clearExistence, exact}, conflict: foreignClaim},
		{name: "later mode request", claims: []Claim{foreignWhole}, modes: []pathauthority.Exact{clearMode, exact}, conflict: foreignWhole},
		{name: "later provisional request", claims: shared, missing: []pathauthority.Provisional{clearCandidate, candidate}, conflict: foreignClaim},
		{name: "clean value then blocked mode", claims: []Claim{foreignWhole}, values: []ManagedAddress{clearValue}, modes: []pathauthority.Exact{exact}, conflict: foreignWhole},
		{name: "clean exact effects then blocked provisional", claims: shared, values: []ManagedAddress{clearValue}, existence: []pathauthority.Exact{clearExistence}, modes: []pathauthority.Exact{clearMode}, missing: []pathauthority.Provisional{candidate}, conflict: foreignClaim},
		{name: "same state key different provenance", claims: []Claim{ownWithChangedProvenance}, values: []ManagedAddress{ownKey}, existence: []pathauthority.Exact{exact}, modes: []pathauthority.Exact{exact}, missing: []pathauthority.Provisional{candidate}},
		{name: "different state key same provenance value", claims: []Claim{foreignWithSameProvenance}, values: []ManagedAddress{ownKey}, conflict: foreignWithSameProvenance},
		{name: "clean value then same-provenance foreign existence", claims: []Claim{foreignWithSameProvenance}, values: []ManagedAddress{clearValue}, existence: []pathauthority.Exact{exact}, conflict: foreignWithSameProvenance},
		{name: "existence with foreign ancestor", claims: []Claim{foreignAncestor}, existence: []pathauthority.Exact{exact}, conflict: foreignAncestor},
		{name: "different state key same provenance mode", claims: []Claim{foreignWithSameProvenance}, modes: []pathauthority.Exact{exact}, conflict: foreignWithSameProvenance},
		{name: "different state key same provenance provisional", claims: []Claim{foreignWithSameProvenance}, missing: []pathauthority.Provisional{candidate}, conflict: foreignWithSameProvenance},
		{name: "unclaimed writes", values: []ManagedAddress{whole}, modes: []pathauthority.Exact{exact}},
		{name: "no write", claims: shared},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, err := NewRegistry(test.claims)
			if err != nil {
				t.Fatal(err)
			}
			before, err := NewRegistry(registry.Claims())
			if err != nil {
				t.Fatal(err)
			}
			err = registry.RequireWriteExclusion(own, test.values, test.existence, test.modes, test.missing)
			if test.conflict.Validate() != nil {
				if err != nil {
					t.Fatal("noninterfering write was refused", err)
				}
			} else {
				var conflict *ForeignClaimError
				if !errors.As(err, &conflict) || !conflict.Existing.Equal(test.conflict) {
					t.Fatalf("foreign claim refusal = %v; want %#v", err, test.conflict)
				}
			}
			if !registry.Equal(before) {
				t.Fatal("write exclusion changed durable facts")
			}
		})
	}
}

func TestRegistryWriteExclusionRefusesIncompleteEvidence(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state"), filepath.Join(root, "daem.toml"))
	for _, test := range []struct {
		name      string
		registry  Registry
		owner     stateauthority.Authority
		values    []ManagedAddress
		existence []pathauthority.Exact
		modes     []pathauthority.Exact
		missing   []pathauthority.Provisional
	}{
		{name: "unobserved Registry", owner: owner},
		{name: "missing owner", registry: EmptyRegistry()},
		{name: "invalid value address", registry: EmptyRegistry(), owner: owner, values: []ManagedAddress{{}}},
		{name: "invalid existence address", registry: EmptyRegistry(), owner: owner, existence: []pathauthority.Exact{{}}},
		{name: "invalid mode address", registry: EmptyRegistry(), owner: owner, modes: []pathauthority.Exact{{}}},
		{name: "invalid provisional", registry: EmptyRegistry(), owner: owner, missing: []pathauthority.Provisional{{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.registry.RequireWriteExclusion(test.owner, test.values, test.existence, test.modes, test.missing); err == nil {
				t.Fatal("incomplete comparison facts granted claim exclusion")
			}
		})
	}
}
