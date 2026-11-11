package mutation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistedReferenceUsesSelectedSpellingNotComparisonKey(t *testing.T) {
	locator := filepath.Join(t.TempDir(), "Selected.json")
	identity := canonicalPath{
		keyPath: strings.ToLower(locator), accessPath: locator, witness: "windows-fold-v1:",
	}
	observed, err := persistedDirectoryEntryAuthority(locator, identity)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Reference().Locator() != locator || observed.Exact().Key() != identity.keyPath {
		t.Fatal("canonical observation facts were conflated")
	}
}

func TestPersistedAuthorityRetainsSelectedAccessReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Settings.json")
	if err := os.WriteFile(path, []byte("selected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	reference := observed.Reference()
	if err := reference.Validate(); err != nil || !reference.Identity().Equal(observed.Exact()) {
		t.Fatal("persisted observation lost its reference", err)
	}
	selectedPath, err := CanonicalDirectoryEntryPath(path)
	if err != nil || reference.Locator() != selectedPath {
		t.Fatal("reference reconstructed access spelling from comparison identity", reference.Locator(), selectedPath, err)
	}
	selected, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	located, err := os.Stat(reference.Locator())
	if err != nil || !os.SameFile(selected, located) {
		t.Fatal("reference no longer selects the observed entry", err)
	}
	identity, err := ObserveDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	exact, present := identity.Exact()
	if !present || !exact.Equal(reference.Identity()) {
		t.Fatal("retaining access evidence changed comparison identity")
	}
}
