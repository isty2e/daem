package cli_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeKeyUsageWorkflow(t *testing.T) {
	for _, example := range []struct {
		target   string
		filename string
		fragment string
		outside  string
		syntax   configdocument.Syntax
	}{
		{"codex", "config.toml", "[tui]\nnotifications = false\n", "# Personal settings\nmodel = 'personal'\n", configdocument.TOML},
		{"pi", "settings.json", "{\"retry\":{\"enabled\":true}}\n", "{\"defaultModel\":\"personal\"}\n", configdocument.JSON},
	} {
		t.Run(example.target, func(t *testing.T) {
			root := t.TempDir()
			setNativeRecoveryEnvironment(t, root)
			workspace := filepath.Join(root, "workspace")
			source := example.target + "-preferences" + filepath.Ext(example.filename)
			testkit.WriteFile(t, workspace, source, example.fragment)
			manifest := filepath.Join(workspace, "daem.toml")
			testkit.WriteFile(t, workspace, "daem.toml", fmt.Sprintf(`version = 1
targets = [%q]
[[host_config]]
id = %q
targets = [%q]
scope = "project"
path = %q
source = %q
ownership = "keys"
`, example.target, example.target+"-preferences", example.target, example.filename, "./"+source))
			destination := filepath.Join(workspace, "."+example.target, example.filename)
			testkit.WriteFile(t, workspace, "."+example.target+"/"+example.filename, example.outside)
			t.Chdir(workspace)
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			fixture := nativeRecoveryFixture{manifest: manifest, destination: destination, paths: paths}
			runNativeLifecycleCommand(t, "lock")
			before := snapshotNativeRecoveryEvidence(t, fixture)
			runNativeLifecycleCommand(t, "apply", "--dry-run")
			if !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, fixture)) {
				t.Fatal("usage preview changed host or persistence")
			}
			runNativeLifecycleCommand(t, "apply", "--yes")
			runNativeLifecycleCommand(t, "status", "--check")

			content := testkit.ReadFile(t, destination)
			requireNativeSharedValues(t, example.syntax, content, example.fragment, true)
			requireNativeSharedValues(t, example.syntax, content, example.outside, true)
			requireNativeLifecycleMetadata(t, manifest, true)

			testkit.WriteFile(t, workspace, "daem.toml", fmt.Sprintf("version = 1\ntargets = [%q]\n", example.target))
			runNativeLifecycleCommand(t, "lock")
			runNativeLifecycleCommand(t, "apply", "--yes")
			runNativeLifecycleCommand(t, "status", "--check")
			content = testkit.ReadFile(t, destination)
			requireNativeSharedValues(t, example.syntax, content, example.fragment, false)
			requireNativeSharedValues(t, example.syntax, content, example.outside, true)
			requireNativeLifecycleMetadata(t, manifest, false)
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}
