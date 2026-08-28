package antigravityplugin

import (
	"os"
	"path/filepath"
	"testing"

	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestInventoryCorrelatesOnlyCompleteImportBundlePairs(t *testing.T) {
	paths, key, carrier := antigravityInventoryFixture(t, "guidance@google")

	inventory := mustReadInventory(t, paths)
	assertAntigravityCorrelationState(
		t,
		inventory,
		key,
		carrier,
		relationobserve.StateMissing,
	)

	writeAntigravityImportManifest(t, paths, `{"imports":[{"name":"guidance","source":"antigravity"}]}`)
	writeAntigravityPluginManifest(t, paths, "guidance", `{"name":"guidance","version":"1.0.0"}`)
	inventory = mustReadInventory(t, paths)
	assertAntigravityCorrelationState(
		t,
		inventory,
		key,
		carrier,
		relationobserve.StateUnkeyedSameSubject,
	)

	if err := os.RemoveAll(filepath.Dir(mustPluginManifestPath(t, paths, "guidance"))); err != nil {
		t.Fatal(err)
	}
	writeAntigravityImportManifest(t, paths, `{"imports":null}`)
	inventory = mustReadInventory(t, paths)
	assertAntigravityCorrelationState(
		t,
		inventory,
		key,
		carrier,
		relationobserve.StateMissing,
	)
}

func TestInventoryRejectsPartialDuplicateAndMismatchedRelations(t *testing.T) {
	tests := []struct {
		name    string
		imports string
		bundle  string
	}{
		{
			name:    "import row only",
			imports: `{"imports":[{"name":"guidance"}]}`,
		},
		{
			name:   "bundle only",
			bundle: `{"name":"guidance"}`,
		},
		{
			name:    "duplicate selected rows",
			imports: `{"imports":[{"name":"guidance"},{"name":"guidance"}]}`,
			bundle:  `{"name":"guidance"}`,
		},
		{
			name:    "mismatched bundle identity",
			imports: `{"imports":[{"name":"guidance"}]}`,
			bundle:  `{"name":"other"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths, key, carrier := antigravityInventoryFixture(t, "guidance@google")
			if test.imports != "" {
				writeAntigravityImportManifest(t, paths, test.imports)
			}
			if test.bundle != "" {
				writeAntigravityPluginManifest(t, paths, "guidance", test.bundle)
			}
			inventory := mustReadInventory(t, paths)
			if _, err := inventory.CorrelateDesired(key, carrier); err == nil {
				t.Fatal("unsafe Antigravity relation was accepted")
			}
		})
	}
	t.Run("empty bundle directory", func(t *testing.T) {
		paths, key, carrier := antigravityInventoryFixture(t, "guidance@google")
		writeAntigravityImportManifest(t, paths, `{"imports":[{"name":"guidance"}]}`)
		directory, err := paths.PluginDirectoryPath("guidance")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		inventory := mustReadInventory(t, paths)
		if _, err := inventory.CorrelateDesired(key, carrier); err == nil {
			t.Fatal("empty desired plugin bundle was accepted")
		}
	})
}

func TestVisitCompletePluginNamesReturnsOnlyValidatedSourceInexactRows(t *testing.T) {
	paths, _, _ := antigravityInventoryFixture(t, "guidance@google")
	writeAntigravityImportManifest(
		t,
		paths,
		`{"imports":[{"name":"tools"},{"name":"guidance"}]}`,
	)
	writeAntigravityPluginManifest(t, paths, "guidance", `{"name":"guidance"}`)
	writeAntigravityPluginManifest(t, paths, "tools", `{"name":"tools"}`)

	inventory := mustReadInventory(t, paths)
	names, err := collectCompletePluginNames(t, inventory)
	if err != nil {
		t.Fatalf("VisitCompletePluginNames: %v", err)
	}
	if len(names) != 2 || names[0] != "guidance" || names[1] != "tools" {
		t.Fatalf("visited plugin names = %#v", names)
	}

	if err := os.Remove(mustPluginManifestPath(t, paths, "tools")); err != nil {
		t.Fatal(err)
	}
	inventory = mustReadInventory(t, paths)
	if _, err := collectCompletePluginNames(t, inventory); err == nil {
		t.Fatal("partial plugin relation was accepted as import diagnostic")
	}
}

func TestRemovalCorrelationKeepsPartialStateLiveForRetry(t *testing.T) {
	tests := []struct {
		name    string
		imports string
		bundle  string
	}{
		{
			name:    "import row remains",
			imports: `{"imports":[{"name":"guidance"}]}`,
		},
		{
			name:   "bundle remains",
			bundle: `{"name":"guidance"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths, key, carrier := antigravityInventoryFixture(t, "guidance@google")
			if test.imports != "" {
				writeAntigravityImportManifest(t, paths, test.imports)
			}
			if test.bundle != "" {
				writeAntigravityPluginManifest(t, paths, "guidance", test.bundle)
			}
			inventory := mustReadInventory(t, paths)
			result, err := inventory.CorrelateRemoval(key, carrier)
			if err != nil {
				t.Fatal(err)
			}
			if result.State() != relationobserve.StateUnkeyedSameSubject {
				t.Fatalf(
					"partial removal correlation = %q, want bounded same-subject retry evidence",
					result.State(),
				)
			}
		})
	}
	t.Run("empty residual bundle", func(t *testing.T) {
		paths, key, carrier := antigravityInventoryFixture(t, "guidance@google")
		writeAntigravityImportManifest(t, paths, `{"imports":[{"name":"guidance"}]}`)
		directory, err := paths.PluginDirectoryPath("guidance")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		inventory := mustReadInventory(t, paths)
		result, err := inventory.CorrelateRemoval(key, carrier)
		if err != nil {
			t.Fatal(err)
		}
		if result.State() != relationobserve.StateUnkeyedSameSubject {
			t.Fatalf(
				"empty residual bundle correlation = %q, want bounded same-subject retry evidence",
				result.State(),
			)
		}
	})
}

func antigravityInventoryFixture(
	t *testing.T,
	selector string,
) (HostPaths, relationobserve.CorrelationKey, desiredextension.CarrierKey) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := ResolveHostPaths()
	if err != nil {
		t.Fatal(err)
	}
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		selector,
	)
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{
		Name:    "guidance",
		Carrier: desiredextension.CarrierAntigravityCLIPlugin,
		Target:  target.TargetAntigravityCLI,
		Scope:   target.ScopeGlobal,
		Source:  source,
	})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := extensiontopology.Relation(value)
	if err != nil {
		t.Fatal(err)
	}
	interpreted, err := extensiontopology.InterpretCarrierSource(carrier)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(interpreted.RelationIdentity())
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.Derive(carrier, subject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := relationobserve.NewCorrelationKey(subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	return paths, key, carrier
}

func collectCompletePluginNames(t *testing.T, inventory Inventory) ([]string, error) {
	t.Helper()
	names := make([]string, 0)
	_, err := inventory.VisitCompletePluginNames(t.Context(), func(name string) error {
		names = append(names, name)
		return nil
	})
	return names, err
}

func mustReadInventory(t *testing.T, paths HostPaths) Inventory {
	t.Helper()
	inventory, err := ReadInventory(paths)
	if err != nil {
		t.Fatal(err)
	}
	return inventory
}

func writeAntigravityImportManifest(t *testing.T, paths HostPaths, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(paths.ImportManifestPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ImportManifestPath(), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeAntigravityPluginManifest(
	t *testing.T,
	paths HostPaths,
	plugin string,
	content string,
) {
	t.Helper()
	path := mustPluginManifestPath(t, paths, plugin)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustPluginManifestPath(t *testing.T, paths HostPaths, plugin string) string {
	t.Helper()
	path, err := paths.PluginManifestPath(plugin)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func assertAntigravityCorrelationState(
	t *testing.T,
	inventory Inventory,
	key relationobserve.CorrelationKey,
	carrier desiredextension.CarrierKey,
	want relationobserve.CorrelationState,
) {
	t.Helper()
	result, err := inventory.CorrelateDesired(key, carrier)
	if err != nil {
		t.Fatal(err)
	}
	if result.State() != want {
		t.Fatalf("correlation state = %q, want %q", result.State(), want)
	}
}
