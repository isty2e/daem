package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/test/testkit"
)

func TestApplyUsesManifestRootForSubdirectoryManifest(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "project")
	manifestPath, _, instructionHash := writeSubdirectoryApplyProject(t, projectDir)

	var applyStdout bytes.Buffer
	var applyStderr bytes.Buffer
	applyExitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &applyStdout, &applyStderr)
	if applyExitCode != 0 {
		t.Fatalf("apply exitCode = %d, stderr = %q", applyExitCode, applyStderr.String())
	}
	testkit.AssertStateResource(t, mustLoadCLIStatefile(t, filepath.Join(projectDir, ".daem", "state.json")), "codex", "AGENTS.md", instructionHash)
	testkit.AssertNoRecoveryArtifacts(t, projectDir)
}

func TestRecoverUsesManifestRootForSubdirectoryManifest(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "project")
	manifestPath := filepath.Join(projectDir, "daem.toml")
	paths, currentState, _, oldHash, _ := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, projectDir, "AGENTS.md", "new instructions\n")

	var recoverStdout bytes.Buffer
	var recoverStderr bytes.Buffer
	recoverExitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--yes"}, &recoverStdout, &recoverStderr)
	if recoverExitCode != 0 {
		t.Fatalf("recover exitCode = %d, stderr = %q", recoverExitCode, recoverStderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(projectDir, "AGENTS.md"), "old instructions\n")
	testkit.AssertStateResource(t, mustLoadCLIStatefile(t, paths.StatefilePath), "codex", "AGENTS.md", oldHash)
	testkit.AssertNoRecoveryArtifacts(t, projectDir)
}

func TestRecoverUsesRelativeManifestRoot(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "project")
	absoluteManifestPath := filepath.Join(projectDir, "daem.toml")
	paths, currentState, _, oldHash, _ := captureCLIRecoveryUpdateJournal(t, absoluteManifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, projectDir, "AGENTS.md", "new instructions\n")
	testkit.WithWorkingDirectory(t, tempDir)

	manifestPath := filepath.Join("project", "daem.toml")
	var recoverStdout bytes.Buffer
	var recoverStderr bytes.Buffer
	recoverExitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--yes"}, &recoverStdout, &recoverStderr)
	if recoverExitCode != 0 {
		t.Fatalf("recover exitCode = %d, stderr = %q", recoverExitCode, recoverStderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(projectDir, "AGENTS.md"), "old instructions\n")
	testkit.AssertStateResource(t, mustLoadCLIStatefile(t, paths.StatefilePath), "codex", "AGENTS.md", oldHash)
}

func writeSubdirectoryApplyProject(t *testing.T, projectDir string) (string, string, string) {
	t.Helper()

	manifestPath := filepath.Join(projectDir, "daem.toml")
	lockfilePath := filepath.Join(projectDir, "daem.lock.toml")
	testkit.WriteFile(t, projectDir, "instructions/AGENTS.md", "project instructions\n")
	instructionHash := testkit.HashPath(t, filepath.Join(projectDir, "instructions/AGENTS.md"))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`), 0o600); err != nil {
		t.Fatalf("WriteFile manifest returned error: %v", err)
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	return manifestPath, lockfilePath, instructionHash
}

func mustLoadCLIStatefile(t *testing.T, path string) durable.Snapshot {
	t.Helper()

	state, err := statefile.Load(t.Context(), path)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}

	return state
}
