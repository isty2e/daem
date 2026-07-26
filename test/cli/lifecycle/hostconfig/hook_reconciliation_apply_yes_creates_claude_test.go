package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyYesCreatesClaudeCodeHookSettingsAndStatefileRecord(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	expectedSettings := "{\n  \"hooks\": {\n    \"PostToolUse\": [\n      {\n        \"matcher\": \"Edit|Write\",\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"python3 hooks/protect.py\",\n            \"if\": \"tool_name == 'Edit'\",\n            \"timeout\": 7,\n            \"statusMessage\": \"checking\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "protect-env"
event = "PostToolUse"
matcher = "Write"
command = "python3 hooks/protect.py"
timeout = 7
status_message = "checking"
targets = ["claude-code"]

[[hook.target_override]]
target = "claude-code"
if = "tool_name == 'Edit'"
matcher = "Edit|Write"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "lock-only:") {
		t.Fatalf("stdout = %q, want no lock-only summary for supported claude-code hook", stdout.String())
	}
	if !strings.Contains(stdout.String(), "applied: 1 actions") {
		t.Fatalf("stdout = %q, want one hook create action", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".claude/settings.json"), expectedSettings)
	if testkit.HashPath(t, filepath.Join(tempDir, ".claude/settings.json")) != string(artifact.HashFileContent([]byte(expectedSettings))) {
		t.Fatalf("installed settings hash does not match expected generated content")
	}

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertHookAggregateState(t, state, "protect-env", "claude-code", "project", ".claude/settings.json")
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}

func TestRunApplyYesCreatesClaudeCodeGlobalHookSettingsUnderHome(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	expectedSettings := "{\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"make test\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "global-test"
event = "Stop"
command = "make test"
targets = ["claude-code"]
scope = "global"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".claude/settings.json"), expectedSettings)

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertHookAggregateState(t, state, "global-test", "claude-code", "global", "~/.claude/settings.json")
}

func TestRunApplyYesCreatesCodexHookFilesAndStatefileRecords(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	expectedProjectConfig := "{\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\n        \"matcher\": \"^Bash$\",\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"python3 hooks/protect.py\",\n            \"timeout\": 30,\n            \"statusMessage\": \"Checking Bash command\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"
	expectedGlobalConfig := "{\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"make test\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["codex"]

[[hook]]
name = "protect-bash"
event = "PreToolUse"
matcher = "^Bash$"
command = "python3 hooks/protect.py"
timeout = 30
status_message = "Checking Bash command"
targets = ["codex"]

[[hook]]
name = "global-test"
event = "Stop"
command = "make test"
targets = ["codex"]
scope = "global"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "lock-only:") {
		t.Fatalf("stdout = %q, want no lock-only summary for supported codex hooks", stdout.String())
	}
	if !strings.Contains(stdout.String(), "applied: 2 actions") {
		t.Fatalf("stdout = %q, want two hook create actions", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".codex/hooks.json"), expectedProjectConfig)
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".codex/hooks.json"), expectedGlobalConfig)

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertHookAggregateState(t, state, "protect-bash", "codex", "project", ".codex/hooks.json")
	testkit.AssertHookAggregateState(t, state, "global-test", "codex", "global", "~/.codex/hooks.json")
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}

func TestRunApplyYesUpdatesAndDeletesManagedCodexHookProjections(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	oldProjectHooks := "{\n  \"Stop\": [\n    {\n      \"hooks\": [\n        {\n          \"type\": \"command\",\n          \"command\": \"make old-project\"\n        }\n      ]\n    }\n  ]\n}\n"
	oldGlobalHooks := "{\n  \"Stop\": [\n    {\n      \"hooks\": [\n        {\n          \"type\": \"command\",\n          \"command\": \"make old-global\"\n        }\n      ]\n    }\n  ]\n}\n"
	testkit.WriteFile(t, tempDir, ".codex/hooks.json", "{\n  \"hooks\": "+strings.TrimSpace(oldProjectHooks)+",\n  \"meta\": {\n    \"keep\": true\n  }\n}\n")
	testkit.WriteFile(t, homeDir, ".codex/hooks.json", "{\n  \"hooks\": "+strings.TrimSpace(oldGlobalHooks)+",\n  \"meta\": {\n    \"keep\": true\n  }\n}\n")
	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["codex"]

[[hook]]
name = "old-project"
event = "Stop"
command = "make old-project"
targets = ["codex"]

[[hook]]
name = "old-global"
event = "Stop"
command = "make old-global"
targets = ["codex"]
scope = "global"
`)
	testkit.WriteHookAggregateStateFromLock(t, tempDir)
	testkit.WriteActiveOwnershipClaim(t, manifestPath, "~/.codex/hooks.json", "/hooks")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["codex"]

[[hook]]
name = "format"
event = "PostToolUse"
matcher = "Write"
command = "make fmt"
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"applied: 3 actions",
		`create resource="hook/format" target=codex scope=project`,
		`delete resource="hook/old-project" target=codex scope=project`,
		`delete resource="hook/old-global" target=codex scope=global`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want subject-level transition %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".codex/hooks.json"), "{\n  \"hooks\": {\n    \"PostToolUse\": [\n      {\n        \"matcher\": \"Write\",\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"make fmt\"\n          }\n        ]\n      }\n    ]\n  },\n  \"meta\": {\n    \"keep\": true\n  }\n}\n")
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".codex/hooks.json"), "{\n  \"meta\": {\n    \"keep\": true\n  }\n}\n")

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertHookAggregateState(t, state, "format", "codex", "project", ".codex/hooks.json")
	testkit.AssertHookAggregateStateMissing(t, state, "old-project", "codex", "project")
	testkit.AssertHookAggregateStateMissing(t, state, "old-global", "codex", "global")
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}

func TestRunApplyManageExistingRecordsMatchingCodexHooksWithoutOwningOtherKeys(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	hooks := "{\n  \"Stop\": [\n    {\n      \"hooks\": [\n        {\n          \"type\": \"command\",\n          \"command\": \"make test\"\n        }\n      ]\n    }\n  ]\n}\n"
	existingConfig := "{\n  \"hooks\": " + strings.TrimSpace(hooks) + ",\n  \"meta\": {\n    \"keep\": true\n  }\n}\n"
	testkit.WriteFile(t, tempDir, ".codex/hooks.json", existingConfig)

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["codex"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["codex"]
`)

	var dryStdout bytes.Buffer
	var dryStderr bytes.Buffer
	dryExit := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &dryStdout, &dryStderr)
	if dryExit != 1 {
		t.Fatalf("dryExit = %d, want unmanaged conflict; stdout = %q stderr = %q", dryExit, dryStdout.String(), dryStderr.String())
	}
	if !strings.Contains(dryStdout.String(), "reason=unmanaged_output_exists") {
		t.Fatalf("stdout = %q stderr = %q, want unmanaged hook conflict", dryStdout.String(), dryStderr.String())
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--manage-existing", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".codex/hooks.json"), existingConfig)

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertHookAggregateState(t, state, "test", "codex", "project", ".codex/hooks.json")
}

func TestRunStatusRejectsSameScopeCodexInlineHooksConflict(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, ".codex/config.toml", "[hooks]\n")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["codex"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["codex"]
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
	if !strings.Contains(stderr.String(), `unmanaged Codex inline hooks found in ".codex/config.toml"`) {
		t.Fatalf("stderr = %q, want inline hook conflict", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".codex/hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("hooks.json exists or stat failed: %v", err)
	}
}
