package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyRecordsStateWhenOutputAlreadyMatchesDesired(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "instructions\n")
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
	testkit.WriteStatefile(t, filepath.Join(tempDir, ".daem", "state.json"), testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", "sha256:stale-state"),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "applied: 1 actions") {
		t.Fatalf("stdout = %q, want one state record action", stdout.String())
	}

	content, err := os.ReadFile(filepath.Join(tempDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "instructions\n" {
		t.Fatalf("output content changed unexpectedly: %q", content)
	}

	state, err := statefile.Load(t.Context(), filepath.Join(tempDir, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResource(t, state, "codex", "AGENTS.md", instructionHash)
}

func TestRunApplyWritesGlobalCodexInstructionsUnderHome(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)

	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "global instructions\n")
	instructionHash := testkit.HashPath(t, sourcePath)

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.global]
source = "`+filepath.ToSlash(sourcePath)+`"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "global", SourceID: "local:" + filepath.ToSlash(sourcePath) + "?mode=vendor", ContentHash: instructionHash, Scope: "global"}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	content, err := os.ReadFile(filepath.Join(homeDir, ".codex", "AGENTS.md"))
	if err != nil {
		t.Fatalf("ReadFile global output returned error: %v", err)
	}
	if string(content) != "global instructions\n" {
		t.Fatalf("global output content = %q", content)
	}

	state, err := statefile.Load(t.Context(), filepath.Join(tempDir, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResourceNamed(t, state, "global", "codex", "global", "~/.codex/AGENTS.md", instructionHash)
}
