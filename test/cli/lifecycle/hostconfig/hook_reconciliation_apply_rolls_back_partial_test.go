package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyRollsBackPartialClaudeCodeHookCreateIntoSharedSettingsWhenLaterWriteFails(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	homeDir := filepath.Join(tempDir, "home")
	blockedDir := filepath.Join(homeDir, ".claude")
	t.Setenv("HOME", homeDir)
	originalSettings := "{\n  \"env\": {\n    \"KEEP\": \"yes\"\n  }\n}\n"
	testkit.WriteFile(t, tempDir, ".claude/settings.json", originalSettings)
	testkit.WriteFile(t, tempDir, "instructions/fail.md", "fail later\n")
	failSourcePath := filepath.Join(tempDir, "instructions/fail.md")
	if err := os.MkdirAll(blockedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll blockedDir returned error: %v", err)
	}
	if err := os.Chmod(blockedDir, 0o500); err != nil {
		t.Fatalf("Chmod blockedDir returned error: %v", err)
	}
	defer os.Chmod(blockedDir, 0o700)
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
	if !strings.Contains(stderr.String(), "host changes were rolled back") {
		t.Fatalf("stderr = %q, want rollback confirmation", stderr.String())
	}
	if err := os.Chmod(blockedDir, 0o700); err != nil {
		t.Fatalf("Chmod blockedDir after apply returned error: %v", err)
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".claude/settings.json"), originalSettings)
}

func TestRunApplyDryRunJSONIncludesClaudeCodeHookContentPath(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["claude-code"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"content_path": "/hooks"`) {
		t.Fatalf("stdout = %q, want hook content_path in JSON", stdout.String())
	}
}

func TestRunApplyDryRunJSONIncludesCodexHookAggregate(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

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
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		`"resource_id": "hook/test"`,
		`"target": "codex"`,
		`"destination": ".codex/hooks.json"`,
		`"content_path": "/hooks"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunStatusRejectsClaudeCodeNullHooksAsUnsupportedSelectedShape(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	testkit.WriteFile(t, tempDir, ".claude/settings.json", "{\n  \"env\": {\n    \"KEEP\": \"yes\"\n  },\n  \"hooks\": null\n}\n")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["claude-code"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--check"}, &stdout, &stderr)
	if exitCode != 1 || stderr.Len() != 0 {
		t.Fatalf("exitCode = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "aggregate codec selected_shape_unsupported at /hooks") {
		t.Fatalf("stdout = %q, want redaction-safe unsupported-shape diagnostic", stdout.String())
	}
}

func TestRunApplyRejectsMalformedExistingClaudeCodeSettingsBeforeMutation(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	testkit.WriteFile(t, tempDir, ".claude/settings.json", "{\"env\":")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
targets = ["claude-code"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "aggregate codec document_malformed at /hooks") {
		t.Fatalf("stderr = %q, want malformed hook config diagnostic", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("malformed settings apply wrote statefile or stat failed: %v", err)
	}
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}

func TestRunStatusReportsUnmanagedClaudeCodeSettingsConflict(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	testkit.WriteFile(t, tempDir, ".claude/settings.json", "{\"hooks\":{}}\n")

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
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"status: 1 actions",
		`error resource="hook/protect-env" target=claude-code scope=project destination=".claude/settings.json" content_path="/hooks" reason=unmanaged_output_exists`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunApplyDryRunAggregatesMultipleClaudeCodeHooksIntoOneSettingsAction(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "format"
event = "PostToolUse"
matcher = "Edit"
command = "make fmt"
targets = ["claude-code"]

[[hook]]
name = "test"
event = "Stop"
command = "make test"
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
	if strings.Contains(stdout.String(), "destination_conflict") {
		t.Fatalf("stdout = %q, want aggregate action without destination conflict", stdout.String())
	}
	for _, resource := range []string{"hook/format", "hook/test"} {
		if count := strings.Count(stdout.String(), `create resource="`+resource+`"`); count != 1 {
			t.Fatalf("create contribution %q count = %d, stdout = %q", resource, count, stdout.String())
		}
	}
	if !strings.Contains(stdout.String(), "dry-run: 2 actions") {
		t.Fatalf("stdout = %q, want two subject decisions over one aggregate projection", stdout.String())
	}
}

func TestRunApplyDryRunReportsMixedSupportedAndLockOnlyHookTargets(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code", "pi"]

[[hook]]
name = "mixed"
event = "Stop"
command = "make test"
targets = ["claude-code", "pi"]
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
		"lock-only: skills=0 hooks=1 (unsupported or not reconciled by apply/status)",
		"dry-run: 1 actions",
		`create resource="hook/mixed" target=claude-code scope=project destination=".claude/settings.json" content_path="/hooks" reason=missing_output`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}
