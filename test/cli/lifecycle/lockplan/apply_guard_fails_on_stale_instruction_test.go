package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunApplyFailsOnStaleInstructionLockWithoutWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "changed instructions\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: testkit.FixtureHash("sha256:stale")}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "stale_lock") {
		t.Fatalf("stderr = %q, want stale lock diagnostic", stderr.String())
	}
	wantNext := "next: run " + testkit.ExpectedShellCommand(t, "daem", "lock", "--manifest", manifestPath)
	if !strings.Contains(stderr.String(), wantNext) || strings.Contains(stderr.String(), "--lockfile") {
		t.Fatalf("stderr = %q, want lock recovery next step", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("stale apply wrote output or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf("stale apply created .daem or stat failed: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyRejectsStaleManagedInstructionLockBeforeNoop(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _ := writeStaleManagedInstructionFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "stale_lock") {
		t.Fatalf("stderr = %q, want stale lock diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "old instructions\n")
}

func TestRunApplyYesJSONRejectsStaleManagedInstructionLockBeforeNoop(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath, _ := writeStaleManagedInstructionFixture(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	payload := clijson.DecodeApplyResult(t, stdout.Bytes())
	if !payload.HasErrors || len(payload.Errors) != 1 {
		t.Fatalf("payload = %#v, want one error", payload)
	}
	clijson.RequireApplyFailure(
		t,
		payload,
		applyworkflow.FailureReasonApplyRefused,
		applyworkflow.FailurePhasePreflight,
		applyworkflow.FailureOutcomeRefused,
	)
	if len(payload.Actions) != 1 || payload.Actions[0].Reason != "stale_lock" {
		t.Fatalf("payload actions = %#v, want stale lock decision", payload.Actions)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "old instructions\n")
}

func TestRunApplyRejectsStaleManagedSkillLockBeforeNoop(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Old\n---\n")
	testkit.WriteFile(t, tempDir, ".agents/skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Old\n---\n")
	oldHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/oracle"))
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: New\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: oldHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.SkillPathState(t, "oracle", []string{"codex"}, "project", ".agents/skills/oracle", oldHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "stale_lock") {
		t.Fatalf("stderr = %q, want stale lock diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".agents/skills/oracle/SKILL.md"), "---\nname: oracle\ndescription: Old\n---\n")
}

func TestRunApplyFailsOnMissingInstructionLockWithoutWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")

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

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "missing_lock") {
		t.Fatalf("stderr = %q, want missing lock diagnostic", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("missing-lock apply wrote output or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf("missing-lock apply created .daem or stat failed: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyFailsOnStaleInstructionSourceIDWithoutWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:other.md?mode=vendor", ContentHash: instructionHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "stale_lock") {
		t.Fatalf("stderr = %q, want stale source_id diagnostic", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("stale source_id apply wrote output or stat failed: %v", err)
	}
}

func TestRunApplyRejectsUnmanagedExistingOutputBeforeRecoveryJournal(t *testing.T) {
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

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "unmanaged_output_exists") {
		t.Fatalf("stderr = %q, want unmanaged output diagnostic", stderr.String())
	}
	content, err := os.ReadFile(filepath.Join(tempDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "foreign instructions\n" {
		t.Fatalf("foreign output was modified: %q", content)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "snapshots")); !os.IsNotExist(err) {
		t.Fatalf("plan error created snapshots or stat failed: %v", err)
	}
}
