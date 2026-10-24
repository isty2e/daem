//go:build darwin || linux

package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestJointRegistryConvergenceKeepsCapturedRootAndSharedTraversalBudget(t *testing.T) {
	root := canonicalTestRoot(t)
	dataA, dataB := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, path := range []string{dataA, dataB} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "data")
	if err := os.Symlink(dataA, alias); err != nil {
		t.Skipf("create data-root alias: %v", err)
	}
	captured, destination, err := rootedpath.CaptureDestination(filepath.Join(alias, "claims.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = captured.Close() })
	budget := &recordingPhysicalTraversalBudget{remaining: 1 << 30}
	store, err := NewRooted(captured, destination, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dataB, alias); err != nil {
		t.Fatal(err)
	}
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "settings.json"), "")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	claims, err := ownership.NewClaimConvergence([]ownership.ClaimChange{
		mustStoreClaimChange(t, claim.Address(), ownership.NoClaim(), storeClaimValue(t, claim)),
	})
	if err != nil {
		t.Fatal(err)
	}
	bindings := mustStoreBindingConvergence(t, storeBindingIntent(t, binding, false, true))
	want, err := ownership.NewRegistry([]ownership.Claim{claim}, binding)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := store.ConvergeRegistry(t.Context(), claims, bindings); err != nil || !result.Equal(want) {
		t.Fatalf("rooted joint registry write failed: %v", err)
	}
	actual := mustStore(t, filepath.Join(dataA, "claims.json"))
	if loaded, err := actual.Load(t.Context()); err != nil || !loaded.Equal(want) {
		t.Fatalf("captured data root did not receive the joint registry: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dataB, "claims.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retargeted alias received a registry: %v", err)
	}

	before, err := os.Stat(actual.Path())
	if err != nil {
		t.Fatal(err)
	}
	budget.remaining = 0
	for _, invalid := range []struct {
		claims   ownership.ClaimConvergence
		bindings ownership.DocumentBindingConvergence
	}{
		{bindings: bindings},
		{claims: claims},
	} {
		calls := budget.calls
		result, err := store.ConvergeRegistry(t.Context(), invalid.claims, invalid.bindings)
		if err == nil || errors.Is(err, errTestTraversalBudgetExhausted) || budget.calls != calls {
			t.Fatalf("invalid family reached physical registry observation: %v", err)
		}
		assertUnusableRegistry(t, result)
	}

	result, err := store.ConvergeRegistry(t.Context(), claims, bindings)
	if !errors.Is(err, errTestTraversalBudgetExhausted) {
		t.Fatalf("joint convergence bypassed the exhausted shared budget: %v", err)
	}
	assertUnusableRegistry(t, result)
	after, err := os.Stat(actual.Path())
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("budget refusal replaced the registry: %v", err)
	}
}
