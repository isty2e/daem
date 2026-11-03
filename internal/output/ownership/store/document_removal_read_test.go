package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestDocumentRegistryReadJoinsMissingHistoryAndExpectedRemovals(t *testing.T) {
	root := canonicalTestRoot(t)
	documentPath := filepath.Join(root, "Caf\u00e9.json")
	ordinaryPath := filepath.Join(root, "R\u00e9sum\u00e9.md")
	for _, path := range []string{documentPath, ordinaryPath} {
		if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	document := testActiveClaim(t, root, "owner", documentPath, "")
	ordinary := testActiveClaim(t, root, "owner", ordinaryPath, "")
	registry, err := ownership.NewRegistry([]ownership.Claim{document, ordinary})
	if err != nil {
		t.Fatal(err)
	}
	content, err := Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(root, "claims.json")
	if err := os.WriteFile(registryPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{documentPath, ordinaryPath} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	observed, err := mutation.ObserveDirectoryEntryAuthority(documentPath)
	if err != nil {
		t.Fatal(err)
	}
	missing, provisional := observed.Provisional()
	if !provisional {
		t.Skip("filesystem provides exact absent Unicode authority")
	}
	store := mustStore(t, registryPath)
	for _, mode := range []string{"both", "missing only", "removal only", "foreign document owner"} {
		t.Run(mode, func(t *testing.T) {
			budget, err := recovery.NewPhysicalWorkBudget(2)
			if err != nil {
				t.Fatal(err)
			}
			owner := document.Owner()
			removals := []ownership.Claim{ordinary}
			if mode == "missing only" {
				removals = nil
			}
			if mode == "foreign document owner" {
				owner = testActiveClaim(t, root, "foreign", filepath.Join(root, "foreign.txt"), "").Owner()
			}
			var actual ownership.Registry
			if mode == "removal only" {
				actual, err = store.LoadForClaimRemovals(t.Context(), removals, recovery.MaximumPhysicalPathDepth, budget)
			} else {
				actual, err = store.LoadForDocuments(t.Context(), owner, []pathauthority.Provisional{missing}, removals, recovery.MaximumPhysicalPathDepth, budget)
			}
			if mode == "both" {
				if err != nil || !actual.Equal(registry) {
					t.Fatal("combined qualifications lost actual stored claims", err)
				}
			} else if err == nil {
				t.Fatal("one qualification was allowed to cover the other owner's missing path")
			}
			after, err := os.ReadFile(registryPath)
			if err != nil || !bytes.Equal(content, after) {
				t.Fatal("comparison changed the Registry", err)
			}
		})
	}
}
