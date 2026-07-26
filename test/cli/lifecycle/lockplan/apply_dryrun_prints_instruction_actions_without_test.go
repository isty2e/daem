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

func TestRunApplyDryRunPrintsInstructionActionsWithoutWritingHostState(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex", "claude-code"]

[instructions.project.target.claude-code]
render_to = "CLAUDE.md"
mode = "copy"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "project instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	testkit.WriteLockfile(t, filepath.Join(tempDir, "daem.lock.toml"), testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash, Targets: []target.Target{target.TargetCodex, target.TargetClaudeCode}}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"dry-run: 2 actions",
		`create resource="instructions/project" target=codex scope=project destination="AGENTS.md" mode=copy reason=missing_output`,
		`create resource="instructions/project" target=claude-code scope=project destination="CLAUDE.md" mode=copy reason=missing_output`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}

	for _, path := range []string{
		filepath.Join(tempDir, "AGENTS.md"),
		filepath.Join(tempDir, "CLAUDE.md"),
		filepath.Join(tempDir, ".daem"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("dry-run wrote %q or stat failed unexpectedly: %v", path, err)
		}
	}
}

func TestRunApplyDryRunRejectsSymlinkInstructionModeBeforePlanOutput(t *testing.T) {
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

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `apply symlink mode for "CLAUDE.md" is not implemented`) {
		t.Fatalf("stderr = %q, want symlink diagnostic", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote output or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created .daem or stat failed: %v", err)
	}
}
