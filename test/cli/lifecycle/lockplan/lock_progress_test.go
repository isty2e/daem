package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunLockWithTerminalStderrWritesProgressToStderrOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, lockfilePath, _ := testkit.WriteLockDeltaFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	assertProgressOnStderrOnly(t, stdout.String(), stderr.String())
	if !strings.Contains(stdout.String(), "wrote lockfile: "+lockfilePath) {
		t.Fatalf("stdout = %q, want final lock summary", stdout.String())
	}
}

func TestRunLockDryRunWithTerminalStderrWritesProgressToStderrOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, lockfilePath, _ := testkit.WriteLockDeltaFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	assertProgressOnStderrOnly(t, stdout.String(), stderr.String())
	if !strings.Contains(stdout.String(), "would write lockfile: "+lockfilePath) {
		t.Fatalf("stdout = %q, want dry-run summary", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created .daem or stat failed unexpectedly: %v", err)
	}
}

func TestRunLockJSONSuppressesProgressWithTerminalStderr(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := testkit.WriteLockDeltaFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"lock", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "progress:") {
		t.Fatalf("stdout = %q, did not want progress", stdout.String())
	}
	payload := clijson.DecodeLock(t, stdout.Bytes())
	if payload.Command != "lock" || payload.Mode != "dry-run" {
		t.Fatalf("payload command/mode = %s/%s", payload.Command, payload.Mode)
	}
}

func TestRunOutdatedWithTerminalStderrWritesProgressToStderrOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, lockfilePath, _ := testkit.WriteLockDeltaFixture(t, tempDir)
	beforeLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"outdated", "--manifest", manifestPath}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	assertProgressOnStderrOnly(t, stdout.String(), stderr.String())
	if !strings.Contains(stdout.String(), "outdated: lockfile can be refreshed: "+lockfilePath) {
		t.Fatalf("stdout = %q, want outdated summary", stdout.String())
	}
	afterLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(afterLockfile, beforeLockfile) {
		t.Fatalf("outdated changed lockfile bytes")
	}
}

func TestRunOutdatedCheckWithTerminalStderrPreservesExitCode(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, lockfilePath, _ := testkit.WriteLockDeltaFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"outdated", "--manifest", manifestPath, "--check"}, &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q", exitCode, stderr.String())
	}
	assertProgressOnStderrOnly(t, stdout.String(), stderr.String())
	if !strings.Contains(stdout.String(), "outdated: lockfile can be refreshed: "+lockfilePath) {
		t.Fatalf("stdout = %q, want outdated summary", stdout.String())
	}
}

func TestRunOutdatedJSONSuppressesProgressWithTerminalStderr(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := testkit.WriteLockDeltaFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"outdated", "--manifest", manifestPath, "--check", "--json"}, &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "progress:") {
		t.Fatalf("stdout = %q, did not want progress", stdout.String())
	}
	payload := clijson.DecodeLock(t, stdout.Bytes())
	if payload.Command != "outdated" || payload.Mode != "check" {
		t.Fatalf("payload command/mode = %s/%s", payload.Command, payload.Mode)
	}
	if !payload.HasChanges {
		t.Fatalf("payload HasChanges = false, want true")
	}
}

func TestRunCLIDefaultSuppressesProgressForLock(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := testkit.WriteLockDeltaFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "progress:") || strings.Contains(stderr.String(), "progress:") {
		t.Fatalf("stdout = %q, stderr = %q, did not want progress", stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunWithOptionsNonTerminalStderrSuppressesProgressForLock(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := testkit.WriteLockDeltaFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLIWithOptions([]string{"lock", "--manifest", manifestPath, "--dry-run"}, cli.RunOptions{
		Stdout:           &stdout,
		Stderr:           &stderr,
		StderrIsTerminal: false,
	})

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "progress:") || strings.Contains(stderr.String(), "progress:") {
		t.Fatalf("stdout = %q, stderr = %q, did not want progress", stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunLockFailureWithTerminalStderrKeepsDiagnostics(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "missing"
source = { path = "skills/missing", mode = "vendor" }
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q", exitCode, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Resolving sources") {
		t.Fatalf("stderr = %q, want progress before failure diagnostic", stderr.String())
	}
	if !strings.Contains(stderr.String(), "lock failed:") {
		t.Fatalf("stderr = %q, want lock failure diagnostic", stderr.String())
	}
}

func runCLITerminalStderr(args []string, stdout *bytes.Buffer, stderr *bytes.Buffer) int {
	return testkit.RunVerboseCLIWithOptions(args, cli.RunOptions{
		Stdout:           stdout,
		Stderr:           stderr,
		StderrIsTerminal: true,
	})
}

func assertProgressOnStderrOnly(t *testing.T, stdout string, stderr string) {
	t.Helper()

	if strings.Contains(stdout, "Resolving sources") {
		t.Fatalf("stdout = %q, did not want progress", stdout)
	}
	for _, want := range []string{"\r\x1b[2KResolving sources 0", "\r\x1b[2KResolving sources 3", "\r\x1b[2K"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want %q", stderr, want)
		}
	}
	for _, forbidden := range []string{"request=", "ordinal=", "stage=", "source_kind=", "progress:"} {
		if strings.Contains(stderr, forbidden) {
			t.Fatalf("stderr = %q, did not want %q", stderr, forbidden)
		}
	}
}
