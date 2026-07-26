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

func TestRunApplyYesDeletesRemovedManagedOutputAndStatefileRecord(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")

	testkit.WriteFile(t, tempDir, "instructions/active.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "removed managed\n")
	testkit.WriteFile(t, tempDir, "UNMANAGED.md", "leave alone\n")
	activeHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/active.md"))
	removedHash := testkit.HashPath(t, filepath.Join(tempDir, "CLAUDE.md"))

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
		testkit.InstructionPathState(t, "removed", []string{"claude-code"}, "project", "CLAUDE.md", removedHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
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
	if _, err := os.Stat(filepath.Join(tempDir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("removed output exists or stat failed: %v", err)
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "active managed\n")
	testkit.AssertFileContent(t, filepath.Join(tempDir, "UNMANAGED.md"), "leave alone\n")
}

func TestRunApplyYesDeletesOnlySelectedRemovedManagedOutput(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")

	testkit.WriteFile(t, tempDir, "instructions/active.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "GEMINI.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "codex removed\n")
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "claude removed\n")
	activeHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/active.md"))
	codexRemovedHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))
	claudeRemovedHash := testkit.HashPath(t, filepath.Join(tempDir, "CLAUDE.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex", "claude-code", "antigravity-cli"]

[instructions.active]
source = "instructions/active.md"
targets = ["antigravity-cli"]

[instructions.active.target.antigravity-cli]
render_to = "GEMINI.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
		Kind: testkit.ExactSupplyInstructions, Name: "active",
		SourceID: "local:instructions/active.md?mode=vendor", ContentHash: activeHash,
		Targets:      []target.Target{target.TargetAntigravityCLI},
		Destinations: map[target.Target]string{target.TargetAntigravityCLI: "GEMINI.md"},
	}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "active", []string{"antigravity-cli"}, "project", "GEMINI.md", activeHash),
		testkit.InstructionPathState(t, "removed-codex", []string{"codex"}, "project", "AGENTS.md", codexRemovedHash),
		testkit.InstructionPathState(t, "removed-claude", []string{"claude-code"}, "project", "CLAUDE.md", claudeRemovedHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI(
		[]string{"apply", "--manifest", manifestPath, "--target", "codex", "--yes"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "applied: 1 actions") {
		t.Fatalf("stdout = %q, want one selected delete action", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("selected removed output exists or stat failed: %v", err)
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "GEMINI.md"), "active managed\n")
	testkit.AssertFileContent(t, filepath.Join(tempDir, "CLAUDE.md"), "claude removed\n")

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResourceNamed(t, state, "active", "antigravity-cli", "project", "GEMINI.md", activeHash)
	testkit.AssertStateResourceMissing(t, state, "removed-codex", "codex", "project", "AGENTS.md")
	testkit.AssertStateResourceNamed(t, state, "removed-claude", "claude-code", "project", "CLAUDE.md", claudeRemovedHash)
}

func TestRunApplyYesUpdatesAndDeletesWithRecoveryJournalAndStatefile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")

	testkit.WriteFile(t, tempDir, "instructions/updated.md", "updated new\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "updated old\n")
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "removed managed\n")
	updatedHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/updated.md"))
	updatedStateHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))
	removedHash := testkit.HashPath(t, filepath.Join(tempDir, "CLAUDE.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.updated]
source = "instructions/updated.md"

[instructions.updated.target.codex]
render_to = "AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "updated", SourceID: "local:instructions/updated.md?mode=vendor", ContentHash: updatedHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "updated", []string{"codex"}, "project", "AGENTS.md", updatedStateHash),
		testkit.InstructionPathState(t, "removed", []string{"claude-code"}, "project", "CLAUDE.md", removedHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "applied: 2 actions") {
		t.Fatalf("stdout = %q, want update and delete actions", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "updated new\n")
	if _, err := os.Stat(filepath.Join(tempDir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("removed output exists or stat failed: %v", err)
	}

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResourceNamed(t, state, "updated", "codex", "project", "AGENTS.md", updatedHash)
	testkit.AssertStateResourceMissing(t, state, "removed", "claude-code", "project", "CLAUDE.md")
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}
