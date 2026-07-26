package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunStatusRejectsGlobalCodexInlineHooksConflict(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")

	testkit.WriteFile(t, homeDir, ".codex/config.toml", "[hooks]\n")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["codex"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["codex"]
scope = "global"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unmanaged Codex inline hooks found in "~/.codex/config.toml"`) {
		t.Fatalf("stderr = %q, want global inline hook conflict", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".codex/hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("hooks.json exists or stat failed: %v", err)
	}
}

func TestRunApplyDryRunFiltersCodexHookTargetSelection(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["codex", "claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["codex", "claude-code"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--target", "codex", "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `create resource="hook/test" target=codex`) {
		t.Fatalf("stdout = %q, want codex hook create action", stdout.String())
	}
	if strings.Contains(stdout.String(), "claude-code") {
		t.Fatalf("stdout = %q, want unselected claude-code hook omitted", stdout.String())
	}
}

func TestRunApplyDryRunPlansLockedCodexHookContribution(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["codex"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
matcher = "Bash"
command = "python3 hooks/protect.py"
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"dry-run: 1 actions",
		`create resource="hook/protect-env" target=codex`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunLockRejectsCodexUnsupportedHookShapes(t *testing.T) {
	scenarios := []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name: "target override if",
			manifest: `
version = 1
targets = ["codex"]

[[hook]]
name = "conditional"
event = "PreToolUse"
matcher = "Bash"
command = "make test"
targets = ["codex"]

[[hook.target_override]]
target = "codex"
if = "tool_name == 'Bash'"
`,
			want: `hook "conditional" target "codex": target_override.if is not supported`,
		},
		{
			name: "stop matcher",
			manifest: `
version = 1
targets = ["codex"]

[[hook]]
name = "stop-filter"
event = "Stop"
matcher = "Bash"
command = "make test"
targets = ["codex"]
`,
			want: `hook "stop-filter" target "codex": matcher is not supported for event "Stop"`,
		},
		{
			name: "unknown event",
			manifest: `
version = 1
targets = ["codex"]

[[hook]]
name = "unknown"
event = "pre_apply"
command = "make test"
targets = ["codex"]
`,
			want: `hook "unknown" target "codex": unsupported event "pre_apply"`,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			testkit.WriteFile(t, tempDir, "daem.toml", scenario.manifest)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), scenario.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), scenario.want)
			}
		})
	}
}

func TestRunApplyDryRunUsesCurrentHookLock(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "protect-env"
event = "PostToolUse"
command = "python3 hooks/protect.py"
targets = ["claude-code"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"dry-run: 1 actions",
		`create resource="hook/protect-env" target=claude-code scope=project destination=".claude/settings.json" content_path="/hooks" reason=missing_output`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunApplyYesUpdatesAndDeletesManagedClaudeCodeHookSettings(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")

	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	oldProjectSettings := "{\"hooks\":{\"Stop\":[{\"hooks\":[{\"type\":\"command\",\"command\":\"make old-project\"}]}]}}\n"
	oldGlobalSettings := "{\"hooks\":{\"Stop\":[{\"hooks\":[{\"type\":\"command\",\"command\":\"make old-global\"}]}]}}\n"
	newProjectSettings := "{\n  \"hooks\": {\n    \"PostToolUse\": [\n      {\n        \"matcher\": \"Write\",\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"make fmt\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"
	testkit.WriteFile(t, tempDir, ".claude/settings.json", oldProjectSettings)
	testkit.WriteFile(t, homeDir, ".claude/settings.json", oldGlobalSettings)
	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "old-project"
event = "Stop"
command = "make old-project"
targets = ["claude-code"]

[[hook]]
name = "old-global"
event = "Stop"
command = "make old-global"
targets = ["claude-code"]
scope = "global"
`)
	testkit.WriteHookAggregateStateFromLock(t, tempDir)
	testkit.WriteActiveOwnershipClaim(t, manifestPath, "~/.claude/settings.json", "/hooks")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "format"
event = "PostToolUse"
matcher = "Write"
command = "make fmt"
targets = ["claude-code"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"applied: 3 actions",
		`create resource="hook/format" target=claude-code scope=project`,
		`delete resource="hook/old-project" target=claude-code scope=project`,
		`delete resource="hook/old-global" target=claude-code scope=global`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want subject-level transition %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".claude/settings.json"), newProjectSettings)
	if _, err := os.Stat(filepath.Join(homeDir, ".claude/settings.json")); !os.IsNotExist(err) {
		t.Fatalf("removed global hook settings exists or stat failed: %v", err)
	}

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertHookAggregateState(t, state, "format", "claude-code", "project", ".claude/settings.json")
	testkit.AssertHookAggregateStateMissing(t, state, "old-project", "claude-code", "project")
	testkit.AssertHookAggregateStateMissing(t, state, "old-global", "claude-code", "global")
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}
