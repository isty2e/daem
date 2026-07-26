package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyManageExistingRecordsMatchingGlobalHooksWithoutRewritingDocument(t *testing.T) {
	tests := []struct {
		name            string
		target          string
		relativePath    string
		logicalPath     string
		existingContent string
	}{
		{
			name:            "Codex",
			target:          "codex",
			relativePath:    ".codex/hooks.json",
			logicalPath:     "~/.codex/hooks.json",
			existingContent: "{\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"make test\"\n          }\n        ]\n      }\n    ]\n  },\n  \"meta\": true\n}\n",
		},
		{
			name:            "Claude Code",
			target:          "claude-code",
			relativePath:    ".claude/settings.json",
			logicalPath:     "~/.claude/settings.json",
			existingContent: "{\n  \"env\": {\n    \"KEEP\": \"yes\"\n  },\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"make test\"\n          }\n        ]\n      }\n    ]\n  }\n}\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			manifestPath := filepath.Join(root, "daem.toml")
			hostPath := filepath.Join(home, filepath.FromSlash(test.relativePath))
			testkit.WriteFile(t, home, test.relativePath, test.existingContent)

			testkit.WriteHookManifestAndLock(t, root, `
version = 1
targets = ["`+test.target+`"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["`+test.target+`"]
scope = "global"
`)

			var dryStdout bytes.Buffer
			var dryStderr bytes.Buffer
			dryExit := testkit.RunVerboseCLI(
				[]string{"apply", "--manifest", manifestPath, "--dry-run"},
				&dryStdout,
				&dryStderr,
			)
			if dryExit != 1 || !strings.Contains(dryStdout.String(), "reason=unmanaged_output_exists") {
				t.Fatalf(
					"apply without manage-existing exit=%d stdout=%q stderr=%q, want unmanaged conflict",
					dryExit,
					dryStdout.String(),
					dryStderr.String(),
				)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(
				[]string{"apply", "--manifest", manifestPath, "--manage-existing", "--yes"},
				&stdout,
				&stderr,
			)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("manage-existing exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			testkit.AssertFileContent(t, hostPath, test.existingContent)

			state, err := statefile.Load(t.Context(), filepath.Join(root, ".daem", "state.json"))
			if err != nil {
				t.Fatalf("statefile.Load returned error: %v", err)
			}
			testkit.AssertHookAggregateState(t, state, "test", test.target, "global", test.logicalPath)
			testkit.AssertNoRecoveryArtifacts(t, root)
		})
	}
}
