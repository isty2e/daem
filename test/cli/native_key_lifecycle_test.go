package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/pelletier/go-toml/v2"
)

func TestNativeKeyCLILifecycle(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "missing document"
		if existing {
			name = "existing shared document"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]

[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
			testkit.WriteFile(t, workspace, "preferences.toml", "model = \"native-test\"\n")
			destination := filepath.Join(workspace, ".codex", "config.toml")
			const unmanaged = "# User-owned comment\nunmanaged = true\n"
			if existing {
				testkit.WriteFile(t, workspace, ".codex/config.toml", unmanaged)
			}

			for _, args := range [][]string{
				{"lock", "--manifest", manifest},
				{"apply", "--manifest", manifest, "--yes"},
			} {
				if code, stdout, stderr := runOwnershipCLI(args...); code != 0 {
					t.Fatalf("%q failed: exit=%d stdout=%s stderr=%s", args, code, stdout, stderr)
				}
			}
			content, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal("applied key document is missing", err)
			}
			var values struct {
				Model     *string `toml:"model"`
				Unmanaged bool    `toml:"unmanaged"`
			}
			if err := toml.Unmarshal(content, &values); err != nil || (values.Model == nil || *values.Model != "native-test") {
				t.Fatal("CLI apply did not create the selected managed value", err)
			}
			if existing && (!values.Unmanaged || !bytes.HasPrefix(content, []byte("# User-owned comment\n"))) {
				t.Fatal("adding a managed key lost unrelated values or their comment")
			}
			requireNativeLifecycleMetadata(t, manifest, true)
			code, stdout, stderr := runOwnershipCLI("status", "--manifest", manifest, "--check", "--json")
			if code != 0 {
				t.Fatalf("status after apply failed: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
			requireNativeLifecycleClean(t, stdout)

			testkit.WriteFile(t, workspace, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
			for _, args := range [][]string{
				{"lock", "--manifest", manifest},
				{"apply", "--manifest", manifest, "--yes"},
			} {
				if code, stdout, stderr := runOwnershipCLI(args...); code != 0 {
					t.Fatalf("last native declaration removal %q failed: exit=%d stdout=%s stderr=%s", args, code, stdout, stderr)
				}
			}
			content, err = os.ReadFile(destination)
			if err != nil && (existing || !os.IsNotExist(err)) {
				t.Fatal("reading released native document failed", err)
			}
			values.Model = nil
			values.Unmanaged = false
			if err == nil {
				if err := toml.Unmarshal(content, &values); err != nil {
					t.Fatal("native removal left invalid TOML", err)
				}
			}
			if values.Model != nil {
				t.Fatal("declaration removal retained its managed key")
			}
			if existing && (!values.Unmanaged || !bytes.Contains(content, []byte("# User-owned comment\n"))) {
				t.Fatal("declaration removal changed unrelated values or their comment")
			}
			if code, stdout, stderr := runOwnershipCLI("status", "--manifest", manifest, "--check", "--json"); code != 0 {
				t.Fatalf("status after last native removal failed: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
			} else {
				requireNativeLifecycleClean(t, stdout)
			}
			requireNativeLifecycleMetadata(t, manifest, false)
		})
	}
}

func requireNativeLifecycleClean(t *testing.T, output string) {
	t.Helper()
	status := clijson.DecodePlan(t, []byte(output))
	if status.HasErrors || status.CheckClean == nil || !*status.CheckClean {
		t.Fatal("persisted native lifecycle is not clean in real status", output)
	}
	for _, action := range status.Actions {
		if action.Kind != "noop" {
			t.Fatal("clean status still plans a state or host change", output)
		}
	}
}
