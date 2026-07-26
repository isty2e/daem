package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunListOutputsReportsManagedAndUnmanagedOutputs(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "desired instructions\n")
	desiredHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions", "AGENTS.md"))
	testkit.WriteFile(t, tempDir, "AGENTS.md", "manual instructions\n")
	liveHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "old managed\n")
	oldHash := testkit.HashPath(t, filepath.Join(tempDir, "CLAUDE.md"))
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: desiredHash}))
	testkit.WriteStatefile(t, filepath.Join(tempDir, ".daem", "state.json"), testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "old", []string{"claude-code"}, "project", "CLAUDE.md", oldHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"list", "outputs", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"inventory:",
		"managed: 1",
		`managed resource="instructions/old" target=claude-code scope=project path="CLAUDE.md" hash="` + oldHash + `"`,
		"unmanaged: 1",
		`unmanaged resource="instructions/project" target=codex scope=project path="AGENTS.md" hash="` + liveHash + `"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if strings.Contains(stdout.String(), "status:") {
		t.Fatalf("stdout = %q, did not want action plan", stdout.String())
	}
}

func TestRunListOutputsRespectsTargetSelection(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/shared.md", "desired instructions\n")
	desiredHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions", "shared.md"))
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "instructions/shared.md"
targets = ["codex", "claude-code"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/shared.md?mode=vendor", ContentHash: desiredHash, Targets: []target.Target{target.TargetCodex, target.TargetClaudeCode}}))
	testkit.WriteStatefile(t, filepath.Join(tempDir, ".daem", "state.json"), testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", desiredHash),
		testkit.InstructionPathState(t, "project", []string{"claude-code"}, "project", "CLAUDE.md", desiredHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"list", "outputs", "--manifest", manifestPath, "--target", "claude-code"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "target=codex") || strings.Contains(stdout.String(), "AGENTS.md") {
		t.Fatalf("stdout = %q, did not want codex inventory", stdout.String())
	}
	if !strings.Contains(stdout.String(), `managed resource="instructions/project" target=claude-code scope=project path="CLAUDE.md"`) {
		t.Fatalf("stdout = %q, want claude-code managed inventory", stdout.String())
	}
}

func TestRunStatusRejectsRemovedInventoryFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--inventory"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -inventory") {
		t.Fatalf("stderr = %q, want removed inventory diagnostic", stderr.String())
	}
}

func TestRunListOutputsRejectsStatusCheckFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"list", "outputs", "--check"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -check") {
		t.Fatalf("stderr = %q, want list parser diagnostic", stderr.String())
	}
}
