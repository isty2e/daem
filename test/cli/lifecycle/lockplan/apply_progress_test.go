package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunApplyYesWithTerminalStderrWritesProgressToStderrOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := testkit.WriteInstructionApplyFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	assertApplyProgressOnStderrOnly(t, stdout.String(), stderr.String())
	if !strings.Contains(stdout.String(), "applied: 1 actions") {
		t.Fatalf("stdout = %q, want final apply summary", stdout.String())
	}
	if !strings.Contains(stdout.String(), "statefile: "+filepath.Join(tempDir, ".daem", "state.json")) {
		t.Fatalf("stdout = %q, want statefile summary", stdout.String())
	}
}

func TestRunApplyYesJSONSuppressesProgressWithTerminalStderr(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := testkit.WriteInstructionApplyFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"apply", "--manifest", manifestPath, "--yes", "--json"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "progress:") {
		t.Fatalf("stdout = %q, did not want progress", stdout.String())
	}
	payload := clijson.DecodeApplyResult(t, stdout.Bytes())
	if payload.Command != "apply" || payload.Mode != "write" || payload.ActionCount != 1 {
		t.Fatalf("payload = %#v, want apply write with one action", payload)
	}
}

func TestRunApplyDryRunJSONSuppressesProgressWithTerminalStderr(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := testkit.WriteInstructionApplyFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "progress:") {
		t.Fatalf("stdout = %q, did not want progress", stdout.String())
	}
	payload := clijson.DecodePlan(t, stdout.Bytes())
	if payload.Command != "apply" || payload.Mode != "dry-run" || payload.ActionCount != 1 {
		t.Fatalf("payload = %#v, want dry-run plan with one action", payload)
	}
}

func TestRunApplyYesWithTerminalStderrEmitsNoProgressForZeroActions(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, instructionHash := testkit.WriteInstructionApplyFixture(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "managed instructions\n")
	testkit.WriteStatefile(t, filepath.Join(tempDir, ".daem", "state.json"), testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", instructionHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "progress:") || strings.Contains(stderr.String(), "progress:") {
		t.Fatalf("stdout = %q, stderr = %q, did not want progress for zero actions", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "applied: 0 actions") {
		t.Fatalf("stdout = %q, want zero-action summary", stdout.String())
	}
}

func TestRunApplyFailureWithTerminalStderrKeepsDiagnosticsAndRollbackProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions are required to force a host write failure after journal capture")
	}
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	homeDir := filepath.Join(tempDir, "home")
	blockedDir := filepath.Join(homeDir, ".claude")
	t.Setenv("HOME", homeDir)
	testkit.WriteFile(t, tempDir, "instructions/alpha.md", "alpha instructions\n")
	testkit.WriteFile(t, tempDir, "instructions/beta.md", "beta instructions\n")
	betaSourcePath := filepath.Join(tempDir, "instructions/beta.md")
	if err := os.MkdirAll(blockedDir, 0o700); err != nil {
		t.Fatalf("MkdirAll blockedDir returned error: %v", err)
	}
	if err := os.Chmod(blockedDir, 0o500); err != nil {
		t.Fatalf("Chmod blockedDir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(blockedDir, 0o700)
	})
	alphaHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/alpha.md"))
	betaHash := testkit.HashPath(t, betaSourcePath)
	testkit.WriteFile(t, tempDir, "daem.toml", fmt.Sprintf(`
version = 1
targets = ["codex", "claude-code"]

[instructions.alpha]
source = "instructions/alpha.md"
targets = ["codex"]

[instructions.alpha.target.codex]
render_to = "AGENTS.md"

[instructions.beta]
source = %q
targets = ["claude-code"]
scope = "global"

[instructions.beta.target.claude-code]
render_to = "CLAUDE.md"
`, betaSourcePath))
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(
		t,
		testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "alpha", SourceID: "local:instructions/alpha.md?mode=vendor", ContentHash: alphaHash},
		testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "beta", SourceID: "local:" + filepath.ToSlash(betaSourcePath) + "?mode=vendor", ContentHash: betaHash, Targets: []target.Target{target.TargetClaudeCode}, Scope: target.ScopeGlobal},
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLITerminalStderr([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		"Applying 0/2",
		": failed",
		"apply failed:",
		"host changes rolled back",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if !strings.Contains(stderr.String(), "\r\x1b[2Kapply failed:") {
		t.Fatalf("stderr = %q, want cleared progress before failure", stderr.String())
	}
	if strings.Contains(stdout.String(), "progress:") {
		t.Fatalf("stdout = %q, did not want progress", stdout.String())
	}
	if err := os.Chmod(blockedDir, 0o700); err != nil {
		t.Fatalf("Chmod blockedDir after apply returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("AGENTS.md was not rolled back: %v", err)
	}
}

func TestRunApplyDefaultSuppressesProgress(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _, _ := testkit.WriteInstructionApplyFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "progress:") || strings.Contains(stderr.String(), "progress:") {
		t.Fatalf("stdout = %q, stderr = %q, did not want progress", stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func assertApplyProgressOnStderrOnly(t *testing.T, stdout string, stderr string) {
	t.Helper()

	if strings.Contains(stdout, "Applying ") {
		t.Fatalf("stdout = %q, did not want progress", stdout)
	}
	for _, want := range []string{
		"\r\x1b[2KApplying 0/1",
		"\r\x1b[2KApplying 1/1",
		"\r\x1b[2K",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want %q", stderr, want)
		}
	}
	for _, forbidden := range []string{"journal_capture", "action_started", "statefile_written", "request=", "stage="} {
		if strings.Contains(stderr, forbidden) {
			t.Fatalf("stderr = %q, did not want %q", stderr, forbidden)
		}
	}
}
