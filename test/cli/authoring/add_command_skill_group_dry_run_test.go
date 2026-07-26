package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunAddSkillGroupDryRunPlansGitGroupWithoutWrites(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, filepath.Join(repoPath, "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")
	testkit.WriteFile(t, filepath.Join(repoPath, "skills", "lean-ontology"), "SKILL.md", "---\nname: lean-ontology\ndescription: Lean ontology\n---\n")
	testkit.CommitRepository(t, repoPath, "add skill group skills")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill-group", repoPath,
		"--manifest", manifestPath,
		"--path", "skills",
		"--ref", "main",
		"--member", "oracle",
		"--member", "lean-ontology",
		"--target", "codex",
		"--target", "claude-code",
		"--scope", "global",
		"--dry-run",
		"--diff",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"add: skill_group/oracle,lean-ontology",
		"change: append skill_group resource",
		"lockfile: would write " + filepath.Join(tempDir, "daem.lock.toml"),
		"[[skill_group]]",
		`names = ["oracle", "lean-ontology"]`,
		`source = { git = "` + repoPath + `", path = "skills", ref = "main" }`,
		`targets = ["codex", "claude-code"]`,
		`scope = "global"`,
		"manifest diff:",
		`+[[skill_group]]`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	for _, want := range []string{
		"next: rerun this authoring command without --dry-run",
		"note: add updates the manifest and lockfile only; host files are written only by apply",
	} {
		testkit.AssertOutputLine(t, stdout.String(), want)
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestRunAddSkillGroupYesAllowsGitRepositoryRoot(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, filepath.Join(repoPath, "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")
	testkit.CommitRepository(t, repoPath, "add root skill group member")
	bareRepoPath := filepath.Join(tempDir, "repo.git")
	testkit.RunGit(t, repoPath, "clone", "--bare", repoPath, bareRepoPath)
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill-group", bareRepoPath,
		"--manifest", manifestPath,
		"--ref", "main",
		"--member", "oracle",
		"--target", "codex",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "added: skill_group/oracle") {
		t.Fatalf("stdout = %q, want added skill_group", stdout.String())
	}

	locked, err := lockfile.Load(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 1 || testkit.LockedSkills(t, locked)[0].Name != "oracle" {
		t.Fatalf("locked skills = %#v, want oracle", testkit.LockedSkills(t, locked))
	}
}

func TestRunAddSkillGroupYesAddsLocalGroupToManifestOnly(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	skillRoot := filepath.Join(tempDir, "skills")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	testkit.WriteFile(t, filepath.Join(skillRoot, "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")
	testkit.WriteFile(t, filepath.Join(skillRoot, "review"), "SKILL.md", "---\nname: review\ndescription: Review\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill-group", skillRoot,
		"--manifest", manifestPath,
		"--member", "oracle",
		"--member", "review",
		"--target", "codex",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "added: skill_group/oracle,review") {
		t.Fatalf("stdout = %q, want added skill_group", stdout.String())
	}
	if strings.Contains(stdout.String(), "warning: local source root") {
		t.Fatalf("stdout = %q, want no local source warning for existing root", stdout.String())
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Skills()) != 2 {
		t.Fatalf("skills = %#v, want two skills", config.Skills())
	}
	if config.Skills()[0].ID().Name() != "oracle" || config.Skills()[1].ID().Name() != "review" {
		t.Fatalf("skills = %#v, want oracle then review", config.Skills())
	}
	locked, err := lockfile.Load(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 2 {
		t.Fatalf("locked skills = %#v, want two skills", testkit.LockedSkills(t, locked))
	}
	testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(tempDir, ".daem")))
}

func TestRunAddSkillGroupRejectsRemovedModeFlag(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	skillRoot := filepath.Join(tempDir, "skills")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	testkit.WriteFile(t, filepath.Join(skillRoot, "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill-group", skillRoot,
		"--manifest", manifestPath,
		"--member", "oracle",
		"--mode", "link",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2; stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -mode") {
		t.Fatalf("stderr = %q, want removed mode diagnostic", stderr.String())
	}
}

func TestRunAddSkillGroupRequiresExplicitMember(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill-group", "owner/repo",
		"--manifest", manifestPath,
		"--ref", "main",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "at least one --member is required") {
		t.Fatalf("stderr = %q, want missing member diagnostic", stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
}

func TestRunAddSkillGroupCollapsesDuplicateMembers(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	skillRoot := filepath.Join(tempDir, "skills")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	testkit.WriteFile(t, filepath.Join(skillRoot, "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")
	testkit.WriteFile(t, filepath.Join(skillRoot, "review"), "SKILL.md", "---\nname: review\ndescription: Review\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill-group", skillRoot,
		"--manifest", manifestPath,
		"--member", "oracle",
		"--member", "review",
		"--member", "oracle",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `names = ["oracle", "review"]`) {
		t.Fatalf("stdout = %q, want stable deduplicated members", stdout.String())
	}
}

func TestRunAddSkillGroupTreatsCommaMemberAsOneToken(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	skillRoot := filepath.Join(tempDir, "skills")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)
	testkit.WriteFile(t, filepath.Join(skillRoot, "oracle,review"), "SKILL.md", "---\nname: oracle-review\ndescription: Combined\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill-group", skillRoot,
		"--manifest", manifestPath,
		"--member", "oracle,review",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `names = ["oracle,review"]`) {
		t.Fatalf("stdout = %q, want one unexpanded member", stdout.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
}

func TestRunAddSkillGroupReportsManifestConflictsBeforeWriting(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { git = "https://github.com/owner/first.git", path = "skills/oracle", ref = "main" }
targets = ["codex"]
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill-group", "https://github.com/owner/second.git",
		"--manifest", manifestPath,
		"--path", "skills",
		"--ref", "main",
		"--member", "oracle",
		"--target", "codex",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), `duplicate skill id "oracle"`) {
		t.Fatalf("stderr = %q, want duplicate skill diagnostic", stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
}
