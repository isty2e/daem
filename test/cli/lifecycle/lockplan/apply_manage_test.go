package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyManagesMatchingExistingInstructionOutput(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "managed instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "managed instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--manage-existing", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "applied: 1 actions") {
		t.Fatalf("stdout = %q, want one record action", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "managed instructions\n")

	state, err := statefile.Load(t.Context(), filepath.Join(tempDir, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResource(t, state, "codex", "AGENTS.md", instructionHash)

	var statusStdout bytes.Buffer
	var statusStderr bytes.Buffer
	statusExitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &statusStdout, &statusStderr)
	if statusExitCode != 0 {
		t.Fatalf("statusExitCode = %d, stderr = %q", statusExitCode, statusStderr.String())
	}
	for _, want := range []string{
		"status: 1 actions",
		`noop resource="instructions/project" target=codex`,
		"reason=already_current",
	} {
		if !strings.Contains(statusStdout.String(), want) {
			t.Fatalf("status stdout = %q, want %q", statusStdout.String(), want)
		}
	}
}

func TestRunApplyManagesMatchingExistingAntigravityGlobalInstructionOutput(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	sourcePath := filepath.Join(tempDir, "instructions", "global.md")
	testkit.WriteFile(t, tempDir, "instructions/global.md", "managed antigravity instructions\n")
	testkit.WriteFile(t, homeDir, ".gemini/GEMINI.md", "managed antigravity instructions\n")
	instructionHash := testkit.HashPath(t, sourcePath)

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["antigravity-cli"]

[instructions.global]
source = "`+filepath.ToSlash(sourcePath)+`"
scope = "global"
targets = ["antigravity-cli"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "global", SourceID: "local:" + filepath.ToSlash(sourcePath) + "?mode=vendor", ContentHash: instructionHash, Targets: []target.Target{target.TargetAntigravityCLI}, Scope: target.ScopeGlobal}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--manage-existing", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "applied: 1 actions") {
		t.Fatalf("stdout = %q, want one record action", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".gemini", "GEMINI.md"), "managed antigravity instructions\n")

	state, err := statefile.Load(t.Context(), filepath.Join(tempDir, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResourceNamed(t, state, "global", "antigravity-cli", "global", "~/.gemini/GEMINI.md", instructionHash)
}

func TestRunApplyDryRunReportsManagedExistingInstructionOutput(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "managed instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "managed instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--manage-existing", "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"dry-run: 1 actions",
		`record resource="instructions/project" target=codex`,
		"reason=managed_existing",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote statefile or stat failed: %v", err)
	}
}

func TestRunApplyManageExistingReobservesAfterDryRun(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "managed instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "managed instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	var dryStdout bytes.Buffer
	var dryStderr bytes.Buffer
	dryExitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--manage-existing", "--dry-run"}, &dryStdout, &dryStderr)
	if dryExitCode != 0 {
		t.Fatalf("dryExitCode = %d, stderr = %q", dryExitCode, dryStderr.String())
	}
	if !strings.Contains(dryStdout.String(), "reason=managed_existing") {
		t.Fatalf("dry stdout = %q, want managed_existing preview", dryStdout.String())
	}

	testkit.WriteFile(t, tempDir, "AGENTS.md", "foreign instructions\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--manage-existing", "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "unmanaged_output_exists") {
		t.Fatalf("stderr = %q, want unmanaged output diagnostic", stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "foreign instructions\n")
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("failed manage-existing wrote statefile or stat failed: %v", err)
	}
}

func TestRunApplyRejectsMatchingExistingInstructionOutputWithoutManageExistingFlag(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "managed instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "managed instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "unmanaged_output_exists") {
		t.Fatalf("stderr = %q, want unmanaged output diagnostic", stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "managed instructions\n")
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("rejected apply wrote statefile or stat failed: %v", err)
	}
}

func TestRunApplyRejectsManageExistingWhenContentDiffers(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "managed instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "foreign instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--manage-existing", "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "unmanaged_output_exists") {
		t.Fatalf("stderr = %q, want unmanaged output diagnostic", stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "foreign instructions\n")
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("failed manage-existing wrote statefile or stat failed: %v", err)
	}
}
