package stateauthority_test

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

func TestAuthorityValidationAndZeroSemantics(t *testing.T) {
	root := t.TempDir()
	statefile := mustExact(t, filepath.Join(root, "state.json"))
	manifestPath := filepath.Join(root, "daem.toml")

	tests := []struct {
		name      string
		statefile pathauthority.Exact
		manifest  string
		wantError string
	}{
		{
			name:      "missing statefile authority",
			manifest:  manifestPath,
			wantError: "statefile authority key: exact path authority key is required",
		},
		{
			name:      "missing manifest path",
			statefile: statefile,
			wantError: "manifest provenance path is required",
		},
		{
			name:      "unclean manifest path",
			statefile: statefile,
			manifest: root + string(filepath.Separator) + "nested" +
				string(filepath.Separator) + ".." + string(filepath.Separator) + "daem.toml",
			wantError: "manifest provenance path \"" + root + string(filepath.Separator) +
				"nested" + string(filepath.Separator) + ".." +
				string(filepath.Separator) + `daem.toml" must be clean`,
		},
		{
			name:      "NUL manifest path",
			statefile: statefile,
			manifest:  manifestPath + "\x00",
			wantError: "manifest provenance path contains a NUL byte",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authority, err := stateauthority.New(test.statefile, test.manifest)
			if err == nil {
				t.Fatalf("New(%q) = %#v, want error", test.manifest, authority)
			}
			if got := err.Error(); got != test.wantError {
				t.Fatalf("error = %q, want %q", got, test.wantError)
			}
		})
	}

	var zero stateauthority.Authority
	if !zero.IsZero() {
		t.Fatal("zero Authority did not report IsZero")
	}
	if err := zero.Validate(); err == nil {
		t.Fatal("zero Authority validated")
	}
}

func TestKeyValidation(t *testing.T) {
	root := t.TempDir()
	canonical := filepath.Join(root, "state.json")
	exact := mustExact(t, canonical)
	key, err := stateauthority.NewKey(exact)
	if err != nil {
		t.Fatalf("NewKey returned error: %v", err)
	}
	if key.String() != canonical {
		t.Fatalf("key = %q, want %q", key.String(), canonical)
	}
	if err := (stateauthority.Key{}).Validate(); err == nil {
		t.Fatal("zero Key validated")
	}

	whitespace := filepath.Join(root, "state directory\n", "state.json ")
	whitespaceKey, err := stateauthority.NewKey(mustExact(t, whitespace))
	if err != nil {
		t.Fatalf("NewKey preserved whitespace: %v", err)
	}
	if whitespaceKey.String() != whitespace {
		t.Fatalf("whitespace key = %q, want %q", whitespaceKey.String(), whitespace)
	}
}

func TestAuthorityEqualitySeparatesIdentityFromProvenance(t *testing.T) {
	root := t.TempDir()
	statefileKey := filepath.Join(root, "state.json")
	statefile := mustExact(t, statefileKey)
	first, err := stateauthority.New(statefile, filepath.Join(root, "first.toml"))
	if err != nil {
		t.Fatalf("New first authority: %v", err)
	}
	second, err := stateauthority.New(statefile, filepath.Join(root, "second.toml"))
	if err != nil {
		t.Fatalf("New second authority: %v", err)
	}
	foreign, err := stateauthority.New(mustExact(t, filepath.Join(root, "other-state.json")), first.ManifestPath())
	if err != nil {
		t.Fatalf("New foreign authority: %v", err)
	}

	if !first.Equal(second) {
		t.Fatal("same statefile key did not identify the same authority")
	}
	if first.ExactEqual(second) {
		t.Fatal("different manifest provenance compared exactly equal")
	}
	if first.Equal(foreign) || first.ExactEqual(foreign) {
		t.Fatal("different statefile key compared equal")
	}
	if !first.ExactEqual(first) {
		t.Fatal("authority did not compare exactly equal to itself")
	}
}

func TestAuthorityPreservesCanonicalWhitespace(t *testing.T) {
	root := t.TempDir()
	statefileKey := filepath.Join(root, "state directory\n", "state.json")
	manifestPath := filepath.Join(root, "manifest directory ", "daem.toml\n")
	authority, err := stateauthority.New(mustExact(t, statefileKey), manifestPath)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if authority.StatefileKey() != statefileKey || authority.ManifestPath() != manifestPath {
		t.Fatalf(
			"authority paths = (%q, %q), want (%q, %q)",
			authority.StatefileKey(),
			authority.ManifestPath(),
			statefileKey,
			manifestPath,
		)
	}
}

func mustExact(t *testing.T, key string) pathauthority.Exact {
	t.Helper()
	authority, err := pathauthority.NewExact(key, "exact-v1:")
	if err != nil {
		t.Fatalf("NewExact(%q): %v", key, err)
	}
	return authority
}
