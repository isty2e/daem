//go:build darwin

package mutation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

func TestNativeDarwinKeysLocateAndDistinguishExistingEntries(t *testing.T) {
	root := t.TempDir()
	var previous PersistedDirectoryEntryAuthority
	for _, name := range []string{"i", "İ", "ẞ", "Σ", "Caf\u00e9", strings.Repeat("Ⱥ", 100)} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		observed, err := ObservePersistedDirectoryEntryAuthority(path)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		located, err := os.Stat(observed.Exact().Key())
		if err != nil || !os.SameFile(actual, located) {
			t.Fatal("comparison key did not locate its recorded native entry", name, observed.Exact().Key(), err)
		}
		reference := observed.Reference()
		if err := reference.Validate(); err != nil || !reference.Identity().Equal(observed.Exact()) {
			t.Fatal("persisted observation lost its path reference", name, err)
		}
		accessed, err := os.Stat(reference.Locator())
		if err != nil || !os.SameFile(actual, accessed) {
			t.Fatal("recorded access spelling did not locate the selected entry", name, reference.Locator(), err)
		}
		if name == "İ" && observed.Exact().Equal(previous.Exact()) {
			t.Fatal("distinct i and İ entries received the same exact authority")
		}
		previous = observed
	}
}

func TestNativeDarwinHardLinksRetainDistinctEntryAuthorities(t *testing.T) {
	for _, created := range []string{"i", "İ"} {
		for _, queried := range []string{"i", "İ"} {
			t.Run("created-"+created+"/queried-"+queried, func(t *testing.T) {
				root := t.TempDir()
				other := "İ"
				if created == other {
					other = "i"
				}
				if err := os.WriteFile(filepath.Join(root, created), []byte("shared inode\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(filepath.Join(root, created), filepath.Join(root, other)); err != nil {
					t.Fatal(err)
				}
				other = "İ"
				if queried == other {
					other = "i"
				}
				first, err := ObservePersistedDirectoryEntryAuthority(filepath.Join(root, queried))
				if err != nil {
					t.Fatal(err)
				}
				second, err := ObservePersistedDirectoryEntryAuthority(filepath.Join(root, other))
				if err != nil || first.Exact().Equal(second.Exact()) {
					t.Fatal("distinct hard-link entries collapsed into one namespace", err)
				}
			})
		}
	}
}

func TestNativeDarwinLeaseSharesLegacyLocksWithoutGrantingLegacyAuthority(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "İ", "point.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("point\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := NewLogicalPathDomain(LogicalPathRequest{Path: path, Effect: PathEffectReferent, Access: AccessExclusive})
	if err != nil {
		t.Fatal(err)
	}
	point, err := pathauthority.NewExact(current.canonicalPath, string(current.pathWitness))
	if err != nil {
		t.Fatal(err)
	}
	legacyKey, native := point.LegacyComparisonKey()
	if !native {
		t.Skip("filesystem lookup already preserves this folded entry")
	}
	store, err := NewStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		modes  []AccessMode
		strong AccessMode
	}{
		{name: "shared", modes: []AccessMode{AccessShared}, strong: AccessShared},
		{name: "shared-shared", modes: []AccessMode{AccessShared, AccessShared}, strong: AccessShared},
		{name: "exclusive", modes: []AccessMode{AccessExclusive}, strong: AccessExclusive},
		{name: "shared-exclusive", modes: []AccessMode{AccessShared, AccessExclusive}, strong: AccessExclusive},
		{name: "exclusive-shared", modes: []AccessMode{AccessExclusive, AccessShared}, strong: AccessExclusive},
	} {
		t.Run(test.name, func(t *testing.T) {
			domains := make([]Domain, len(test.modes))
			for index, mode := range test.modes {
				domains[index] = current
				domains[index].access = mode
			}
			locks, err := store.normalize(domains)
			if err != nil {
				t.Fatal(err)
			}
			assertMutationTestDomainMode(t, locks, pathKey(current.canonicalPath), test.strong)
			assertMutationTestDomainMode(t, locks, pathKey(legacyKey), test.strong)
			assertMutationTestDomainMode(t, locks, pathKey(filepath.Dir(legacyKey)), AccessShared)
		})
	}
	legacy := current
	legacy.canonicalPath = legacyKey
	legacy.pathWitness = pathSemanticsWitness(strings.ReplaceAll(strings.Replace(string(current.pathWitness), "darwin-case-v2:", "darwin-case-v1:", 1), "u", "i"))
	if covered, err := coversExclusiveDomains([]Domain{current}, []Domain{legacy}); err != nil || covered {
		t.Fatal("compatibility lock was promoted into filesystem authority", err)
	}
}

func TestNativeDarwinStoredKeyKeepsHealthyLegacyIdentity(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"StoredName", "Caf\u00e9", "ẞ"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		current, err := canonicalPathIdentity(path, PathEffectDirectoryEntry)
		if err != nil {
			t.Fatal(err)
		}
		_, names, err := darwinPathComponents(current.accessPath)
		if err != nil {
			t.Fatal(err)
		}
		components := make([]observedPathComponent, 0, len(names))
		parent := string(filepath.Separator)
		for _, name := range names {
			mode, err := darwinDirectoryCaseSemantics(parent)
			if err != nil {
				t.Fatal(err)
			}
			components = append(components, observedPathComponent{spelling: name, caseMode: mode})
			parent = filepath.Join(parent, name)
		}
		legacy, err := canonicalObservedPath(string(filepath.Separator), components, "darwin-case-v1")
		if err != nil {
			t.Fatal(err)
		}
		if current.keyPath != legacy.keyPath || current.witness != legacy.witness {
			t.Fatal("healthy legacy authority changed", name, current, legacy)
		}
	}
}
