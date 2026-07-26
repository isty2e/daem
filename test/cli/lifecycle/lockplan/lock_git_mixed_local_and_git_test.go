package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockMixedLocalAndGitSkillsRemainSorted(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "skills/zeta/SKILL.md", "---\nname: zeta\ndescription: zeta\n---\n")
	testkit.CommitRepository(t, repoPath, "add zeta skill")
	testkit.WriteFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "zeta"
source = { git = "`+repoPath+`", path = "skills/zeta", ref = "main" }

[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	locked, err := lockfile.Load(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	if testkit.LockedSkills(t, locked)[0].Name != "alpha" || testkit.LockedSkills(t, locked)[1].Name != "zeta" {
		t.Fatalf("skills not sorted by name: %#v", testkit.LockedSkills(t, locked))
	}
}
