package cli_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeMixedPreviewAndWriteShareSupportAdmission(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[instructions.project]
source = "./instructions.md"
[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
	testkit.WriteFile(t, workspace, "instructions.md", "project instructions\n")
	testkit.WriteFile(t, workspace, "preferences.toml", "[tui]\ntheme = 'test'\n")
	nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)

	for _, jsonOutput := range []bool{false, true} {
		arguments := []string{"apply", "--manifest", manifest, "--dry-run"}
		if jsonOutput {
			arguments = append(arguments, "--json")
		}
		code, stdout, stderr := testkit.RunCapturedCLI(arguments...)
		if code != 0 || jsonOutput && !json.Valid([]byte(stdout)) {
			t.Fatalf("mixed preview was not executable: %q code=%d stdout=%s stderr=%s", arguments, code, stdout, stderr)
		}
		for _, relative := range []string{"AGENTS.md", ".codex/config.toml", ".daem/state.json"} {
			if _, err := os.Lstat(filepath.Join(workspace, relative)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preview changed %s: %v", relative, err)
			}
		}
	}
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes", "--json")
	if actual := string(testkit.ReadFile(t, filepath.Join(workspace, "AGENTS.md"))); actual != "project instructions\n" {
		t.Fatalf("confirmed mixed Apply omitted the ordinary output: %q", actual)
	}
	nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
}
