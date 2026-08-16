package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyDryRunDiffOmitsTextualDiffForBinaryContent(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte{'a', 0, 'b', '\n'}, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	instructionHash := testkit.HashPath(t, sourcePath)

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
	output := stdout.String()
	if !strings.Contains(output, "binary content differs; textual diff omitted") {
		t.Fatalf("stdout = %q, want binary omission", output)
	}
	if strings.Contains(output, "+a\x00b") {
		t.Fatalf("stdout contains raw binary content: %q", output)
	}
}

func TestRunApplyDryRunDiffOmitsOversizedDesiredContentAtCollection(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	sourcePath := filepath.Join(tempDir, "instructions", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	content := []byte(strings.Repeat("x", (4<<20)+1))
	if err := os.WriteFile(sourcePath, content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	instructionHash := testkit.HashPath(t, sourcePath)

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
	exitCode := testkit.RunVerboseCLI(
		[]string{"apply", "--manifest", manifestPath, "--dry-run", "--diff"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "inline diff omitted because the files are too large") {
		t.Fatalf("stdout = %q, want bounded collection omission", output)
	}
	if strings.Contains(output, strings.Repeat("x", 1024)) {
		t.Fatal("stdout contains oversized desired content")
	}
}

func TestRunApplyDryRunDiffSuppressesDiffForReadinessErrors(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "old instructions\n")
	oldHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "new instructions\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: oldHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--diff"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, `reason=stale_lock`) {
		t.Fatalf("stdout = %q, want stale lock action", output)
	}
	for _, reject := range []string{
		"diff:",
		"+++ desired/AGENTS.md",
		"+new instructions",
	} {
		if strings.Contains(output, reject) {
			t.Fatalf("stdout = %q, did not want %q", output, reject)
		}
	}
}

func TestRunApplyDryRunDiffSuppressesDiffForUnmanagedConflict(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "desired instructions\n")
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

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--diff"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, `reason=unmanaged_output_exists`) {
		t.Fatalf("stdout = %q, want unmanaged conflict action", output)
	}
	for _, reject := range []string{
		"diff:",
		"+++ desired/AGENTS.md",
		"+desired instructions",
		"-foreign instructions",
	} {
		if strings.Contains(output, reject) {
			t.Fatalf("stdout = %q, did not want %q", output, reject)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "foreign instructions\n")
}

func TestRunApplyDryRunDiffSuppressesDiffForDriftedManagedOutput(t *testing.T) {
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

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--diff"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, `reason=drifted_output`) {
		t.Fatalf("stdout = %q, want drifted output action", output)
	}
	for _, reject := range []string{
		"diff:",
		"+++ desired/AGENTS.md",
		"+desired instructions",
		"-local edits",
	} {
		if strings.Contains(output, reject) {
			t.Fatalf("stdout = %q, did not want %q", output, reject)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "local edits\n")
}

func TestRunApplyDryRunDiffRejectsUnsupportedModeBeforeDiffOutput(t *testing.T) {
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

[instructions.project.target.codex]
mode = "symlink"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash, InstallMode: "symlink"}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--diff"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "symlink mode") || !strings.Contains(stderr.String(), "AGENTS.md") {
		t.Fatalf("stderr = %q, want symlink diagnostic", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run diff wrote output or stat failed: %v", err)
	}
}

func TestRunApplyDryRunDiffMarksMissingFinalNewline(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "single line")
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
	output := stdout.String()
	for _, want := range []string{
		"+single line",
		`\ No newline at end of file`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}
