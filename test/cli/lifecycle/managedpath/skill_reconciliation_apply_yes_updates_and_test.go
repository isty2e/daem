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

func TestRunApplyYesUpdatesAndDeletesManagedSkillDirectories(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "skills/active/SKILL.md", "---\nname: active\ndescription: active\nversion: new\n---\n")
	testkit.WriteFile(t, tempDir, ".agents/skills/active/SKILL.md", "---\nname: active\nversion: old\n---\n")
	testkit.WriteFile(t, tempDir, ".agents/skills/removed/SKILL.md", "---\nname: removed\n---\n")
	activeNewHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/active"))
	activeOldHash := testkit.HashDirectory(t, filepath.Join(tempDir, ".agents/skills/active"))
	removedHash := testkit.HashDirectory(t, filepath.Join(tempDir, ".agents/skills/removed"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "active"
source = { path = "skills/active", mode = "vendor" }
targets = ["codex"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplySkill, Name: "active", SourceID: "local:skills/active?mode=vendor", ContentHash: activeNewHash}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.SkillPathState(t, "active", []string{"codex"}, "project", ".agents/skills/active", activeOldHash),
		testkit.SkillPathState(t, "removed", []string{"codex"}, "project", ".agents/skills/removed", removedHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "applied: 2 actions") {
		t.Fatalf("stdout = %q, want update and delete actions", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".agents/skills/active/SKILL.md"), "---\nname: active\ndescription: active\nversion: new\n---\n")
	if _, err := os.Stat(filepath.Join(tempDir, ".agents/skills/removed")); !os.IsNotExist(err) {
		t.Fatalf("removed skill directory exists or stat failed: %v", err)
	}

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertSkillPathState(t, state, "active", "codex", "project", ".agents/skills/active", activeNewHash)
	testkit.AssertSkillPathStateMissing(t, state, "removed", "project", ".agents/skills/removed")
	testkit.AssertNoRecoveryArtifacts(t, tempDir)
}

func TestRunApplyYesDeletesRemovedOpenCodeSkillDirectory(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	testkit.WriteFile(t, tempDir, "skills/active/SKILL.md", "---\nname: active\ndescription: Active skill.\n---\n")
	testkit.WriteFile(t, tempDir, ".opencode/skills/removed/SKILL.md", "---\nname: removed\ndescription: Removed skill.\n---\n")
	activeHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/active"))
	removedHash := testkit.HashDirectory(t, filepath.Join(tempDir, ".opencode/skills/removed"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "active"
source = { path = "skills/active", mode = "vendor" }
targets = ["opencode"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
		Kind: testkit.ExactSupplySkill, Name: "active", SourceID: "local:skills/active?mode=vendor", ContentHash: activeHash,
		Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject,
	}))
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.SkillPathState(t, "removed", []string{"opencode"}, "project", ".opencode/skills/removed", removedHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--target", "opencode", "--yes"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".opencode/skills/removed")); !os.IsNotExist(err) {
		t.Fatalf("removed skill directory exists or stat failed: %v", err)
	}

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertSkillPathStateMissing(t, state, "removed", "project", ".opencode/skills/removed")
}
