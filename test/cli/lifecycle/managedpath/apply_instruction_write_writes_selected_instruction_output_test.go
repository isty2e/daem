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

func TestRunApplyWritesSelectedInstructionOutputAndStatefile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "shared instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex", "claude-code"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash, Targets: []target.Target{target.TargetCodex, target.TargetClaudeCode}}))
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--target", "codex", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"applied: 1 actions",
		"statefile:",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if strings.Contains(stdout.String(), "snapshot:") {
		t.Fatalf("stdout = %q, did not want snapshot output", stdout.String())
	}

	appliedContent, err := os.ReadFile(filepath.Join(tempDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("ReadFile applied output returned error: %v", err)
	}
	if string(appliedContent) != "shared instructions\n" {
		t.Fatalf("applied content = %q", appliedContent)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("non-selected target output was written or stat failed: %v", err)
	}
}

func TestRunApplyWritesCodexAndClaudeInstructionOutputs(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/shared.md", "shared instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/shared.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "instructions/shared.md"
targets = ["codex", "claude-code"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/shared.md?mode=vendor", ContentHash: instructionHash, Targets: []target.Target{target.TargetCodex, target.TargetClaudeCode}}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "applied: 2 actions") {
		t.Fatalf("stdout = %q, want two applied actions", stdout.String())
	}

	for _, path := range []string{"AGENTS.md", "CLAUDE.md"} {
		content, err := os.ReadFile(filepath.Join(tempDir, path))
		if err != nil {
			t.Fatalf("ReadFile %s returned error: %v", path, err)
		}
		if string(content) != "shared instructions\n" {
			t.Fatalf("%s content = %q", path, content)
		}
	}

	state, err := statefile.Load(t.Context(), filepath.Join(tempDir, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResource(t, state, "codex", "AGENTS.md", instructionHash)
	testkit.AssertStateResource(t, state, "claude-code", "CLAUDE.md", instructionHash)
}

func TestRunApplyUpdatesManagedInstructionOutputAndCapturesBackup(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "new instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "old instructions\n")
	newHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
	oldHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: newHash}))
	testkit.WriteStatefile(t, filepath.Join(tempDir, ".daem", "state.json"), testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", oldHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	content, err := os.ReadFile(filepath.Join(tempDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "new instructions\n" {
		t.Fatalf("updated content = %q", content)
	}

	state, err := statefile.Load(t.Context(), filepath.Join(tempDir, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResource(t, state, "codex", "AGENTS.md", newHash)
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}

func TestRunApplyRejectsSymlinkInstructionModeBeforeMutation(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/CLAUDE.md", "claude instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/CLAUDE.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["claude-code"]

[instructions.project]
source = "instructions/CLAUDE.md"
targets = ["claude-code"]

[instructions.project.target.claude-code]
mode = "symlink"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/CLAUDE.md?mode=vendor", ContentHash: instructionHash, Targets: []target.Target{target.TargetClaudeCode}, InstallMode: "symlink"}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "symlink") {
		t.Fatalf("stderr = %q, want symlink diagnostic", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("symlink apply wrote output or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf("symlink apply created .daem or stat failed: %v", err)
	}
}
