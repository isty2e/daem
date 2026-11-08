package native

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func protectionReference(t *testing.T, identity string) output.RootReference {
	t.Helper()
	contracts, err := configuration.NativeProtectionLocations()
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range contracts {
		if contract.Reference().Identity() == identity {
			return contract.Reference()
		}
	}
	t.Fatalf("protection contract %q is missing", identity)
	return output.RootReference{}
}

func TestOpenCodeDefaultAndAdditionalLocationsCoexist(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	locations, err := NewLocations(work, home, map[string]string{"OPENCODE_CONFIG_DIR": "extra"})
	if err != nil {
		t.Fatal(err)
	}
	writer, _ := configuration.RootReferenceForScope(target.TargetOpenCode, target.ScopeGlobal)
	selected, err := locations.Resolve(writer)
	if err != nil || selected.Path() != filepath.Join(home, ".config", "opencode") {
		t.Fatal("an additional directory replaced the admitted XDG writer root", selected, err)
	}
	for _, row := range []struct{ identity, path string }{
		{"opencode-additional-configuration", filepath.Join(work, "extra")},
		{"opencode-legacy-configuration", filepath.Join(home, ".opencode")},
		{"opencode-data", filepath.Join(home, ".local", "share", "opencode")},
		{"opencode-state", filepath.Join(home, ".local", "state", "opencode")},
		{"opencode-cache", filepath.Join(home, ".cache", "opencode")},
	} {
		reference := protectionReference(t, row.identity)
		location, err := locations.ResolveProtection(reference)
		if err != nil || location.Absent() || location.Path() != row.path {
			t.Fatalf("%s = %#v, %v; want %q", row.identity, location, err, row.path)
		}
		if _, err := locations.Resolve(reference); err == nil {
			t.Fatalf("read-only %s was promoted to a native writer selector", row.identity)
		}
	}
}

func TestOpenCodeSelectorsPreserveTheirDistinctRelativeContracts(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	locations, err := NewLocations(work, home, map[string]string{
		"XDG_CONFIG_HOME": "link/../configuration", "XDG_DATA_HOME": "data",
		"XDG_STATE_HOME": "", "XDG_CACHE_HOME": "cache",
		"OPENCODE_DB": "../outside.db", "OPENCODE_PLUGIN_META_FILE": "link/../metadata.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ identity, path, source string }{
		{"opencode-configuration", filepath.Join(work, "configuration", "opencode"), "XDG_CONFIG_HOME"},
		{"opencode-data", filepath.Join(work, "data", "opencode"), "XDG_DATA_HOME"},
		{"opencode-state", filepath.Join(home, ".local", "state", "opencode"), "default location"},
		{"opencode-cache", filepath.Join(work, "cache", "opencode"), "XDG_CACHE_HOME"},
		{"opencode-database-override", filepath.Join(work, "data", "outside.db"), "OPENCODE_DB"},
		{"opencode-plugin-metadata-override", work + string(filepath.Separator) + "link/../metadata.json", "OPENCODE_PLUGIN_META_FILE"},
	} {
		location, err := locations.ResolveProtection(protectionReference(t, row.identity))
		if err != nil || location.Absent() || location.Path() != row.path || location.Source() != row.source {
			t.Fatalf("%s = %#v, %v; want %q from %s", row.identity, location, err, row.path, row.source)
		}
	}
	writer, _ := configuration.RootReferenceForScope(target.TargetOpenCode, target.ScopeGlobal)
	if location, err := locations.Resolve(writer); err == nil || location.Source() != "XDG_CONFIG_HOME" {
		t.Fatal("relative protection selection widened global writer admission", location, err)
	}
}

func TestOpenCodeLeadingParentsDoNotCleanTheCapturedContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Win32 resolves lexical parents before reparse traversal")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	actual := filepath.Join(base, "actual")
	if err := os.MkdirAll(filepath.Join(actual, "inner"), 0o700); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(actual, "inner"), work); err != nil {
		t.Fatal(err)
	}
	locations, err := NewLocations(work, t.TempDir(), map[string]string{
		"XDG_CONFIG_HOME": "../configuration", "OPENCODE_CONFIG_DIR": "../extra",
		"XDG_DATA_HOME": "../data", "OPENCODE_DB": "../outside.db",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		identity, relative string
		file               bool
	}{
		{"opencode-configuration", "configuration/opencode", false},
		{"opencode-additional-configuration", "extra", false},
		{"opencode-data", "data/opencode", false},
		{"opencode-database-override", "data/outside.db", true},
	} {
		want := filepath.Join(actual, filepath.FromSlash(row.relative))
		for _, fixture := range []string{want, filepath.Join(base, filepath.FromSlash(row.relative))} {
			if row.file {
				if err := os.MkdirAll(filepath.Dir(fixture), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fixture, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.MkdirAll(fixture, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		location, err := locations.ResolveProtection(protectionReference(t, row.identity))
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := filepath.EvalSymlinks(location.Path())
		if err != nil || resolved != want {
			t.Fatalf("%s cleaned through its physical context: %q => %q, %v; want %q", row.identity, location.Path(), resolved, err, want)
		}
	}
}

func TestOpenCodeOptionalLocationsDistinguishUnsetEmptyAndMemory(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	for _, values := range []map[string]string{nil, {"OPENCODE_DB": ":memory:"}, {"OPENCODE_DB": ""}} {
		locations, err := NewLocations(work, home, values)
		if err != nil {
			t.Fatal(err)
		}
		for _, identity := range []string{"opencode-additional-configuration", "opencode-database-override", "opencode-plugin-metadata-override"} {
			location, err := locations.ResolveProtection(protectionReference(t, identity))
			if err != nil || !location.Absent() || location.Path() != "" {
				t.Fatal("optional selector invented an additional path", identity, location, err)
			}
		}
	}
	locations, err := NewLocations(work, home, map[string]string{"OPENCODE_CONFIG_DIR": "", "OPENCODE_PLUGIN_META_FILE": ""})
	if err != nil {
		t.Fatal(err)
	}
	extra, err := locations.ResolveProtection(protectionReference(t, "opencode-additional-configuration"))
	if err != nil || extra.Absent() || extra.Path() != work {
		t.Fatal("presence-sensitive core configuration lost its cwd base", extra, err)
	}
	if _, err := locations.ResolveProtection(protectionReference(t, "opencode-plugin-metadata-override")); err == nil {
		t.Fatal("empty metadata override became an absent/default file")
	}
}

func TestOpenCodeFileOverridesDoNotGrantTheirParentsOrDiscloseInvalidValues(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	file := work + string(filepath.Separator) + "link/../private.db"
	locations, err := NewLocations(work, home, map[string]string{"OPENCODE_DB": file, "OPENCODE_PLUGIN_META_FILE": "private\x00file"})
	if err != nil {
		t.Fatal(err)
	}
	reference := protectionReference(t, "opencode-database-override")
	database, err := locations.ResolveProtection(reference)
	if err != nil || database.Path() != file || database.Absent() {
		t.Fatal("absolute database spelling was lexically changed or widened", database, err)
	}
	if _, err := locations.Resolve(reference); err == nil {
		t.Fatal("database selector became a writable native root")
	}
	if _, err := locations.ResolveProtection(protectionReference(t, "opencode-plugin-metadata-override")); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("invalid file selector was admitted or disclosed", err)
	}
}
