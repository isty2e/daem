package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyRejectsDriftedManagedUpdateBeforeRecoveryJournal(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "desired instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "managed baseline\n")
	desiredHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
	baselineHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))
	testkit.WriteFile(t, tempDir, "AGENTS.md", "local edits\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: desiredHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", baselineHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "drifted_output") {
		t.Fatalf("stderr = %q, want drifted output diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "local edits\n")
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "snapshots")); !os.IsNotExist(err) {
		t.Fatalf("drifted output created snapshots or stat failed: %v", err)
	}
}

func TestRunApplyRejectsEscapingStatefileDestination(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	outsidePath := filepath.Join(tempDir, "outside.md")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.current]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "current", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))
	escaping := testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", "sha256:old")
	testkit.WriteStatefileWithInvalidManagedPathDestination(
		t,
		filepath.Join(tempDir, ".daem", "state.json"),
		testkit.Snapshot(t, escaping),
		"../outside.md",
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "relative path must be canonical and stay inside its selected root") {
		t.Fatalf("stderr = %q, want canonical destination diagnostic", stderr.String())
	}
	if _, err := os.Stat(outsidePath); !os.IsNotExist(err) {
		t.Fatalf("escaping destination was touched or stat failed: %v", err)
	}
}

func TestRunApplyDryRunRequiresInstructionLockEntry(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), `error resource="instructions/project" target=codex scope=project destination="AGENTS.md" mode=copy reason=missing_lock`) {
		t.Fatalf("stdout = %q, want missing lock action", stdout.String())
	}
}
