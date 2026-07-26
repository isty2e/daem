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

func TestRunLockRejectsSkillGroupSelectorZeroMatches(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:missing-*"]
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "matched no skill directories") {
		t.Fatalf("stderr = %q, want zero-match diagnostic", stderr.String())
	}
}

func TestRunLockRejectsSkillGroupSelectorDestinationConflict(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	testkit.WriteFile(t, tempDir, "other/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha duplicate\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
id = "manual-alpha"
name = "alpha"
source = { path = "other/alpha", mode = "vendor" }
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:alpha"]
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "duplicate skill destination name") {
		t.Fatalf("stderr = %q, want destination conflict diagnostic", stderr.String())
	}
}

func TestRunLockRefreshesSelectorMembershipAndOutdatedDoesNotWrite(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("initial lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	assertLockedSkillNames(t, lockfilePath, []string{"alpha"})

	testkit.WriteFile(t, tempDir, "skills/beta/SKILL.md", "---\nname: beta\ndescription: beta\n---\n")
	beforeAddBytes, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"outdated", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("outdated exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "lockfile changes: added=2 changed=0 removed=0 unchanged=2") {
		t.Fatalf("stdout = %q, want selector child add delta", stdout.String())
	}
	assertFileBytes(t, lockfilePath, beforeAddBytes)

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("dry-run lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "lockfile changes: added=2 changed=0 removed=0 unchanged=2") {
		t.Fatalf("stdout = %q, want dry-run selector child add delta", stdout.String())
	}
	assertFileBytes(t, lockfilePath, beforeAddBytes)

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("refresh lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	assertLockedSkillNames(t, lockfilePath, []string{"alpha", "beta"})

	if err := os.RemoveAll(filepath.Join(tempDir, "skills", "alpha")); err != nil {
		t.Fatalf("RemoveAll returned error: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("remove refresh lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	assertLockedSkillNames(t, lockfilePath, []string{"beta"})
	if strings.Contains(stdout.String(), "alpha") && !strings.Contains(stdout.String(), "removed=2") {
		t.Fatalf("stdout = %q, want removed selector child reported as delta", stdout.String())
	}
}

func TestRunLockRefreshesChangedSelectorChildHash(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["regex:^alpha$"]
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("initial lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	initialHash := lockedSkillHash(t, lockfilePath, "alpha")
	testkit.WriteFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: changed\n---\n")

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"outdated", "--manifest", manifestPath, "--check"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("outdated --check exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "lockfile changes: added=0 changed=1 removed=0 unchanged=1") {
		t.Fatalf("stdout = %q, want changed selector child delta", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("refresh lock exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if refreshedHash := lockedSkillHash(t, lockfilePath, "alpha"); refreshedHash == initialHash {
		t.Fatalf("selector child hash stayed %q after content change", refreshedHash)
	}
}

func TestRunLockRemovesLocalSkillEntriesRemovedFromManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	testkit.WriteFile(t, tempDir, "skills/zeta/SKILL.md", "---\nname: zeta\ndescription: zeta\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }

[[skill]]
name = "zeta"
source = { path = "skills/zeta", mode = "vendor" }

[[hook]]
name = "protect-env"
event = "PreToolUse"
command = "python3 hooks/protect_env.py"
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("second exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	if len(testkit.LockedSkills(t, locked)) != 1 || testkit.LockedSkills(t, locked)[0].Name != "alpha" {
		t.Fatalf("locked skills = %#v, want alpha only", testkit.LockedSkills(t, locked))
	}
	if len(testkit.LockedHooks(t, locked)) != 0 {
		t.Fatalf("locked hooks = %#v, want none", testkit.LockedHooks(t, locked))
	}

	content, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if strings.Contains(string(content), "zeta") || strings.Contains(string(content), "protect-env") {
		t.Fatalf("lockfile retained removed manifest entries:\n%s", content)
	}
}

func assertLockedSkillNames(t *testing.T, lockfilePath string, want []string) {
	t.Helper()
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != len(want) {
		t.Fatalf("locked skills = %#v, want names %#v", testkit.LockedSkills(t, locked), want)
	}
	for index, expected := range want {
		if testkit.LockedSkills(t, locked)[index].Name != expected {
			t.Fatalf("locked skills = %#v, want names %#v", testkit.LockedSkills(t, locked), want)
		}
	}
}

func lockedSkillHash(t *testing.T, lockfilePath string, name string) string {
	t.Helper()
	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	for _, lockedSkill := range testkit.LockedSkills(t, locked) {
		if lockedSkill.Name == name {
			return lockedSkill.ContentHash
		}
	}
	t.Fatalf("locked skill %q not found in %#v", name, testkit.LockedSkills(t, locked))
	return ""
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s bytes changed unexpectedly", path)
	}
}

func TestRunLockRemovesInstructionEntryRemovedFromManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "instructions/keep.md", "keep\n")
	testkit.WriteFile(t, tempDir, "instructions/drop.md", "drop\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex", "claude-code"]

[instructions.keep]
source = "instructions/keep.md"
targets = ["codex"]

[instructions.drop]
source = "instructions/drop.md"
targets = ["claude-code"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	firstLock, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedInstructions(t, firstLock)) != 2 {
		t.Fatalf("initial locked instructions = %#v, want keep and drop", testkit.LockedInstructions(t, firstLock))
	}

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.keep]
source = "instructions/keep.md"
targets = ["codex"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("second exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	secondLock, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedInstructions(t, secondLock)) != 1 || testkit.LockedInstructions(t, secondLock)[0].Name != "keep" {
		t.Fatalf("locked instructions after relock = %#v, want keep only", testkit.LockedInstructions(t, secondLock))
	}

	content, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if strings.Contains(string(content), "drop") {
		t.Fatalf("lockfile retained removed instructions entry:\n%s", content)
	}
}

func TestRunLockRemovesGitSkillEntryRemovedFromManifest(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	testkit.WriteFile(t, repoPath, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	testkit.CommitRepository(t, repoPath, "add oracle skill")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }

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

	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	firstLock, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, firstLock)) != 2 {
		t.Fatalf("initial locked skills = %#v, want alpha and oracle", testkit.LockedSkills(t, firstLock))
	}

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("second exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	secondLock, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, secondLock)) != 1 || testkit.LockedSkills(t, secondLock)[0].Name != "alpha" {
		t.Fatalf("locked skills after relock = %#v, want alpha only", testkit.LockedSkills(t, secondLock))
	}

	content, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if strings.Contains(string(content), "oracle") || strings.Contains(string(content), "git:") {
		t.Fatalf("lockfile retained removed git skill entry:\n%s", content)
	}
}
