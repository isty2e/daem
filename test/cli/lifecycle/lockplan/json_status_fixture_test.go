package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

const (
	skillDiagnosticRepairable = "skill.compat.repairable"
	skillDiagnosticManual     = "skill.compat.manual"
)

func writeCurrentInvalidOpenCodeSkillFixture(t *testing.T, root string, manifestPath string, lockfilePath string, statefilePath string) {
	t.Helper()

	skillHash := writeInvalidOpenCodeSkillManifestAndLock(t, root, manifestPath, lockfilePath)
	testkit.WriteFile(t, root, ".opencode/skills/oracle/SKILL.md", "---\nname: oracle\n---\n")
	testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
		t,
		testkit.SkillPathState(t, "oracle", []string{"opencode"}, "project", ".opencode/skills/oracle", skillHash),
	))
}

func writeInvalidOpenCodeSkillManifestAndLock(t *testing.T, root string, manifestPath string, lockfilePath string) string {
	t.Helper()

	testkit.WriteFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\n---\n")
	skillHash := testkit.HashDirectory(t, filepath.Join(root, "skills", "oracle"))
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]
`)
	if manifestPath != filepath.Join(root, "daem.toml") {
		content, err := os.ReadFile(filepath.Join(root, "daem.toml"))
		if err != nil {
			t.Fatalf("ReadFile returned error: %v", err)
		}
		if err := os.WriteFile(manifestPath, content, 0o600); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}
	}
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash}))
	return skillHash
}
