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

func TestRunLockUpdatesHashWhenSkillContentChanges(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	skillPath := filepath.Join(tempDir, "skills/demo/SKILL.md")
	testkit.WriteFile(t, tempDir, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "demo"
source = { path = "skills/demo", mode = "vendor" }
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

	if err := os.WriteFile(skillPath, []byte("---\nname: demo\ndescription: demo\n---\nextra\n"), 0o600); err != nil {
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

	firstHash := testkit.LockedSkills(t, firstLock)[0].ContentHash
	secondHash := testkit.LockedSkills(t, secondLock)[0].ContentHash
	if firstHash == secondHash {
		t.Fatalf("hash did not change after skill content update: %q", firstHash)
	}
}

func TestRunLockDerivesLockfileBesideNestedManifest(t *testing.T) {
	tempDir := t.TempDir()
	workspaceDir := filepath.Join(tempDir, "nested")
	manifestPath := filepath.Join(workspaceDir, "daem.toml")
	lockfilePath := filepath.Join(workspaceDir, "daem.lock.toml")
	testkit.WriteFile(t, workspaceDir, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "demo"
source = { path = "skills/demo", mode = "vendor" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	if _, err := lockfile.Load(lockfilePath); err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	lockfileDirectoryEntries, err := os.ReadDir(filepath.Dir(lockfilePath))
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}

	for _, entry := range lockfileDirectoryEntries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatalf("temporary lockfile was left behind: %s", entry.Name())
		}
	}
}

func TestRunLockHandlesPathsWithSpaces(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "project with spaces")
	manifestPath := filepath.Join(projectDir, "daem.toml")
	lockfilePath := filepath.Join(projectDir, "daem.lock.toml")
	testkit.WriteFile(t, projectDir, "skills/my skill/SKILL.md", "---\nname: my-skill\ndescription: my skill\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "my-skill"
source = { path = "skills/my skill", mode = "vendor" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	if len(testkit.LockedSkills(t, locked)) != 1 {
		t.Fatalf("len(locked skills) = %d, want 1", len(testkit.LockedSkills(t, locked)))
	}

	if testkit.LockedSkills(t, locked)[0].SourceID != "local:skills/my skill?mode=vendor" {
		t.Fatalf("SourceID = %q", testkit.LockedSkills(t, locked)[0].SourceID)
	}
}

func TestRunLockRelativeManifestWritesLockfileNextToManifest(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "project")
	manifestPath := filepath.Join(projectDir, "daem.toml")
	testkit.WriteFile(t, projectDir, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "demo"
source = { path = "skills/demo", mode = "vendor" }
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalWorkingDirectory); err != nil {
			t.Fatalf("Chdir returned error: %v", err)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", filepath.Join("project", "daem.toml")}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	if _, err := os.Stat(filepath.Join(projectDir, "daem.lock.toml")); err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
}

func TestRunLockWithNoLockableResourcesWritesStableEmptyLockfile(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
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

	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	firstContent, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("second exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	secondContent, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if string(firstContent) != string(secondContent) {
		t.Fatalf("empty lockfile changed across identical runs:\nfirst:\n%s\nsecond:\n%s", firstContent, secondContent)
	}
}

func TestRunLockClearsExistingLockfileWhenManifestHasNoLockableResources(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplySkill, Name: "stale-skill", SourceID: "local:skills/stale?mode=vendor", ContentHash: testkit.FixtureHash("sha256:stale-skill")}, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "stale-instructions", SourceID: "local:instructions/stale.md?mode=vendor", ContentHash: testkit.FixtureHash("sha256:stale-instructions")}))

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
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

	locked, err := lockfile.Load(lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 0 || len(testkit.LockedHooks(t, locked)) != 0 || len(testkit.LockedInstructions(t, locked)) != 0 {
		t.Fatalf("locked resources = %#v, want no stale entries", locked.Locked)
	}

	content, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if strings.Contains(string(content), "stale-") {
		t.Fatalf("lockfile retained stale entries:\n%s", content)
	}
}
