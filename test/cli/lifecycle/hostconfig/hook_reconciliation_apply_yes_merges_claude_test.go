package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyYesMergesClaudeCodeHookSettingsIntoExistingSettings(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, ".claude/settings.json", `{"env":{"KEEP":"yes"},"permissions":{"allow":["Bash(make test)"]}}`+"\n")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["claude-code"]
`)
	expectedSettings := "{\n  \"env\": {\n    \"KEEP\": \"yes\"\n  },\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"make test\"\n          }\n        ]\n      }\n    ]\n  },\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(make test)\"\n    ]\n  }\n}\n"

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".claude/settings.json"), expectedSettings)

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertHookAggregateState(t, state, "test", "claude-code", "project", ".claude/settings.json")
}

func TestRunApplyYesRepairsExecutableClaudeCodeHookSettingsMode(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	settingsPath := filepath.Join(tempDir, ".claude/settings.json")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["claude-code"]
`)
	runHookApplyYes(t, manifestPath)
	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("ReadFile settings returned error: %v", err)
	}
	if err := os.Chmod(settingsPath, 0o700); err != nil {
		t.Fatalf("Chmod settings to 0700 returned error: %v", err)
	}

	runHookApplyYes(t, manifestPath)
	testkit.AssertFileContent(t, settingsPath, string(before))
	assertFileMode(t, settingsPath, 0o600)
}

func TestRunStatusIgnoresUnrelatedClaudeCodeSettingsDrift(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	hooks := "{\n  \"Stop\": [\n    {\n      \"hooks\": [\n        {\n          \"type\": \"command\",\n          \"command\": \"make test\"\n        }\n      ]\n    }\n  ]\n}\n"
	testkit.WriteFile(t, tempDir, ".claude/settings.json", "{\n  \"env\": {\n    \"CHANGED\": \"yes\"\n  },\n  \"hooks\": "+strings.TrimSpace(hooks)+",\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(make test)\"\n    ]\n  }\n}\n")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["claude-code"]
`)
	testkit.WriteHookAggregateStateFromLock(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "drifted_output") {
		t.Fatalf("stdout = %q, want unrelated top-level settings ignored", stdout.String())
	}
	if !strings.Contains(stdout.String(), "reason=already_current") {
		t.Fatalf("stdout = %q, want hook aggregate already current", stdout.String())
	}
}

func TestRunStatusBlocksManagedClaudeCodeHookDrift(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, ".claude/settings.json", "{\n  \"env\": {\n    \"KEEP\": \"yes\"\n  },\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"make hacked\"\n          }\n        ]\n      }\n    ]\n  }\n}\n")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["claude-code"]
`)
	testkit.WriteHookAggregateStateFromLock(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "reason=drifted_output") {
		t.Fatalf("stdout = %q, want managed hook drift blocked", stdout.String())
	}
}

func TestRunApplyManageExistingRecordsMatchingClaudeCodeHooksWithoutOwningOtherSettings(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	existingSettings := "{\n  \"env\": {\n    \"KEEP\": \"yes\"\n  },\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"make test\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"
	testkit.WriteFile(t, tempDir, ".claude/settings.json", existingSettings)

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["claude-code"]
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
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".claude/settings.json"), existingSettings)

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertHookAggregateState(t, state, "test", "claude-code", "project", ".claude/settings.json")
}

