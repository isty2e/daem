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

func TestRunApplyYesCreatesCodexProjectSkillDirectoryAndStatefileRecord(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	testkit.WriteFile(t, tempDir, "skills/oracle/scripts/check.sh", "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(tempDir, "skills/oracle/scripts/check.sh"), 0o700); err != nil {
		t.Fatalf("Chmod script returned error: %v", err)
	}
	skillHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/oracle"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "lock-only:") {
		t.Fatalf("stdout = %q, want no lock-only summary for supported codex skill", stdout.String())
	}
	if !strings.Contains(stdout.String(), "applied: 1 actions") {
		t.Fatalf("stdout = %q, want one skill create action", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".agents/skills/oracle/SKILL.md"), "---\nname: oracle\ndescription: oracle\n---\n")
	if testkit.HashDirectory(t, filepath.Join(tempDir, ".agents/skills/oracle")) != skillHash {
		t.Fatalf("installed skill hash does not match source hash")
	}

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "codex", "project", ".agents/skills/oracle", skillHash)
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}

func TestRunApplyDryRunUsesLockExpandedSelectorSkillsWithoutRediscovery(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/active/SKILL.md", "---\nname: active\ndescription: active\n---\n")

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
targets = ["codex"]
`)
	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &lockStdout, &lockStderr); exitCode != 0 {
		t.Fatalf("initial lock exitCode = %d, stdout = %q stderr = %q", exitCode, lockStdout.String(), lockStderr.String())
	}
	testkit.WriteFile(t, tempDir, "skills/new/SKILL.md", "---\nname: new\ndescription: new\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "skill/new") || strings.Contains(stdout.String(), "missing_lock") {
		t.Fatalf("stdout = %q, want apply to consume only lockfile-expanded selector entries", stdout.String())
	}
}

func TestRunApplyDryRunReportsStaleLockForSelectorExpandedSkill(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/active/SKILL.md", "---\nname: active\ndescription: before lock\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:active"]
targets = ["codex"]
`)
	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &lockStdout, &lockStderr); exitCode != 0 {
		t.Fatalf("initial lock exitCode = %d, stdout = %q stderr = %q", exitCode, lockStdout.String(), lockStderr.String())
	}
	testkit.WriteFile(t, tempDir, "skills/active/SKILL.md", "---\nname: active\ndescription: changed after lock\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), `error resource="skill/active"`) || !strings.Contains(stdout.String(), `reason=stale_lock`) {
		t.Fatalf("stdout = %q, want stale lock plan error for selector-expanded skill", stdout.String())
	}
}

func TestRunStatusAndApplyRejectSelectorSourcePathChangeUnderOldLock(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/active/SKILL.md", "---\nname: active\ndescription: old source\n---\n")
	testkit.WriteFile(t, tempDir, "other-skills/active/SKILL.md", "---\nname: active\ndescription: new source\n---\n")

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:active"]
targets = ["codex"]
`)
	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &lockStdout, &lockStderr); exitCode != 0 {
		t.Fatalf("initial lock exitCode = %d, stdout = %q stderr = %q", exitCode, lockStdout.String(), lockStderr.String())
	}
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "other-skills", mode = "vendor" }
include = ["glob:active"]
targets = ["codex"]
`)

	var statusStdout bytes.Buffer
	var statusStderr bytes.Buffer
	statusExitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath}, &statusStdout, &statusStderr)
	if statusExitCode != 1 {
		t.Fatalf("status exitCode = %d, want 1; stdout = %q stderr = %q", statusExitCode, statusStdout.String(), statusStderr.String())
	}
	if statusStdout.Len() != 0 || !strings.Contains(statusStderr.String(), "belongs to a stale skill_group declaration") {
		t.Fatalf("status stdout = %q stderr = %q, want stale selector declaration error", statusStdout.String(), statusStderr.String())
	}

	var applyStdout bytes.Buffer
	var applyStderr bytes.Buffer
	applyExitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &applyStdout, &applyStderr)
	if applyExitCode != 1 {
		t.Fatalf("apply exitCode = %d, want 1; stdout = %q stderr = %q", applyExitCode, applyStdout.String(), applyStderr.String())
	}
	if applyStdout.Len() != 0 || !strings.Contains(applyStderr.String(), "belongs to a stale skill_group declaration") {
		t.Fatalf("apply stdout = %q stderr = %q, want stale selector declaration error", applyStdout.String(), applyStderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".agents", "skills", "active")); !os.IsNotExist(err) {
		t.Fatalf("old-lock selector apply wrote host output or stat failed: %v", err)
	}
}

