package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyDryRunReportsDriftedRemovedManagedOutputWithoutWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")

	testkit.WriteFile(t, tempDir, "instructions/active.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "edited managed\n")
	activeHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/active.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.active]
source = "instructions/active.md"

[instructions.active.target.codex]
render_to = "AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "active", SourceID: "local:instructions/active.md?mode=vendor", ContentHash: activeHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "active", []string{"codex"}, "project", "AGENTS.md", activeHash),
		testkit.InstructionPathState(t, "drifted", []string{"claude-code"}, "project", "CLAUDE.md", "sha256:state-baseline"),
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
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	want := `error resource="instructions/drifted" target=claude-code scope=project destination="CLAUDE.md" mode= reason=drifted_output detail="managed output content differs from statefile baseline" safety=drift_blocked`
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}

	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile after dry-run returned error: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("dry-run mutated statefile:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "active managed\n")
	testkit.AssertFileContent(t, filepath.Join(tempDir, "CLAUDE.md"), "edited managed\n")
}

func TestRunApplyDryRunReportsMissingRemovedManagedOutputWithoutWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")

	testkit.WriteFile(t, tempDir, "instructions/active.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "active managed\n")
	activeHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/active.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.active]
source = "instructions/active.md"

[instructions.active.target.codex]
render_to = "AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "active", SourceID: "local:instructions/active.md?mode=vendor", ContentHash: activeHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "active", []string{"codex"}, "project", "AGENTS.md", activeHash),
		testkit.InstructionPathState(t, "missing", []string{"claude-code"}, "project", "CLAUDE.md", "sha256:state-baseline"),
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
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	want := `error resource="instructions/missing" target=claude-code scope=project destination="CLAUDE.md" mode= reason=missing_output detail="managed output is missing" safety=missing_evidence`
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}

	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile after dry-run returned error: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("dry-run mutated statefile:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "active managed\n")
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "snapshots")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created snapshots or stat failed: %v", err)
	}
}

func TestRunApplyYesRejectsDriftedRemovedManagedOutputBeforeWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")

	testkit.WriteFile(t, tempDir, "instructions/active.md", "active new\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "active old\n")
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "edited managed\n")
	activeHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/active.md"))
	activeStateHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.active]
source = "instructions/active.md"

[instructions.active.target.codex]
render_to = "AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "active", SourceID: "local:instructions/active.md?mode=vendor", ContentHash: activeHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "active", []string{"codex"}, "project", "AGENTS.md", activeStateHash),
		testkit.InstructionPathState(t, "drifted", []string{"claude-code"}, "project", "CLAUDE.md", "sha256:state-baseline"),
	))
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		`apply failed: apply was refused before effects`,
		"drifted_output",
		"managed output content differs from statefile baseline",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}

	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile after apply returned error: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("apply mutated statefile:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "active old\n")
	testkit.AssertFileContent(t, filepath.Join(tempDir, "CLAUDE.md"), "edited managed\n")
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "snapshots")); !os.IsNotExist(err) {
		t.Fatalf("apply created snapshots or stat failed: %v", err)
	}
}
