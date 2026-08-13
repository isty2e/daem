package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockWritesStableLockfileNextToManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/zeta/SKILL.md", "---\nname: zeta\ndescription: zeta\n---\n")
	testkit.WriteFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "zeta"
source = { path = "skills/zeta", mode = "vendor" }

[[skill]]
name = "alpha"
source = { path = "skills/alpha", mode = "vendor" }

[[hook]]
name = "inline"
event = "SessionStart"
command = "bd prime"

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
		t.Fatalf("lockfile changed across identical runs:\nfirst:\n%s\nsecond:\n%s", firstContent, secondContent)
	}

	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	if len(testkit.LockedSkills(t, locked)) != 2 {
		t.Fatalf("len(locked skills) = %d, want 2", len(testkit.LockedSkills(t, locked)))
	}

	if testkit.LockedSkills(t, locked)[0].Name != "alpha" || testkit.LockedSkills(t, locked)[1].Name != "zeta" {
		t.Fatalf("locked skills not sorted by name: %#v", testkit.LockedSkills(t, locked))
	}

	if len(testkit.LockedHooks(t, locked)) != 0 {
		t.Fatalf("locked hooks = %#v, want none", testkit.LockedHooks(t, locked))
	}

	if strings.Contains(string(firstContent), "generated_at") {
		t.Fatalf("lockfile content = %q, want no generated_at churn", firstContent)
	}

	if strings.Contains(string(firstContent), "hook = []") {
		t.Fatalf("lockfile content = %q, want empty hook array omitted", firstContent)
	}

	if strings.Contains(string(firstContent), "resolved_ref") {
		t.Fatalf("lockfile content = %q, want local resources without resolved_ref", firstContent)
	}

	lockfileInfo, err := os.Stat(lockfilePath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}

	if lockfileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("lockfile mode = %s, want 0600", lockfileInfo.Mode().Perm())
	}
}

func TestRunLockDryRunReportsCompatRepairedSkill(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
compat_repair = true
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "  - resource/skill/oracle (repaired)") {
		t.Fatalf("stdout = %q, want repaired lock entry", stdout.String())
	}

	if _, err := os.Stat(filepath.Join(tempDir, "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote lockfile or stat failed unexpectedly: %v", err)
	}
	testkit.AssertDirectoryEntryMissingExact(t, filepath.Join(tempDir, "skills/oracle"), "SKILL.md")
	testkit.AssertDirectoryEntryExistsExact(t, filepath.Join(tempDir, "skills/oracle"), "skill.md")
}

func TestRunLockDryRunAcceptsCompatibleSkillWithCompatRepair(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	skillPath := filepath.Join(tempDir, "skills/ast-grep/SKILL.md")
	testkit.WriteFile(
		t,
		tempDir,
		"skills/ast-grep/SKILL.md",
		"---\nname: ast-grep\ndescription: Use for structural code search.\n---\n",
	)
	originalContent, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	manifest := fmt.Sprintf(`
version = 1
targets = ["codex", "claude-code", "opencode", "pi", "antigravity-cli"]

[[skill]]
name = "ast-grep"
source = { path = %q, mode = "vendor" }
scope = "global"
compat_repair = true
`, filepath.Dir(skillPath))
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "resource/skill/ast-grep") {
		t.Fatalf("stdout = %q, want compatible skill lock entry", stdout.String())
	}
	if strings.Contains(stdout.String(), "(repaired)") {
		t.Fatalf("stdout = %q, compatible skill must not be reported as repaired", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote lockfile or stat failed unexpectedly: %v", err)
	}
	currentContent, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(currentContent, originalContent) {
		t.Fatal("dry-run mutated the compatible skill source")
	}
}

func TestRunLockWritesCompatRepairRecipe(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
compat_repair = true
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
	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 1 || testkit.LockedSkills(t, locked)[0].Repair == nil {
		t.Fatalf("locked skills = %#v, want repaired skill entry", testkit.LockedSkills(t, locked))
	}
	if testkit.LockedSkills(t, locked)[0].SourceID != "local:skills/oracle?mode=vendor" {
		t.Fatalf("SourceID = %q, want original source identity", testkit.LockedSkills(t, locked)[0].SourceID)
	}
	testkit.AssertDirectoryEntryMissingExact(t, filepath.Join(tempDir, "skills/oracle"), "SKILL.md")
	testkit.AssertDirectoryEntryExistsExact(t, filepath.Join(tempDir, "skills/oracle"), "skill.md")

	content, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	for _, fragment := range []string{
		"[locked.subject.repair_recipe]",
		"[[locked.subject.repair_recipe.operation]]",
		`kind = "rename"`,
		`kind = "set_frontmatter_string"`,
	} {
		if !strings.Contains(string(content), fragment) {
			t.Fatalf("lockfile content = %s, missing %q", content, fragment)
		}
	}
}

func TestRunLockExpandsSkillGroupsIntoPerSkillLockEntries(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/foo/SKILL.md", "---\nname: foo\ndescription: foo\n---\n")
	testkit.WriteFile(t, tempDir, "skills/bar/SKILL.md", "---\nname: bar\ndescription: bar\n---\n")

	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[[skill_group]]
names = ["foo", "bar"]
source = { path = "skills", mode = "vendor" }
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
	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}

	if len(testkit.LockedSkills(t, locked)) != 2 {
		t.Fatalf("len(locked skills) = %d, want 2", len(testkit.LockedSkills(t, locked)))
	}
	if testkit.LockedSkills(t, locked)[0].Name != "bar" || testkit.LockedSkills(t, locked)[1].Name != "foo" {
		t.Fatalf("locked skills = %#v, want bar and foo entries", testkit.LockedSkills(t, locked))
	}
	if testkit.LockedSkills(t, locked)[0].SourceID != "local:skills/bar?mode=vendor" {
		t.Fatalf("bar SourceID = %q", testkit.LockedSkills(t, locked)[0].SourceID)
	}
	if testkit.LockedSkills(t, locked)[1].SourceID != "local:skills/foo?mode=vendor" {
		t.Fatalf("foo SourceID = %q", testkit.LockedSkills(t, locked)[1].SourceID)
	}

	content, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if strings.Contains(string(content), "skill_group") {
		t.Fatalf("lockfile content = %q, want no skill_group metadata", content)
	}
}

func TestRunLockExpandsSkillGroupSelectorsIntoSortedLockEntries(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: alpha\n---\n")
	testkit.WriteFile(t, tempDir, "skills/beta-tool/SKILL.md", "---\nname: beta-tool\ndescription: beta\n---\n")
	testkit.WriteFile(t, tempDir, "skills/zeta/SKILL.md", "---\nname: zeta\ndescription: zeta\n---\n")

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["regex:^beta-tool$", "glob:alpha"]
targets = ["codex"]
`)

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
	if testkit.LockedSkills(t, locked)[0].Name != "alpha" || testkit.LockedSkills(t, locked)[1].Name != "beta-tool" {
		t.Fatalf("locked skills = %#v, want deterministic alpha, beta-tool order", testkit.LockedSkills(t, locked))
	}
}
