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

func TestRunApplyDiffRequiresDryRun(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--diff", "--yes"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "--diff requires --dry-run") {
		t.Fatalf("stderr = %q, want --diff diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyDryRunDiffShowsInstructionCreateWithoutWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "first line\nsecond line\n")
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

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--diff"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"dry-run: 1 actions",
		`create resource="instructions/project" target=codex scope=project destination="AGENTS.md" mode=copy reason=missing_output`,
		"diff: 1 files",
		`diff resource="instructions/project" target=codex scope=project destination="AGENTS.md"`,
		"--- /dev/null",
		"+++ desired/AGENTS.md",
		"+first line",
		"+second line",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run diff wrote output or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".daem")); !os.IsNotExist(err) {
		t.Fatalf("dry-run diff created state directory or stat failed: %v", err)
	}
}

func TestRunApplyDryRunDiffShowsManagedUpdate(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "keep\nnew\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "keep\nold\n")
	newHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
	oldHash := testkit.HashPath(t, filepath.Join(tempDir, "AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: newHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", oldHash),
	))
	stateBefore, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--diff"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		`update resource="instructions/project" target=codex scope=project destination="AGENTS.md" mode=copy reason=content_changed`,
		"diff: 1 files",
		"--- current/AGENTS.md",
		"+++ desired/AGENTS.md",
		" keep",
		"-old",
		"+new",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "keep\nold\n")
	stateAfter, err := os.ReadFile(statefilePath)
	if err != nil {
		t.Fatalf("ReadFile statefile after dry-run returned error: %v", err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("dry-run diff mutated statefile:\nbefore:\n%s\nafter:\n%s", stateBefore, stateAfter)
	}
}

func TestRunApplyDryRunDiffRespectsTargetSelection(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "shared instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex", "claude-code"]

[instructions.project.target.claude-code]
render_to = "CLAUDE.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash, Targets: []target.Target{target.TargetCodex, target.TargetClaudeCode}}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--diff", "--target", "claude-code"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"diff: 1 files",
		`diff resource="instructions/project" target=claude-code scope=project destination="CLAUDE.md"`,
		"+++ desired/CLAUDE.md",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	for _, reject := range []string{
		"target=codex",
		"desired/AGENTS.md",
	} {
		if strings.Contains(output, reject) {
			t.Fatalf("stdout = %q, did not want %q", output, reject)
		}
	}
}

func TestRunApplyDryRunDiffReportsEveryConsumerOfSharedInstructionPath(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "shared instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex", "opencode"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex", "opencode"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
		Kind: testkit.ExactSupplyInstructions, Name: "project",
		SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash,
		Targets: []target.Target{target.TargetCodex, target.TargetOpenCode},
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"apply", "--manifest", manifestPath, "--dry-run", "--diff"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"diff: 1 files",
		`diff resource="instructions/project" targets=codex,opencode scope=project destination="AGENTS.md"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	if strings.Contains(output, `diff resource="instructions/project" target=codex `) {
		t.Fatalf("stdout = %q, invented a primary target for shared output", output)
	}
}

func TestRunApplyDryRunDiffReportsZeroFilesForNoOp(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "already current\n")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "already current\n")
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
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", instructionHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--diff"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		`noop resource="instructions/project" target=codex scope=project destination="AGENTS.md" mode=copy reason=already_current`,
		"diff: 0 files",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
	for _, reject := range []string{
		"diff resource=",
		"--- current/",
		"+++ desired/",
	} {
		if strings.Contains(output, reject) {
			t.Fatalf("stdout = %q, did not want %q", output, reject)
		}
	}
}
