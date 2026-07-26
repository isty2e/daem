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

func TestRunRecoverRollsBackInterruptedApplyAndRemovesJournal(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, currentState, nextState, oldHash, newHash := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "new instructions\n")

	var dryRunStdout bytes.Buffer
	var dryRunStderr bytes.Buffer
	dryRunExitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--dry-run"}, &dryRunStdout, &dryRunStderr)
	if dryRunExitCode != 0 {
		t.Fatalf("dry-run exitCode = %d, stderr = %q", dryRunExitCode, dryRunStderr.String())
	}
	for _, want := range []string{
		"recover: needs_rollback",
		`restore_write resource="instructions/project" target=codex scope=project destination="AGENTS.md" reason=restore_file backup="files/000001"`,
	} {
		if !strings.Contains(dryRunStdout.String(), want) {
			t.Fatalf("dry-run stdout = %q, want %q", dryRunStdout.String(), want)
		}
	}

	var yesStdout bytes.Buffer
	var yesStderr bytes.Buffer
	yesExitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--yes"}, &yesStdout, &yesStderr)
	if yesExitCode != 0 {
		t.Fatalf("yes exitCode = %d, stderr = %q", yesExitCode, yesStderr.String())
	}
	if !strings.Contains(yesStdout.String(), "recover: needs_rollback") {
		t.Fatalf("yes stdout = %q, want rollback plan", yesStdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "old instructions\n")
	state, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResource(t, state, "codex", "AGENTS.md", oldHash)
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
	if singleCLIManagedPath(t, state).ContentHash() == singleCLIManagedPath(t, nextState).ContentHash() ||
		newHash == oldHash {
		t.Fatalf("recovery did not distinguish before and after state")
	}
}

func TestRunRecoverRollsBackInterruptedCreateByDeletingCreatedFile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, currentState, _, newHash := captureCLIRecoveryCreateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "new instructions\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"recover: needs_rollback",
		`restore_delete resource="instructions/project" target=codex scope=project destination="AGENTS.md" reason=restore_absent`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(tempDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("created file still exists or stat failed unexpectedly: %v", err)
	}
	state, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResourceMissing(t, state, "project", "codex", "project", "AGENTS.md")
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
	if newHash == "" {
		t.Fatalf("create recovery test used empty desired hash")
	}
}

func TestRunRecoverRollsBackInterruptedDeleteByRestoringManagedFile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, currentState, _, oldHash := captureCLIRecoveryDeleteJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	if err := os.Remove(filepath.Join(tempDir, "AGENTS.md")); err != nil {
		t.Fatalf("Remove destination returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"recover: needs_rollback",
		`restore_write resource="instructions/project" target=codex scope=project destination="AGENTS.md" reason=restore_file backup="files/000001"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "old instructions\n")
	state, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResource(t, state, "codex", "AGENTS.md", oldHash)
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}

func TestRunRecoverRollsBackInterruptedSkillDirectoryUpdate(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, currentState, _, oldHash, newHash := captureCLIRecoverySkillUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	if err := testkit.ReplaceDirectoryAtomic(filepath.Join(tempDir, "desired/oracle"), filepath.Join(tempDir, ".agents/skills/oracle")); err != nil {
		t.Fatalf("testkit.ReplaceDirectoryAtomic returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"recover: needs_rollback",
		`restore_write resource="skill/oracle" target=codex scope=project destination=".agents/skills/oracle" reason=restore_directory backup="files/000001"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".agents/skills/oracle/SKILL.md"), "---\nname: oracle\nversion: old\n---\n")
	state, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "codex", "project", ".agents/skills/oracle", oldHash)
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
	if oldHash == newHash {
		t.Fatalf("recovery test used identical old and new skill hashes")
	}
}

func TestRunRecoverCleansUnstartedApplyJournal(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, currentState, _, oldHash, _ := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "recover: clean_before") {
		t.Fatalf("stdout = %q, want clean_before", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "old instructions\n")
	state, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResource(t, state, "codex", "AGENTS.md", oldHash)
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}

func TestRunRecoverCleansCompletedApplyJournal(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, _, nextState, _, newHash := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "new instructions\n")
	testkit.WriteStatefile(t, paths.StatefilePath, nextState)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "recover: clean_after") {
		t.Fatalf("stdout = %q, want clean_after", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "new instructions\n")
	state, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResource(t, state, "codex", "AGENTS.md", newHash)
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}

func TestRunRecoverBlocksWhenBackupNeededForRollbackIsMissing(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, currentState, _, _, _ := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "new instructions\n")
	backupPath := filepath.Join(paths.RecoveryDir, "20260621T120000.000000000Z-apply", "files", "000001")
	if err := os.Remove(backupPath); err != nil {
		t.Fatalf("Remove backup returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunCLI([]string{"recover", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"recover: blocked",
		"reason=backup_mismatch",
		"backup file is missing",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "new instructions\n")
	assertCLIRecoveryJournalActive(t, paths.RecoveryDir)
}

func TestRunRecoverBlocksWhenStatefileAfterButHostIsBefore(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, _, nextState, _, _ := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, nextState)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"recover: blocked",
		"reason=state_mismatch",
		"statefile is after apply but host paths are not clean_after",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "old instructions\n")
	assertCLIRecoveryJournalActive(t, paths.RecoveryDir)
}

func TestRunRecoverBlocksWhenHostDiffersFromBeforeAndAfter(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, currentState, _, _, _ := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "manual edits\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"recover: blocked",
		"reason=blocked",
		"path differs from both before and expected-after states",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "manual edits\n")
	assertCLIRecoveryJournalActive(t, paths.RecoveryDir)
}