func TestRunApplyYesDeletesOnlyManagedClaudeCodeHooksFromSharedSettings(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	hooks := "{\n  \"Stop\": [\n    {\n      \"hooks\": [\n        {\n          \"type\": \"command\",\n          \"command\": \"make test\"\n        }\n      ]\n    }\n  ]\n}\n"
	testkit.WriteFile(t, tempDir, ".claude/settings.json", "{\n  \"env\": {\n    \"KEEP\": \"yes\"\n  },\n  \"hooks\": "+strings.TrimSpace(hooks)+"\n}\n")
	testkit.WriteFile(t, tempDir, "instructions/keep.md", "keep selected target\n")
	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["claude-code"]
`)
	testkit.WriteHookAggregateStateFromLock(t, tempDir)
	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[instructions.keep]
source = "instructions/keep.md"
targets = ["claude-code"]

[instructions.keep.target.claude-code]
render_to = "CLAUDE.md"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".claude/settings.json"), "{\n  \"env\": {\n    \"KEEP\": \"yes\"\n  }\n}\n")

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertHookAggregateStateMissing(t, state, "test", "claude-code", "project")
}

func TestRunApplyYesRemovesSharedClaudeHooksOneSubjectAtATime(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, ".claude/settings.json", "{\n  \"env\": {\n    \"KEEP\": \"yes\"\n  }\n}\n")
	testkit.WriteFile(t, tempDir, "instructions/keep.md", "keep selected target\n")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "alpha"
event = "Stop"
command = "make alpha"
targets = ["claude-code"]

[[hook]]
name = "beta"
event = "Stop"
command = "make beta"
targets = ["claude-code"]
`)
	runHookApplyYes(t, manifestPath)

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "beta"
event = "Stop"
command = "make beta"
targets = ["claude-code"]
`)
	partialOutput := runHookApplyYes(t, manifestPath)
	for _, want := range []string{
		"applied: 1 actions",
		`delete resource="hook/alpha" target=claude-code scope=project`,
	} {
		if !strings.Contains(partialOutput, want) {
			t.Fatalf("partial apply output = %q, want %q", partialOutput, want)
		}
	}
	for _, unwanted := range []string{
		`create resource="hook/beta"`,
		`update resource="hook/beta"`,
		`delete resource="hook/beta"`,
	} {
		if strings.Contains(partialOutput, unwanted) {
			t.Fatalf("partial apply output = %q, unchanged beta reported as mutation %q", partialOutput, unwanted)
		}
	}

	partialBytes, err := os.ReadFile(filepath.Join(tempDir, ".claude/settings.json"))
	if err != nil {
		t.Fatalf("ReadFile after partial removal returned error: %v", err)
	}
	partial := string(partialBytes)
	if strings.Contains(partial, "make alpha") || !strings.Contains(partial, "make beta") {
		t.Fatalf("settings after partial removal = %q, want only beta contribution", partial)
	}
	partialState, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load after partial removal returned error: %v", err)
	}
	testkit.AssertHookAggregateStateMissing(t, partialState, "alpha", "claude-code", "project")
	testkit.AssertHookAggregateState(t, partialState, "beta", "claude-code", "project", ".claude/settings.json")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[instructions.keep]
source = "instructions/keep.md"
targets = ["claude-code"]

[instructions.keep.target.claude-code]
render_to = "CLAUDE.md"
`)
	runHookApplyYes(t, manifestPath)

	testkit.AssertFileContent(t, filepath.Join(tempDir, ".claude/settings.json"), "{\n  \"env\": {\n    \"KEEP\": \"yes\"\n  }\n}\n")
	finalState, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load after final removal returned error: %v", err)
	}
	testkit.AssertHookAggregateStateMissing(t, finalState, "alpha", "claude-code", "project")
	testkit.AssertHookAggregateStateMissing(t, finalState, "beta", "claude-code", "project")
}

func runHookApplyYes(t *testing.T, manifestPath string) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("apply exitCode = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func TestRunApplyRollsBackPartialClaudeCodeHookUpdateWhenLaterWriteFails(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	homeDir := filepath.Join(tempDir, "home")
	blockedDir := filepath.Join(homeDir, ".claude")
	t.Setenv("HOME", homeDir)
	oldSettings := "{\n  \"env\": {\n    \"KEEP\": \"yes\"\n  },\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"make old\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"
	testkit.WriteFile(t, tempDir, ".claude/settings.json", oldSettings)
	testkit.WriteFile(t, tempDir, "instructions/fail.md", "fail later\n")
	failSourcePath := filepath.Join(tempDir, "instructions/fail.md")
	if err := os.MkdirAll(blockedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll blockedDir returned error: %v", err)
	}
	if err := os.Chmod(blockedDir, 0o500); err != nil {
		t.Fatalf("Chmod blockedDir returned error: %v", err)
	}
	defer os.Chmod(blockedDir, 0o700)
	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "old"
event = "Stop"
command = "make old"
targets = ["claude-code"]
`)
	testkit.WriteHookAggregateStateFromLock(t, tempDir)
	testkit.WriteHookManifestAndLock(t, tempDir, fmt.Sprintf(`
version = 1
targets = ["claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make new"
targets = ["claude-code"]

[instructions.fail]
source = %q
targets = ["claude-code"]
scope = "global"

[instructions.fail.target.claude-code]
render_to = "CLAUDE.md"
`, failSourcePath))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "host changes rolled back") {
		t.Fatalf("stderr = %q, want rollback confirmation", stderr.String())
	}
	if err := os.Chmod(blockedDir, 0o700); err != nil {
		t.Fatalf("Chmod blockedDir after apply returned error: %v", err)
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".claude/settings.json"), oldSettings)
}
