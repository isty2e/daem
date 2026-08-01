package testkit

import (
	"context"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// MustObservedPathAuthority captures exact authority for a filesystem-backed test fixture.
func MustObservedPathAuthority(t testing.TB, path string) pathauthority.Exact {
	t.Helper()
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatalf("ObservePersistedDirectoryEntryAuthority(%q): %v", path, err)
	}
	return authority.Exact()
}

// WriteActiveOwnershipClaim seeds the durable authority paired with a manually written global state fixture.
func WriteActiveOwnershipClaim(t *testing.T, manifestPath string, destination string, contentPath string) ownership.Claim {
	t.Helper()
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("Resolve ownership paths returned error: %v", err)
	}
	resolved, err := hostpath.NewResolverWithManagedDataRoot(paths.ManifestRoot, paths.DataDir).Resolve(parseDestination(t, destination))
	if err != nil {
		t.Fatalf("Resolve ownership destination returned error: %v", err)
	}
	pathAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(resolved)
	if err != nil {
		t.Fatalf("ObservePersistedDirectoryEntryAuthority returned error: %v", err)
	}
	address, err := ownership.NewManagedAddress(pathAuthority.Exact(), contentPath)
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	statefileAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
	if err != nil {
		t.Fatalf("ObservePersistedDirectoryEntryAuthority statefile returned error: %v", err)
	}
	owner, err := stateauthority.New(statefileAuthority.Exact(), paths.ManifestPath)
	if err != nil {
		t.Fatalf("stateauthority.New returned error: %v", err)
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatalf("NewActiveClaim returned error: %v", err)
	}
	value, _ := ownership.PresentClaim(claim)
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatalf("Open ownership store returned error: %v", err)
	}
	if _, err := store.Apply(context.Background(), address, ownership.NoClaim(), value); err != nil {
		t.Fatalf("Write ownership claim returned error: %v", err)
	}
	return claim
}

func AssertOwnershipClaimCount(t *testing.T, manifestPath string, want int) {
	t.Helper()
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("Resolve ownership paths returned error: %v", err)
	}
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatalf("Open ownership store returned error: %v", err)
	}
	registry, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load ownership registry returned error: %v", err)
	}
	if claims := registry.Claims(); len(claims) != want {
		t.Fatalf("ownership claims = %#v, want count %d", claims, want)
	}
}
