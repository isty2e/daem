package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
	"github.com/pelletier/go-toml/v2"
)

func TestNativeKeyCLILifecycle(t *testing.T) {
	// Existing documents and their comments are covered by TestNativeKeySharedCLILifecycle.
	t.Run("missing document", func(t *testing.T) {
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

		for _, args := range [][]string{
			{"lock", "--manifest", manifest},
			{"apply", "--manifest", manifest, "--yes"},
		} {
			if code, stdout, stderr := testkit.RunCapturedCLI(args...); code != 0 {
				t.Fatalf("%q failed: exit=%d stdout=%s stderr=%s", args, code, stdout, stderr)
			}
		}
		content, err := os.ReadFile(destination)
		if err != nil {
			t.Fatal("applied key document is missing", err)
		}
		var values struct {
			Model *string `toml:"model"`
		}
		if err := toml.Unmarshal(content, &values); err != nil || values.Model == nil || *values.Model != "native-test" {
			t.Fatal("CLI apply did not create the selected managed value", err)
		}
		nativefixture.RequireLifecycleMetadata(t, manifest, true)
		code, stdout, stderr := testkit.RunCapturedCLI("status", "--manifest", manifest, "--check", "--json")
		if code != 0 {
			t.Fatalf("status after apply failed: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
		}
		nativefixture.RequireLifecycleClean(t, stdout)

		testkit.WriteFile(t, workspace, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
		for _, args := range [][]string{
			{"lock", "--manifest", manifest},
			{"apply", "--manifest", manifest, "--yes"},
		} {
			if code, stdout, stderr := testkit.RunCapturedCLI(args...); code != 0 {
				t.Fatalf("last native declaration removal %q failed: exit=%d stdout=%s stderr=%s", args, code, stdout, stderr)
			}
		}
		content, err = os.ReadFile(destination)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal("reading released native document failed", err)
		}
		values.Model = nil
		if err == nil {
			if err := toml.Unmarshal(content, &values); err != nil {
				t.Fatal("native removal left invalid TOML", err)
			}
		}
		if values.Model != nil {
			t.Fatal("declaration removal retained its managed key")
		}
		if code, stdout, stderr := testkit.RunCapturedCLI("status", "--manifest", manifest, "--check", "--json"); code != 0 {
			t.Fatalf("status after last native removal failed: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
		} else {
			nativefixture.RequireLifecycleClean(t, stdout)
		}
		nativefixture.RequireLifecycleMetadata(t, manifest, false)
	})
}
