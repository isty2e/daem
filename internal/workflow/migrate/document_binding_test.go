package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
)

func migrationDocumentBinding(t *testing.T, owner stateauthority.Authority, physical string) ownership.DocumentBinding {
	t.Helper()
	destination, err := output.Parse("@pi-agent/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(physical)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(owner, destination, observed.Exact())
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestStateMigrationRetainsBindingIdentityWithoutReadingHostBaseline(t *testing.T) {
	paths, legacy := migrationFixture(t)
	from, err := authorityFor(legacy)
	if err != nil {
		t.Fatal(err)
	}
	physical := filepath.Join(os.Getenv("HOME"), "settings.json")
	before := []byte("an opaque host document; not a new baseline\n")
	writeFixture(t, physical, before)
	selected := migrationDocumentBinding(t, from, physical)
	foreignPath, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	foreignOwner, err := stateauthority.New(foreignPath.Exact(), filepath.Join(t.TempDir(), "foreign.toml"))
	if err != nil {
		t.Fatal(err)
	}
	foreign := migrationDocumentBinding(t, foreignOwner, physical)
	registry, err := ownership.NewRegistry(nil, selected, foreign)
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, paths.OwnershipRegistryPath, content)
	prepared, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	to, err := authorityFor(paths)
	if err != nil {
		t.Fatal(err)
	}
	actual, found := loaded.DocumentBinding(to.Key(), selected.Destination())
	if !found || !actual.PathAuthority().Equal(selected.PathAuthority()) || actual.Owner().ManifestPath() != from.ManifestPath() {
		t.Fatal("migration changed document identity or diagnostic provenance")
	}
	if _, found := loaded.DocumentBinding(from.Key(), selected.Destination()); found {
		t.Fatal("migration retained old binding authority")
	}
	if actual, found := loaded.DocumentBinding(foreignOwner.Key(), foreign.Destination()); !found || !actual.Equal(foreign) {
		t.Fatal("migration changed foreign binding evidence")
	}
	after, err := os.ReadFile(physical)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("metadata migration touched the host document: %v", err)
	}
	repeat, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer repeat.Close()
	if repeat.Disclosure().Action != "already_migrated" {
		t.Fatal("binding transfer did not converge with the relocation receipt")
	}
}

func TestStateMigrationRefusesBindingWithoutSnapshotOrWithForeignProvenance(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign provenance", true: "missing snapshot"}[missing], func(t *testing.T) {
			paths, legacy := migrationFixture(t)
			owner, err := authorityFor(legacy)
			if err != nil {
				t.Fatal(err)
			}
			if missing {
				if err := os.Remove(legacy.StatefilePath); err != nil {
					t.Fatal(err)
				}
			} else {
				owner, err = stateauthority.New(owner.StatefileAuthority(), filepath.Join(t.TempDir(), "foreign.toml"))
				if err != nil {
					t.Fatal(err)
				}
			}
			binding := migrationDocumentBinding(t, owner, filepath.Join(os.Getenv("HOME"), "settings.json"))
			registry, err := ownership.NewRegistry(nil, binding)
			if err != nil {
				t.Fatal(err)
			}
			before, err := ownershipstore.Marshal(registry)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, paths.OwnershipRegistryPath, before)
			prepared, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
			if prepared != nil {
				prepared.Close()
			}
			if err == nil {
				t.Fatal("incomplete binding authority was accepted for migration")
			}
			after, err := os.ReadFile(paths.OwnershipRegistryPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed preview changed binding metadata: %v", err)
			}
			if _, err := os.Lstat(paths.StatefilePath); !os.IsNotExist(err) {
				t.Fatalf("failed preview created destination state: %v", err)
			}
		})
	}
}
