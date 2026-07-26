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

func TestRunStatusAppliesTargetSelection(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/shared.md", "shared\n")
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

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--target", "claude-code", "--target", "claude-code"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "target=claude-code") {
		t.Fatalf("stdout = %q, want claude-code action", stdout.String())
	}
	if strings.Contains(stdout.String(), "target=codex") {
		t.Fatalf("stdout = %q, want codex filtered out", stdout.String())
	}
}

func TestRunStatusReadsGlobalCodexInstructionsUnderHome(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)

	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "global instructions\n")
	testkit.WriteFile(t, homeDir, ".codex/AGENTS.md", "global instructions\n")
	instructionHash := testkit.HashPath(t, sourcePath)

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.global]
source = "`+filepath.ToSlash(sourcePath)+`"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "global", SourceID: "local:" + filepath.ToSlash(sourcePath) + "?mode=vendor", ContentHash: instructionHash, Scope: target.ScopeGlobal}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "global", []string{"codex"}, "global", "~/.codex/AGENTS.md", instructionHash),
	))
	testkit.WriteActiveOwnershipClaim(t, manifestPath, "~/.codex/AGENTS.md", "")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `noop resource="instructions/global" target=codex scope=global destination="~/.codex/AGENTS.md" mode=copy reason=already_current`) {
		t.Fatalf("stdout = %q, want global noop", stdout.String())
	}
}

func TestRunStatusRejectsEscapingStatefileDestination(t *testing.T) {
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
	escaping := testkit.InstructionPathState(t, "old", []string{"codex"}, "project", "AGENTS.md", "sha256:old")
	testkit.WriteStatefileWithInvalidManagedPathDestination(
		t,
		filepath.Join(tempDir, ".daem", "state.json"),
		testkit.Snapshot(t, escaping),
		"../outside.md",
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "relative path must be canonical and stay inside its selected root") {
		t.Fatalf("stderr = %q, want canonical destination diagnostic", stderr.String())
	}
	if _, err := os.Stat(outsidePath); !os.IsNotExist(err) {
		t.Fatalf("escaping status touched outside path or stat failed: %v", err)
	}
}

func TestRunStatusRejectsUnexpectedArguments(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "extra"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "unexpected argument") {
		t.Fatalf("stderr = %q, want unexpected argument diagnostic", stderr.String())
	}
}
