package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output/ownership"
)

func TestRegistryWriterBudgetPreservesReadableMetadata(t *testing.T) {
	root := canonicalTestRoot(t)
	store := mustStore(t, filepath.Join(root, "claims.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "config.json"), "/x")
	registry, err := ownership.NewRegistry([]ownership.Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	small, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	var persisted file
	if err := json.Unmarshal(small, &persisted); err != nil {
		t.Fatal(err)
	}
	overhead := len(small) - len(*persisted.Claims[0].Footprint.KeyPath)
	pointer := "/" + strings.Repeat("x", maximumOwnershipRegistryBytes-overhead-1)
	persisted.Claims[0].Footprint.KeyPath = &pointer
	before, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	before = append(before, '\n')
	if len(before) != maximumOwnershipRegistryBytes {
		t.Fatalf("budget fixture has %d bytes", len(before))
	}
	if err := os.WriteFile(store.Path(), before, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil {
		t.Fatalf("admitted boundary fixture must be readable: %v", err)
	}
	other := testActiveClaim(t, root, "owner", filepath.Join(root, "other.json"), "")
	present, _ := ownership.PresentClaim(other)
	oversized, err := loaded.Apply(other.Address(), ownership.NoClaim(), present)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encode(oversized); err == nil {
		t.Error("private serializer admitted metadata beyond its reader envelope")
	}
	if _, err := store.Apply(t.Context(), other.Address(), ownership.NoClaim(), present); err == nil {
		t.Error("store published an unreadable registry")
	}
	after, err := os.ReadFile(store.Path())
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("budget rejection did not preserve previous metadata: %v", err)
	}
}
