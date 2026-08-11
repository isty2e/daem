//go:build darwin

package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestStoreRemovesClaimAfterNonASCIIFinalPathBecomesProvisional(t *testing.T) {
	root := canonicalTestRoot(t)
	managedPath := filepath.Join(root, "config-\u00e9.json")
	if err := os.WriteFile(managedPath, []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	claim := testActiveClaim(t, root, "owner", managedPath, "")
	value, _ := ownership.PresentClaim(claim)
	registryStore := mustStore(t, filepath.Join(root, "claims.json"))
	if _, err := registryStore.Apply(t.Context(), claim.Address(), ownership.NoClaim(), value); err != nil {
		t.Fatalf("seed claim: %v", err)
	}
	if err := os.Remove(managedPath); err != nil {
		t.Fatal(err)
	}

	if _, err := registryStore.Load(t.Context()); err == nil ||
		!strings.Contains(err.Error(), "provisional authority") {
		t.Fatalf("strict Load error = %v, want provisional-authority refusal", err)
	}
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	loaded, err := registryStore.LoadForClaimRemovals(
		t.Context(),
		[]ownership.Claim{claim},
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		t.Fatalf("LoadForClaimRemovals returned error: %v", err)
	}
	if actual, present := loaded.Exact(claim.Address()); !present || !actual.Equal(claim) {
		t.Fatal("removal-aware load lost the exact expected claim")
	}
	if _, err := registryStore.RemoveClaim(t.Context(), claim); err != nil {
		t.Fatalf("RemoveClaim returned error: %v", err)
	}
	current, err := registryStore.Load(t.Context())
	if err != nil {
		t.Fatalf("strict Load after removal returned error: %v", err)
	}
	if claims := current.Claims(); len(claims) != 0 {
		t.Fatalf("claims after removal = %#v, want empty", claims)
	}
}
