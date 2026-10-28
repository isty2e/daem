package cli_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestNativeMixedPreviewAndWriteShareSupportRefusal(t *testing.T) {
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
	runNativeLifecycleCommand(t, "lock", "--manifest", manifest)

	for _, jsonOutput := range []bool{false, true} {
		for _, mode := range []string{"--dry-run", "--yes"} {
			arguments := []string{"apply", "--manifest", manifest, mode}
			if jsonOutput {
				arguments = append(arguments, "--json")
			}
			code, stdout, stderr := runOwnershipCLI(arguments...)
			if code == 0 {
				t.Fatalf("unsupported mixed plan was published as executable: %q %s", arguments, stdout)
			}
			if !strings.Contains(stdout+stderr, "ordinary file updates") {
				t.Fatalf("basic failure omitted the common readable cause: %s %s", stdout, stderr)
			}
			if !jsonOutput && mode == "--dry-run" {
				if !strings.Contains(stdout, "not executable") || !strings.Contains(stdout, "preferences") || !strings.Contains(stdout, "tui.theme") || !strings.Contains(stdout, "instruction/project") || !strings.Contains(stderr, "apply preview failed:") || strings.Contains(stderr, "apply failed:") {
					t.Fatalf("refused preview must retain affected actions and identify its phase: stdout=%s stderr=%s", stdout, stderr)
				}
			}
			if jsonOutput && !json.Valid([]byte(stdout)) {
				t.Fatalf("support refusal returned invalid JSON: %s", stdout)
			}
			for _, relative := range []string{"AGENTS.md", ".codex/config.toml", ".daem/state.json"} {
				if _, err := os.Lstat(filepath.Join(workspace, relative)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("refused mixed plan changed %s: %v", relative, err)
				}
			}
		}
	}
}
