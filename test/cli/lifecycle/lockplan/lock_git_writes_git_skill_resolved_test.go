package cli_test

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockWritesGitSkillResolvedCommit(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	commit := testkit.CommitRepository(t, repoPath, "add oracle skill")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { git = "`+repoPath+`", path = "skills/oracle", ref = "main" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	locked, err := lockfile.Load(t.Context(), filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	if len(testkit.LockedSkills(t, locked)) != 1 {
		t.Fatalf("len(locked skills) = %d, want 1", len(testkit.LockedSkills(t, locked)))
	}

	lockedSkill := testkit.LockedSkills(t, locked)[0]
	wantSourceID := "git:locator=" + url.QueryEscape(repoPath) + "&path=skills%2Foracle&ref=name%3Amain"
	if lockedSkill.SourceID != wantSourceID {
		t.Fatalf("SourceID = %q", lockedSkill.SourceID)
	}

	if lockedSkill.ResolvedRef != commit {
		t.Fatalf("ResolvedRef = %q, want %q", lockedSkill.ResolvedRef, commit)
	}

	if lockedSkill.ContentHash == "" {
		t.Fatal("ContentHash is empty")
	}

	firstContent, err := os.ReadFile(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if strings.Contains(string(firstContent), ".daem") {
		t.Fatalf("lockfile content = %q, want no cache artifact paths", firstContent)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("second exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	secondContent, err := os.ReadFile(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if string(firstContent) != string(secondContent) {
		t.Fatalf("git lockfile changed across identical runs:\nfirst:\n%s\nsecond:\n%s", firstContent, secondContent)
	}
}

func TestRunLockExpandsGitRootSelectorSkillGroup(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	testkit.WriteFile(t, repoPath, "beta/SKILL.md", "---\nname: beta\ndescription: beta\n---\n")
	commit := testkit.CommitRepository(t, repoPath, "add root skills")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill_group]]
source = { git = "`+repoPath+`", path = ".", ref = "main" }
include = ["glob:*"]
targets = ["codex"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	locked, err := lockfile.Load(t.Context(), filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 2 {
		t.Fatalf("locked skills = %#v, want two selector-expanded entries", testkit.LockedSkills(t, locked))
	}
	if testkit.LockedSkills(t, locked)[0].Name != "alpha" || testkit.LockedSkills(t, locked)[1].Name != "beta" {
		t.Fatalf("locked skills = %#v, want alpha then beta", testkit.LockedSkills(t, locked))
	}
	for _, lockedSkill := range testkit.LockedSkills(t, locked) {
		if lockedSkill.ResolvedRef != commit {
			t.Fatalf("locked skill %#v resolved_ref, want %q", lockedSkill, commit)
		}
	}
}

func TestRunLockAllowsGitRootIndividualSkill(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "SKILL.md", "---\nname: root\ndescription: root\n---\n")
	commit := testkit.CommitRepository(t, repoPath, "add root skill")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "root"
source = { git = "`+repoPath+`", path = ".", ref = "main" }
targets = ["codex"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	locked, err := lockfile.Load(t.Context(), filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 1 {
		t.Fatalf("locked skills = %#v, want one root skill", testkit.LockedSkills(t, locked))
	}
	rootSkill := testkit.LockedSkills(t, locked)[0]
	if rootSkill.Name != "root" {
		t.Fatalf("locked skill name = %q, want root", rootSkill.Name)
	}
	wantSourceID := "git:locator=" + url.QueryEscape(repoPath) + "&path=.&ref=name%3Amain"
	if rootSkill.SourceID != wantSourceID {
		t.Fatalf("root SourceID = %q", rootSkill.SourceID)
	}
	if rootSkill.ResolvedRef != commit {
		t.Fatalf("root ResolvedRef = %q, want %q", rootSkill.ResolvedRef, commit)
	}
}

func TestRunLockRejectsGitRootIndividualSkillWithoutSkillFile(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "README.md", "not a skill\n")
	testkit.CommitRepository(t, repoPath, "add non-skill root")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "root"
source = { git = "`+repoPath+`", path = ".", ref = "main" }
targets = ["codex"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `missing SKILL.md`) {
		t.Fatalf("stderr = %q, want missing SKILL.md validation", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("lockfile exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunLockRejectsGitRootIndividualSkillWithLowercaseSkillFile(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "skill.md", "---\nname: root\ndescription: lowercase\n---\n")
	testkit.CommitRepository(t, repoPath, "add lowercase skill file")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "root"
source = { git = "`+repoPath+`", path = ".", ref = "main" }
targets = ["codex"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `missing SKILL.md`) {
		t.Fatalf("stderr = %q, want case-sensitive missing SKILL.md validation", stderr.String())
	}
}

func TestRunLockGitRootSelectorIgnoresRootSkillFile(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "SKILL.md", "---\nname: root\ndescription: root\n---\n")
	testkit.WriteFile(t, repoPath, "alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	commit := testkit.CommitRepository(t, repoPath, "add root and child skills")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill_group]]
source = { git = "`+repoPath+`", path = ".", ref = "main" }
include = ["glob:*"]
targets = ["codex"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	locked, err := lockfile.Load(t.Context(), filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 1 || testkit.LockedSkills(t, locked)[0].Name != "alpha" {
		t.Fatalf("locked skills = %#v, want only child alpha", testkit.LockedSkills(t, locked))
	}
	if testkit.LockedSkills(t, locked)[0].ResolvedRef != commit {
		t.Fatalf("ResolvedRef = %q, want %q", testkit.LockedSkills(t, locked)[0].ResolvedRef, commit)
	}
}
