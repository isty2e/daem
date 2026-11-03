package cli_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
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
			nativefixture.SetRecoveryEnvironment(t, root)
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
			fixture := nativefixture.RecoveryFixture{Manifest: manifest, Destination: destination, Paths: paths}
			nativefixture.RunLifecycleCommand(t, "lock")
			before := nativefixture.SnapshotRecoveryEvidence(t, fixture)
			nativefixture.RunLifecycleCommand(t, "apply", "--dry-run")
			if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryEvidence(t, fixture)) {
				t.Fatal("usage preview changed host or persistence")
			}
			nativefixture.RunLifecycleCommand(t, "apply", "--yes")
			nativefixture.RunLifecycleCommand(t, "status", "--check")

			content := testkit.ReadFile(t, destination)
			nativefixture.RequireSharedValues(t, example.syntax, content, example.fragment, true)
			nativefixture.RequireSharedValues(t, example.syntax, content, example.outside, true)
			nativefixture.RequireLifecycleMetadata(t, manifest, true)

			testkit.WriteFile(t, workspace, "daem.toml", fmt.Sprintf("version = 1\ntargets = [%q]\n", example.target))
			nativefixture.RunLifecycleCommand(t, "lock")
			nativefixture.RunLifecycleCommand(t, "apply", "--yes")
			nativefixture.RunLifecycleCommand(t, "status", "--check")
			content = testkit.ReadFile(t, destination)
			nativefixture.RequireSharedValues(t, example.syntax, content, example.fragment, false)
			nativefixture.RequireSharedValues(t, example.syntax, content, example.outside, true)
			nativefixture.RequireLifecycleMetadata(t, manifest, false)
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}
