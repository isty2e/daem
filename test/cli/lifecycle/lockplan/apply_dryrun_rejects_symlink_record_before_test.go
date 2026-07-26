package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyDryRunRejectsSymlinkRecordBeforePlanOutput(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"

[instructions.project.target.codex]
mode = "symlink"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash, InstallMode: "symlink"}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", "sha256:stale-state"),
	))
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `apply symlink mode for "AGENTS.md" is not implemented`) {
		t.Fatalf("stderr = %q, want symlink diagnostic", stderr.String())
	}
	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile after dry-run returned error: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("dry-run mutated statefile:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
	}
}

func TestRunApplyDryRunRejectsSymlinkUpdateBeforePlanOutput(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "new instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "old instructions\n")
	newHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
	oldHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"

[instructions.project.target.codex]
mode = "symlink"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: newHash, InstallMode: "symlink"}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", oldHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `apply symlink mode for "AGENTS.md" is not implemented`) {
		t.Fatalf("stderr = %q, want symlink diagnostic", stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "old instructions\n")
}

func TestRunApplyDryRunAppliesTargetSelection(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex", "claude-code"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "project instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash, Targets: []target.Target{target.TargetCodex, target.TargetClaudeCode}}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI(
		[]string{"apply", "--manifest", manifestPath, "--target", "claude-code", "--target", "claude-code", "--dry-run"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	output := stdout.String()
	if !strings.Contains(output, "target=claude-code") {
		t.Fatalf("stdout = %q, want claude-code action", output)
	}
	if strings.Contains(output, "target=codex") {
		t.Fatalf("stdout = %q, want codex filtered out", output)
	}
}
