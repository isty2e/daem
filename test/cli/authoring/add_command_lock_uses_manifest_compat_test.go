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

func TestRunLockUsesManifestCompatRepairPolicyWithoutVendoringSource(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\nBody\n")

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
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "wrote lockfile: "+filepath.Join(tempDir, "daem.lock.toml")) {
		t.Fatalf("stdout = %q, want lockfile write summary", stdout.String())
	}

	locked, err := lockfile.Load(t.Context(), filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 1 || testkit.LockedSkills(t, locked)[0].Repair == nil {
		t.Fatalf("locked skills = %#v, want repaired skill entry", testkit.LockedSkills(t, locked))
	}
	if testkit.LockedSkills(t, locked)[0].SourceID != "local:skills/oracle?mode=vendor" {
		t.Fatalf("SourceID = %q, want original source identity", testkit.LockedSkills(t, locked)[0].SourceID)
	}
	if len(testkit.LockedSkills(t, locked)[0].Repair.Operations()) < 2 {
		t.Fatalf("repair operations = %#v, want replayable mechanical repair operations", testkit.LockedSkills(t, locked)[0].Repair.Operations())
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.d"))
	testkit.AssertDirectoryEntryMissingExact(t, filepath.Join(tempDir, "skills", "oracle"), "SKILL.md")
	testkit.AssertFileContent(t, filepath.Join(tempDir, "skills", "oracle", "skill.md"), "---\ndescription: Use for oracle review.\n---\nBody\n")
}

func TestRunAddSkillKeepsCompatibleSourcePathWithoutRepairSurface(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	testkit.WriteFile(t, filepath.Join(tempDir, "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", filepath.Join(tempDir, "skills", "oracle"),
		"--manifest", manifestPath,
		"--target", "codex",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Skills()) != 1 || config.Skills()[0].ID().Name() != "oracle" {
		t.Fatalf("skills = %#v, want oracle", config.Skills())
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.d"))
}

func TestRunAddSkillLeavesNoRepairArtifactsWhenLockPreflightFails(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `version = 1
targets = ["codex"]

[instructions.project]
source = { path = "missing.md", mode = "vendor" }
`
	testkit.WriteFile(t, tempDir, "daem.toml", original)
	testkit.WriteFile(t, filepath.Join(tempDir, "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", filepath.Join(tempDir, "skills", "oracle"),
		"--manifest", manifestPath,
		"--target", "codex",
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stderr.String(), "add failed: lock prospective manifest: resolve instructions \"project\" source") {
		t.Fatalf("stderr = %q, want lock failure", stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.d", "skills", "oracle"))
}

func TestRunAddSkillYesAllowsGitRepositoryRoot(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "SKILL.md", "---\nname: humanizer\ndescription: Humanizer\n---\n")
	commit := testkit.CommitRepository(t, repoPath, "add root skill")
	bareRepoPath := filepath.Join(tempDir, "humanizer.git")
	testkit.RunGit(t, repoPath, "clone", "--bare", repoPath, bareRepoPath)
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", bareRepoPath,
		"--manifest", manifestPath,
		"--ref", "main",
		"--target", "codex",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "added: skill/humanizer") {
		t.Fatalf("stdout = %q, want added root skill", stdout.String())
	}

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Skills()) != 1 || config.Skills()[0].ID().Name() != "humanizer" {
		t.Fatalf("skills = %#v, want root humanizer skill", config.Skills())
	}

	locked, err := lockfile.Load(t.Context(), filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 1 || testkit.LockedSkills(t, locked)[0].Name != "humanizer" {
		t.Fatalf("locked skills = %#v, want humanizer", testkit.LockedSkills(t, locked))
	}
	if testkit.LockedSkills(t, locked)[0].ResolvedRef != commit {
		t.Fatalf("ResolvedRef = %q, want %q", testkit.LockedSkills(t, locked)[0].ResolvedRef, commit)
	}
}

func TestRunAddSkillDryRunFailsForMissingLocalSource(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", filepath.Join(tempDir, "skills", "missing"),
		"--manifest", manifestPath,
		"--target", "codex",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "add failed: lock prospective manifest: resolve skill \"missing\"") {
		t.Fatalf("stderr = %q, want lock preflight diagnostic", stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestRunAddSkillYesFailsForMissingLocalSource(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", filepath.Join(tempDir, "skills", "missing"),
		"--manifest", manifestPath,
		"--target", "codex",
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "add failed: lock prospective manifest: resolve skill \"missing\"") {
		t.Fatalf("stderr = %q, want lock preflight diagnostic", stderr.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestRunAddSkillUsesUserDefaultManifestWhenManifestFlagOmittedAndScopeIsGlobal(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	configHome := filepath.Join(tempDir, "config")
	testkit.WithWorkingDirectory(t, tempDir)
	manifestPath := filepath.Join(configHome, "daem", "daem.toml")
	testkit.WriteFile(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), "version = 1\ntargets = [\"codex\"]\n")
	testkit.WriteFile(t, filepath.Join(filepath.Dir(manifestPath), "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", filepath.Join(filepath.Dir(manifestPath), "skills", "oracle"),
		"--target", "codex",
		"--scope", "global",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "manifest: "+manifestPath) {
		t.Fatalf("stdout = %q, want default manifest path", stdout.String())
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Skills()) != 1 || config.Skills()[0].ID().Name() != "oracle" {
		t.Fatalf("skills = %#v, want oracle", config.Skills())
	}
	if string(config.Skills()[0].Scope()) != "global" {
		t.Fatalf("skill scope = %q, want global", config.Skills()[0].Scope())
	}
	if _, err := lockfile.Load(t.Context(), filepath.Join(configHome, "daem", "daem.lock.toml")); err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
}

func TestRunAddSkillRejectsProjectScopeFromUserDefaultManifest(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, tempDir)
	configHome := filepath.Join(tempDir, "config")
	testkit.WithWorkingDirectory(t, tempDir)
	manifestPath := filepath.Join(configHome, "daem", "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), original)
	testkit.WriteFile(t, filepath.Join(filepath.Dir(manifestPath), "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", filepath.Join(filepath.Dir(manifestPath), "skills", "oracle"),
		"--target", "codex",
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"add failed: lock prospective manifest",
		"project-scoped skill \"oracle\" requires a project manifest",
		"use --manifest ./daem.toml or set scope = \"global\"",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(configHome, "daem", "daem.lock.toml"))
}
