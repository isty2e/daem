package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunStatusReportsPlanCategoriesWithoutWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)

	testkit.WriteFile(t, tempDir, "instructions/current.md", "new current\n")
	testkit.WriteFile(t, tempDir, "instructions/ready.md", "ready\n")
	testkit.WriteFile(t, tempDir, "instructions/missing.md", "missing\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "old current\n")
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "ready\n")
	testkit.WriteFile(t, tempDir, "GEMINI.md", "old managed\n")

	currentHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/current.md"))
	readyHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/ready.md"))
	missingSourcePath := filepath.Join(tempDir, "instructions/missing.md")
	missingHash := testkit.HashPath(t, missingSourcePath)
	oldCurrentHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))
	oldManagedHash := testkit.HashPath(t, filepath.Join(tempDir, "GEMINI.md"))

	if err := os.WriteFile(manifestPath, fmt.Appendf(nil, `
version = 1
targets = ["codex", "claude-code"]

[instructions.current]
source = "instructions/current.md"
targets = ["codex"]

[instructions.current.target.codex]
render_to = "AGENTS.md"

[instructions.ready]
source = "instructions/ready.md"
targets = ["claude-code"]

[instructions.ready.target.claude-code]
render_to = "CLAUDE.md"

[instructions.missing]
source = %q
targets = ["codex"]
scope = "global"

[instructions.missing.target.codex]
render_to = "AGENTS.md"
`, missingSourcePath), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(
		t,
		testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "current", SourceID: "local:instructions/current.md?mode=vendor", ContentHash: currentHash},
		testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "ready", SourceID: "local:instructions/ready.md?mode=vendor", ContentHash: readyHash, Targets: []target.Target{target.TargetClaudeCode}},
		testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "missing", SourceID: "local:" + filepath.ToSlash(missingSourcePath) + "?mode=vendor", ContentHash: missingHash, Scope: target.ScopeGlobal},
	))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "current", []string{"codex"}, "project", "AGENTS.md", oldCurrentHash),
		testkit.InstructionPathState(t, "ready", []string{"claude-code"}, "project", "CLAUDE.md", readyHash),
		testkit.InstructionPathState(t, "removed", []string{"antigravity-cli"}, "project", "GEMINI.md", oldManagedHash),
	))
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	output := stdout.String()
	for _, want := range []string{
		"status: 4 actions",
		`create resource="instructions/missing" target=codex scope=global destination="~/.codex/AGENTS.md" mode=copy reason=missing_output`,
		`delete resource="instructions/removed" target=antigravity-cli scope=project destination="GEMINI.md" mode= reason=removed_from_manifest safety=deletable`,
		`noop resource="instructions/ready" target=claude-code scope=project destination="CLAUDE.md" mode=copy reason=already_current`,
		`update resource="instructions/current" target=codex scope=project destination="AGENTS.md" mode=copy reason=content_changed`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}

	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile after status returned error: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("status mutated statefile:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
	}
	if content, err := os.ReadFile(filepath.Join(tempDir, "AGENTS.md")); err != nil || string(content) != "old current\n" {
		t.Fatalf("status mutated AGENTS.md or read failed: content=%q err=%v", content, err)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".codex", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("status created missing output or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem", "snapshots")); !os.IsNotExist(err) {
		t.Fatalf("status created snapshots or stat failed: %v", err)
	}
}

func TestRunStatusCheckExitsZeroForCleanPlan(t *testing.T) {
	fixture := writeStatusCheckFixture(t, statusCheckFixtureInput{
		sourceContent: "managed\n",
		hostContent:   "managed\n",
		writeLockfile: true,
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", fixture.manifestPath, "--check"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `noop resource="instructions/project"`) {
		t.Fatalf("stdout = %q, want noop action", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunStatusCheckExitsOneForPendingManagedChange(t *testing.T) {
	fixture := writeStatusCheckFixture(t, statusCheckFixtureInput{
		sourceContent: "new\n",
		hostContent:   "old\n",
		writeLockfile: true,
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", fixture.manifestPath, "--check"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `update resource="instructions/project"`) {
		t.Fatalf("stdout = %q, want update action", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunStatusCheckExitsOneForMissingLockfile(t *testing.T) {
	fixture := writeStatusCheckFixture(t, statusCheckFixtureInput{
		sourceContent: "managed\n",
		hostContent:   "managed\n",
		writeLockfile: false,
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", fixture.manifestPath, "--check"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "lockfile: missing") {
		t.Fatalf("stdout = %q, want missing lockfile diagnostic", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunStatusCheckJSONUsesCheckModeAndExitCode(t *testing.T) {
	fixture := writeStatusCheckFixture(t, statusCheckFixtureInput{
		sourceContent: "new\n",
		hostContent:   "old\n",
		writeLockfile: true,
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", fixture.manifestPath, "--check", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		`"mode": "check"`,
		`"kind": "update"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
