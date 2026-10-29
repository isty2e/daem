//go:build darwin || linux

package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestRootedProtectionStoreSharesPhysicalBudgetAndRetainsCapability(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "claims.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "settings.json"), "/model")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	seedConvergenceRegistry(t, store, nil, binding)
	protection := storedPendingProtection(t, binding)
	arm, _ := ownership.NewArmProtection(protection)
	captured, destination, err := rootedpath.CaptureDestination(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer captured.Close()
	budget := &recordingPhysicalTraversalBudget{remaining: 1 << 25}
	rooted, err := NewRooted(captured, destination, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	beforeArm := budget.admitted
	if _, err := rooted.TransitionProtection(t.Context(), protection.Owner(), arm); err != nil {
		t.Fatalf("rooted protection publication failed: %v", err)
	}
	if budget.admitted <= beforeArm {
		t.Fatal("rooted protection publication bypassed the shared path budget")
	}
	beforeRead := budget.admitted
	rows, err := rooted.LoadPendingProtections(t.Context())
	if err != nil || len(rows) != 1 || !rows[0].Equal(protection) {
		t.Fatalf("rooted protection was not discoverable: %v", err)
	}
	if budget.admitted <= beforeRead {
		t.Fatal("rooted protection read bypassed the shared path budget")
	}
	before, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	budget.remaining = 0
	if _, err := rooted.LoadPendingProtections(t.Context()); err == nil {
		t.Fatal("safety discovery bypassed exhausted path capacity")
	}
	clear, _ := ownership.NewClearProtection(protection)
	if _, err := rooted.TransitionProtection(t.Context(), protection.Owner(), clear); err == nil {
		t.Fatal("protection settlement bypassed exhausted path capacity")
	}
	after, err := os.ReadFile(store.Path())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("under-budget settlement modified the registry: %v", err)
	}
	budget.remaining = 1 << 25
	if _, err := rooted.TransitionProtection(t.Context(), protection.Owner(), clear); err != nil {
		t.Fatalf("failed attempt consumed or leaked retained root authority: %v", err)
	}
}
