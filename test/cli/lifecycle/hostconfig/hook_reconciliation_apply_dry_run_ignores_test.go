package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyDryRunPlansLockedHookContribution(t *testing.T) {
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
	if !strings.Contains(stdout.String(), `reason=missing_output`) {
		t.Fatalf("stdout = %q, want missing_output action", stdout.String())
	}
	if !strings.Contains(stdout.String(), `resource="hook/protect-env"`) {
		t.Fatalf("stdout = %q, want canonical Hook subject action", stdout.String())
	}
}

func TestRunStatusRejectsDirectoryAtManagedHookSettingsDestination(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	if err := os.MkdirAll(filepath.Join(tempDir, ".claude/settings.json"), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	testkit.WriteHookManifestAndLock(t, tempDir, `
version = 1
targets = ["claude-code"]

[[hook]]
name = "protect-env"
event = "PostToolUse"
command = "python3 hooks/protect.py"
targets = ["claude-code"]
`)
	testkit.WriteHookAggregateStateFromLock(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `aggregate destination is not a regular file`) {
		t.Fatalf("stderr = %q, want expected regular file diagnostic", stderr.String())
	}
}
