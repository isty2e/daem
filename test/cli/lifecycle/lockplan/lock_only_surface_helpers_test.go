package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func writeLockOnlyManifest(t *testing.T, path string) {
	t.Helper()

	root := filepath.Dir(path)
	testkit.WriteFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Use for oracle review.\n---\n")

	if err := os.WriteFile(path, []byte(`
version = 1
targets = ["opencode", "pi"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]

[[hook]]
name = "protect-env"
event = "pre_apply"
command = "python3 hooks/protect_env.py"
targets = ["pi"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile manifest returned error: %v", err)
	}
}

func writeLockOnlyLockfile(t *testing.T, path string) {
	t.Helper()

	root := filepath.Dir(path)
	skillHash := testkit.HashDirectory(t, filepath.Join(root, "skills/oracle"))
	testkit.WriteLockfile(t, path, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
		Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash,
		Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject,
	}))
}