func TestRunApplyYesDeletesSkillRemovedBySelectorAgainstLockedEntries(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "skills/active/SKILL.md", "---\nname: active\ndescription: active\n---\n")
	testkit.WriteFile(t, tempDir, "skills/removed/SKILL.md", "---\nname: removed\ndescription: removed\n---\n")
	testkit.WriteFile(t, tempDir, ".agents/skills/active/SKILL.md", "---\nname: active\ndescription: active\n---\n")
	testkit.WriteFile(t, tempDir, ".agents/skills/removed/SKILL.md", "---\nname: removed\ndescription: removed\n---\n")
	activeHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/active"))
	removedHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/removed"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
targets = ["codex"]
`)
	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &lockStdout, &lockStderr); exitCode != 0 {
		t.Fatalf("initial lock exitCode = %d, stdout = %q stderr = %q", exitCode, lockStdout.String(), lockStderr.String())
	}
	if err := os.RemoveAll(filepath.Join(tempDir, "skills/removed")); err != nil {
		t.Fatalf("RemoveAll removed source returned error: %v", err)
	}
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:active"]
targets = ["codex"]
`)
	lockStdout.Reset()
	lockStderr.Reset()
	if exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &lockStdout, &lockStderr); exitCode != 0 {
		t.Fatalf("updated lock exitCode = %d, stdout = %q stderr = %q", exitCode, lockStdout.String(), lockStderr.String())
	}
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.SkillPathState(t, "active", []string{"codex"}, "project", ".agents/skills/active", activeHash),
		testkit.SkillPathState(t, "removed", []string{"codex"}, "project", ".agents/skills/removed", removedHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "applied: 1 actions") {
		t.Fatalf("stdout = %q, want one selector removal delete action", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".agents/skills/removed")); !os.IsNotExist(err) {
		t.Fatalf("removed selector skill directory exists or stat failed: %v", err)
	}

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "active", "codex", "project", ".agents/skills/active", activeHash)
	testkit.AssertSkillPathStateMissing(t, state, "removed", "project", ".agents/skills/removed")
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}

func TestRunApplyYesCreatesCodexGlobalSkillUnderHome(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	sourcePath := filepath.Join(tempDir, "skills", "oracle")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	skillHash := testkit.HashDirectory(t, sourcePath)

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "`+filepath.ToSlash(sourcePath)+`", mode = "vendor" }
targets = ["codex"]
scope = "global"
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
		Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:" + filepath.ToSlash(sourcePath) + "?mode=vendor", ContentHash: skillHash,
		Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeGlobal,
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".agents/skills/oracle/SKILL.md"), "---\nname: oracle\ndescription: oracle\n---\n")

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "codex", "global", "~/.agents/skills/oracle", skillHash)
}

func TestRunApplyYesCreatesOpenCodeProjectSkillDirectoryAndStatefileRecord(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Use for oracle review.\n---\n")
	skillHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/oracle"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
		Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash,
		Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject,
	}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--target", "opencode", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "lock-only:") {
		t.Fatalf("stdout = %q, want no lock-only summary for supported opencode skill", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".opencode/skills/oracle/SKILL.md"), "---\nname: oracle\ndescription: Use for oracle review.\n---\n")

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "oracle", "opencode", "project", ".opencode/skills/oracle", skillHash)
}
