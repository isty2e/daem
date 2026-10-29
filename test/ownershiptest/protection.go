// Package ownershiptest provides persisted ownership fixtures without importing
// the CLI or workflows being tested.
package ownershiptest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// WritePendingDocumentProtection seeds a complete safety record without
// activating an invocation producer. Other current Registry facts are retained.
func WritePendingDocumentProtection(t *testing.T, manifestPath, physical string) ownership.PendingProtection {
	t.Helper()
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(observeExact(t, filepath.Join(paths.ManifestRoot, "foreign", "state.json")), filepath.Join(paths.ManifestRoot, "foreign.toml"))
	if err != nil {
		t.Fatal(err)
	}
	exact := observeExact(t, physical)
	destination, err := output.Parse(".codex/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(owner, destination, exact)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	bindings := append(current.DocumentBindings(), binding)
	cohort := []ownership.DocumentBinding{binding}
	for _, retained := range current.DocumentBindings() {
		if retained.PathAuthority().Equal(exact) {
			cohort = append(cohort, retained)
		}
	}
	document, err := ownership.NewProtectedDocument(cohort, 0, 0, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	invocation, _ := executionidentity.NewInvocation(strings.Repeat("a", 32))
	correlation, _ := ownership.NewProtectionCorrelation("test-operation", strings.Repeat("b", 64), strings.Repeat("c", 64))
	pending, err := ownership.NewArmedProtection(owner, invocation, correlation, []ownership.ProtectedDocument{document})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistryWithProtections(current.Claims(), bindings, append(current.PendingProtections(), pending))
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(store.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), content, 0o600); err != nil {
		t.Fatal(err)
	}
	return pending
}

func observeExact(t *testing.T, path string) pathauthority.Exact {
	t.Helper()
	observation, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	return observation.Exact()
}
