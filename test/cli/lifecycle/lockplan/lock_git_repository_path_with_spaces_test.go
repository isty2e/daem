package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockGitRepositoryPathWithSpaces(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "project with spaces")
	manifestPath := filepath.Join(projectDir, "daem.toml")
	lockfilePath := filepath.Join(projectDir, "daem.lock.toml")
	repoParent := filepath.Join(tempDir, "repo parent with spaces")
	repoPath := testkit.InitGitRepository(t, repoParent)
	testkit.WriteFile(t, repoPath, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	commit := testkit.CommitRepository(t, repoPath, "add oracle skill")

	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

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

	if !strings.Contains(stdout.String(), lockfilePath) {
		t.Fatalf("stdout = %q, want derived lockfile path", stdout.String())
	}

	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	if testkit.LockedSkills(t, locked)[0].ResolvedRef != commit {
		t.Fatalf("ResolvedRef = %q, want %q", testkit.LockedSkills(t, locked)[0].ResolvedRef, commit)
	}
}

func TestRunLockMissingGitRefDoesNotWriteLockfile(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	testkit.CommitRepository(t, repoPath, "add oracle skill")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { git = "`+repoPath+`", path = "skills/oracle", ref = "missing-ref" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}

	if !strings.Contains(stderr.String(), "resolve git ref name:missing-ref") {
		t.Fatalf("stderr = %q, want missing ref diagnostic", stderr.String())
	}

	if _, err := os.Stat(filepath.Join(tempDir, "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("lockfile exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunLockMissingGitSourcePathDoesNotPrintLocalSourceHint(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	testkit.CommitRepository(t, repoPath, "add oracle skill")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { git = "`+repoPath+`", path = "skills/missing", ref = "main" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}

	if !strings.Contains(stderr.String(), `export git source path "skills/missing"`) {
		t.Fatalf("stderr = %q, want missing git source path diagnostic", stderr.String())
	}
	if strings.Contains(stderr.String(), "next:") {
		t.Fatalf("stderr = %q, want no local source next steps", stderr.String())
	}

	if _, err := os.Stat(filepath.Join(tempDir, "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("lockfile exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunLockUpdatesGitRefAndHashWhenBranchMoves(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	skillPath := filepath.Join(repoPath, "skills/oracle/SKILL.md")
	testkit.WriteFile(t, repoPath, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: one\n---\n")
	firstCommit := testkit.CommitRepository(t, repoPath, "add oracle skill")

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

	firstLock, err := lockfile.Load(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	if testkit.LockedSkills(t, firstLock)[0].ResolvedRef != firstCommit {
		t.Fatalf("first ResolvedRef = %q, want %q", testkit.LockedSkills(t, firstLock)[0].ResolvedRef, firstCommit)
	}

	if err := os.WriteFile(skillPath, []byte("---\nname: oracle\ndescription: two\n---\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	secondCommit := testkit.CommitRepository(t, repoPath, "update oracle skill")

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("second exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	secondLock, err := lockfile.Load(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	if testkit.LockedSkills(t, secondLock)[0].ResolvedRef != secondCommit {
		t.Fatalf("second ResolvedRef = %q, want %q", testkit.LockedSkills(t, secondLock)[0].ResolvedRef, secondCommit)
	}

	if testkit.LockedSkills(t, secondLock)[0].ContentHash == testkit.LockedSkills(t, firstLock)[0].ContentHash {
		t.Fatalf("ContentHash did not change after branch moved: %q", testkit.LockedSkills(t, secondLock)[0].ContentHash)
	}
}

func TestRunLockUpdatesGitRootSkillWhenBranchMoves(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	skillPath := filepath.Join(repoPath, "SKILL.md")
	testkit.WriteFile(t, repoPath, "SKILL.md", "---\nname: root\ndescription: one\n---\n")
	firstCommit := testkit.CommitRepository(t, repoPath, "add root skill")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "root"
source = { git = "`+repoPath+`", path = ".", ref = "main" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	firstLock, err := lockfile.Load(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	if testkit.LockedSkills(t, firstLock)[0].ResolvedRef != firstCommit {
		t.Fatalf("first ResolvedRef = %q, want %q", testkit.LockedSkills(t, firstLock)[0].ResolvedRef, firstCommit)
	}

	if err := os.WriteFile(skillPath, []byte("---\nname: root\ndescription: two\n---\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	secondCommit := testkit.CommitRepository(t, repoPath, "update root skill")

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("second exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	secondLock, err := lockfile.Load(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	if testkit.LockedSkills(t, secondLock)[0].ResolvedRef != secondCommit {
		t.Fatalf("second ResolvedRef = %q, want %q", testkit.LockedSkills(t, secondLock)[0].ResolvedRef, secondCommit)
	}

	if testkit.LockedSkills(t, secondLock)[0].ContentHash == testkit.LockedSkills(t, firstLock)[0].ContentHash {
		t.Fatalf("ContentHash did not change after root branch moved: %q", testkit.LockedSkills(t, secondLock)[0].ContentHash)
	}
}

func TestRunLockPreservesExistingLockfileOnGitSourceFailure(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	testkit.CommitRepository(t, repoPath, "add oracle skill")

	originalLockfile := []byte("version = 1\n# keep git failure\n")
	if err := os.WriteFile(lockfilePath, originalLockfile, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { git = "`+repoPath+`", path = "skills/missing", ref = "main" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}

	currentLockfile, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if string(currentLockfile) != string(originalLockfile) {
		t.Fatalf("lockfile was replaced on git failure:\n%s", currentLockfile)
	}
}
