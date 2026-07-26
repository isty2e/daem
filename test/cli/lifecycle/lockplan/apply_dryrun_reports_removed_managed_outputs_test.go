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

func TestRunApplyDryRunReportsRemovedManagedOutputsWithoutWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)

	testkit.WriteFile(t, tempDir, "instructions/active.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "instructions/changed.md", "changed desired\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "active managed\n")
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "changed old\n")
	testkit.WriteFile(t, homeDir, ".codex/AGENTS.md", "removed managed\n")
	activeHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/active.md"))
	changedHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/changed.md"))
	changedStateHash := testkit.HashPath(t, filepath.Join(tempDir, "CLAUDE.md"))
	removedHash := testkit.HashPath(t, filepath.Join(homeDir, ".codex", "AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex", "claude-code"]

[instructions.active]
source = "instructions/active.md"
targets = ["codex"]

[instructions.active.target.codex]
render_to = "AGENTS.md"

[instructions.changed]
source = "instructions/changed.md"
targets = ["claude-code"]

[instructions.changed.target.claude-code]
render_to = "CLAUDE.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(
		t,
		testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "active", SourceID: "local:instructions/active.md?mode=vendor", ContentHash: activeHash},
		testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "changed", SourceID: "local:instructions/changed.md?mode=vendor", ContentHash: changedHash, Targets: []target.Target{target.TargetClaudeCode}},
	))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "active", []string{"codex"}, "project", "AGENTS.md", activeHash),
		testkit.InstructionPathState(t, "changed", []string{"claude-code"}, "project", "CLAUDE.md", changedStateHash),
		testkit.InstructionPathState(t, "removed", []string{"codex"}, "global", "~/.codex/AGENTS.md", removedHash),
	))
	testkit.WriteActiveOwnershipClaim(t, manifestPath, "~/.codex/AGENTS.md", "")
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile returned error: %v", err)
	}

	var statusStdout bytes.Buffer
	var statusStderr bytes.Buffer
	statusExitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &statusStdout, &statusStderr)
	if statusExitCode != 0 {
		t.Fatalf("status exitCode = %d, stderr = %q", statusExitCode, statusStderr.String())
	}
	if statusStderr.Len() != 0 {
		t.Fatalf("status stderr = %q, want empty", statusStderr.String())
	}

	var dryRunStdout bytes.Buffer
	var dryRunStderr bytes.Buffer
	dryRunExitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &dryRunStdout, &dryRunStderr)
	if dryRunExitCode != 0 {
		t.Fatalf("dry-run exitCode = %d, stderr = %q", dryRunExitCode, dryRunStderr.String())
	}
	if dryRunStderr.Len() != 0 {
		t.Fatalf("dry-run stderr = %q, want empty", dryRunStderr.String())
	}

	deleteAction := `delete resource="instructions/removed" target=codex scope=global destination="~/.codex/AGENTS.md" mode= reason=removed_from_manifest safety=deletable`
	if !strings.Contains(statusStdout.String(), deleteAction) {
		t.Fatalf("status stdout = %q, want %q", statusStdout.String(), deleteAction)
	}
	for _, want := range []string{
		"dry-run: 3 actions",
		`noop resource="instructions/active" target=codex scope=project destination="AGENTS.md" mode=copy reason=already_current`,
		`update resource="instructions/changed" target=claude-code scope=project destination="CLAUDE.md" mode=copy reason=content_changed`,
		deleteAction,
	} {
		if !strings.Contains(dryRunStdout.String(), want) {
			t.Fatalf("dry-run stdout = %q, want %q", dryRunStdout.String(), want)
		}
	}

	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile after dry-run returned error: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("dry-run mutated statefile:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "active managed\n")
	testkit.AssertFileContent(t, filepath.Join(tempDir, "CLAUDE.md"), "changed old\n")
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".codex", "AGENTS.md"), "removed managed\n")
}
