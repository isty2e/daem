package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
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
