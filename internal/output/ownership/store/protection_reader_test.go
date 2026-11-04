package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestBoundedProtectionReaderDoesNotCreateMissingArtifacts(t *testing.T) {
	root := canonicalTestRoot(t)
	path := filepath.Join(root, "missing", "nested", "registry.json")
	budget := &documentSettlementCountingBudget{}
	rows, err := ReadPendingProtectionsBounded(t.Context(), path, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil || len(rows) != 0 || budget.calls == 0 {
		t.Fatal("missing artifact did not produce a bounded empty observation", err)
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("safety discovery created its missing hierarchy", err)
	}
}

func TestBoundedProtectionReaderRetainsArtifactAndBudgetRefusals(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "registry.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "config.json"), "/model")
	binding := storedDocumentBinding(t, claim, "@pi-agent/settings.json")
	protection := storedPendingProtection(t, binding)
	registry, err := ownership.NewRegistryWithProtections(nil, nil, []ownership.PendingProtection{protection})
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), content, 0o600); err != nil {
		t.Fatal(err)
	}
	exhausted := errors.New("no physical work remaining")
	rows, err := ReadPendingProtectionsBounded(t.Context(), store.Path(), physicalwork.MaximumPhysicalPathDepth, documentRegistryFailBudget{err: exhausted})
	if !errors.Is(err, exhausted) || rows != nil {
		t.Fatal("safety discovery bypassed exhausted shared work", err)
	}
	after, err := os.ReadFile(store.Path())
	if err != nil || !bytes.Equal(content, after) {
		t.Fatal("failed safety discovery mutated its artifact", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rows, err := ReadPendingProtectionsBounded(ctx, store.Path(), physicalwork.MaximumPhysicalPathDepth, &documentSettlementCountingBudget{}); !errors.Is(err, context.Canceled) || rows != nil {
		t.Fatal("canceled reader returned usable records", err)
	}
	if rows, err := ReadPendingProtectionsBounded(nil, store.Path(), physicalwork.MaximumPhysicalPathDepth, &documentSettlementCountingBudget{}); err == nil || rows != nil {
		t.Fatal("nil reader context returned usable records")
	}
	if rows, err := ReadPendingProtectionsBounded(t.Context(), store.Path(), 0, &documentSettlementCountingBudget{}); err == nil || rows != nil {
		t.Fatal("unbounded reader depth was admitted")
	}
	if rows, err := ReadPendingProtectionsBounded(t.Context(), store.Path(), physicalwork.MaximumPhysicalPathDepth, nil); err == nil || rows != nil {
		t.Fatal("missing reader budget was admitted")
	}
	if err := os.Chmod(store.Path(), 0o644); err != nil {
		t.Fatal(err)
	}
	if rows, err := ReadPendingProtectionsBounded(t.Context(), store.Path(), physicalwork.MaximumPhysicalPathDepth, &documentSettlementCountingBudget{}); err == nil || rows != nil {
		t.Fatal("exposed authority artifact returned usable records")
	}
}
