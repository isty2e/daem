package stateauthority_test

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

func TestAuthorityValidationAndZeroSemantics(t *testing.T) {
	root := t.TempDir()
	statefileKey := filepath.Join(root, "state.json")
	manifestPath := filepath.Join(root, "daem.toml")

	tests := []struct {
		name         string
		statefileKey string
		manifestPath string
		wantError    string
	}{
		{
			name:         "missing statefile key",
			manifestPath: manifestPath,
			wantError:    "statefile authority key is required",
		},
		{
			name:         "missing manifest path",
			statefileKey: statefileKey,
			wantError:    "manifest provenance path is required",
		},
		{
			name:         "relative statefile key",
			statefileKey: "state.json",
			manifestPath: manifestPath,
			wantError:    `statefile authority key "state.json" must be absolute`,
		},
		{
			name:         "relative manifest path",
			statefileKey: statefileKey,
			manifestPath: "daem.toml",
			wantError:    `manifest provenance path "daem.toml" must be absolute`,
		},
		{
			name: "unclean statefile key",
			statefileKey: root + string(filepath.Separator) + "nested" +
				string(filepath.Separator) + ".." + string(filepath.Separator) + "state.json",
			manifestPath: manifestPath,
			wantError: "statefile authority key \"" + root + string(filepath.Separator) +
				"nested" + string(filepath.Separator) + ".." +
				string(filepath.Separator) + `state.json" must be clean`,
		},
		{
			name:         "unclean manifest path",
			statefileKey: statefileKey,
			manifestPath: root + string(filepath.Separator) + "nested" +
				string(filepath.Separator) + ".." + string(filepath.Separator) + "daem.toml",
			wantError: "manifest provenance path \"" + root + string(filepath.Separator) +
				"nested" + string(filepath.Separator) + ".." +
				string(filepath.Separator) + `daem.toml" must be clean`,
		},
		{
			name:         "NUL statefile key",
			statefileKey: statefileKey + "\x00",
			manifestPath: manifestPath,
			wantError:    "statefile authority key contains a NUL byte",
		},
		{
			name:         "NUL manifest path",
			statefileKey: statefileKey,
			manifestPath: manifestPath + "\x00",
			wantError:    "manifest provenance path contains a NUL byte",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authority, err := stateauthority.New(test.statefileKey, test.manifestPath)
			if err == nil {
				t.Fatalf("New(%q, %q) = %#v, want error", test.statefileKey, test.manifestPath, authority)
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
	key, err := stateauthority.NewKey(canonical)
	if err != nil {
		t.Fatalf("NewKey returned error: %v", err)
	}
	if key.String() != canonical {
		t.Fatalf("key = %q, want %q", key.String(), canonical)
	}
	if err := (stateauthority.Key{}).Validate(); err == nil {
		t.Fatal("zero Key validated")
	}

	for name, value := range map[string]string{
		"empty":    "",
		"relative": "state.json",
		"unclean": root + string(filepath.Separator) + "nested" +
			string(filepath.Separator) + ".." + string(filepath.Separator) + "state.json",
		"NUL": canonical + "\x00",
	} {
		t.Run(name, func(t *testing.T) {
			if forged, err := stateauthority.NewKey(value); err == nil {
				t.Fatalf("NewKey(%q) = %#v, want error", value, forged)
			}
		})
	}

	whitespace := filepath.Join(root, "state directory\n", "state.json ")
	whitespaceKey, err := stateauthority.NewKey(whitespace)
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
	first, err := stateauthority.New(statefileKey, filepath.Join(root, "first.toml"))
	if err != nil {
		t.Fatalf("New first authority: %v", err)
	}
	second, err := stateauthority.New(statefileKey, filepath.Join(root, "second.toml"))
	if err != nil {
		t.Fatalf("New second authority: %v", err)
	}
	foreign, err := stateauthority.New(filepath.Join(root, "other-state.json"), first.ManifestPath())
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
	authority, err := stateauthority.New(statefileKey, manifestPath)
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
