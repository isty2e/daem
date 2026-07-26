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

func TestRunAddSkillDryRunPlansGitSkillWithoutWrites(t *testing.T) {
	testkit.RequireGit(t)
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	repoPath := testkit.InitGitRepository(t, tempDir)
	testkit.WriteFile(t, repoPath, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")
	testkit.CommitRepository(t, repoPath, "add oracle skill")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", repoPath,
		"--manifest", manifestPath,
		"--path", "skills/oracle",
		"--ref", "main",
		"--target", "codex",
		"--target", "claude-code",
		"--scope", "global",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"add: skill/oracle",
		"change: append skill resource",
		"lockfile: would write " + filepath.Join(tempDir, "daem.lock.toml"),
		"[[skill]]",
		`source = { git = "` + repoPath + `", path = "skills/oracle", ref = "main" }`,
		`targets = ["codex", "claude-code"]`,
		`scope = "global"`,
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

func TestRunAddSkillDryRunDiffShowsResultingManifestDelta(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)
	testkit.WriteFile(t, filepath.Join(tempDir, "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", filepath.Join(tempDir, "skills", "oracle"),
		"--manifest", manifestPath,
		"--target", "codex",
		"--dry-run",
		"--diff",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"manifest diff:",
		"lockfile: would write " + filepath.Join(tempDir, "daem.lock.toml"),
		"--- " + manifestPath,
		"+++ " + manifestPath,
		"+[[skill]]",
		`+name = "oracle"`,
		`+targets = ["codex"]`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, manifestPath, original)
}

func TestRunAuthoringDiffRequiresDryRun(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "add skill",
			args: []string{"add", "skill", "https://github.com/owner/repo.git", "--path", "skills/oracle", "--ref", "main", "--diff"},
			want: "add failed: --diff requires --dry-run",
		},
		{
			name: "add instruction",
			args: []string{"add", "instruction", "project", "AGENTS.md", "--diff"},
			want: "add failed: --diff requires --dry-run",
		},
		{
			name: "add hook",
			args: []string{"add", "hook", "protect-env", "PreToolUse", "python3 hooks/protect.py", "--diff"},
			want: "add failed: --diff requires --dry-run",
		},
		{
			name: "add skill-group",
			args: []string{"add", "skill-group", "owner/repo", "--member", "oracle", "--ref", "main", "--diff"},
			want: "add failed: --diff requires --dry-run",
		},
		{
			name: "remove skill",
			args: []string{"remove", "skill", "oracle", "--diff"},
			want: "remove failed: --diff requires --dry-run",
		},
		{
			name: "remove instruction",
			args: []string{"remove", "instruction", "project", "--diff"},
			want: "remove failed: --diff requires --dry-run",
		},
		{
			name: "remove hook",
			args: []string{"remove", "hook", "protect-env", "--diff"},
			want: "remove failed: --diff requires --dry-run",
		},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(scenario.args, &stdout, &stderr)
			if exitCode != 2 {
				t.Fatalf("exitCode = %d, want 2", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), scenario.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), scenario.want)
			}
		})
	}
}

func TestRunAddSkillYesAddsLocalSkillAndPreventsStaleLock(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	testkit.WriteFile(t, filepath.Join(tempDir, "skills", "local-review"), "SKILL.md", "---\nname: local-review\ndescription: Local\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", filepath.Join(tempDir, "skills", "local-review"),
		"--manifest", manifestPath,
		"--target", "codex",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "added: skill/local-review") {
		t.Fatalf("stdout = %q, want added resource", stdout.String())
	}
	if strings.Contains(stdout.String(), "warning: local source") {
		t.Fatalf("stdout = %q, want no local source warning for existing source", stdout.String())
	}
	for _, want := range []string{
		"lockfile: wrote " + filepath.Join(tempDir, "daem.lock.toml"),
		"next: run daem apply --manifest " + manifestPath + " --dry-run",
		"note: add updates the manifest and lockfile only; host files are written only by apply",
	} {
		testkit.AssertOutputLine(t, stdout.String(), want)
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Parse returned error: %v\n%s", err, content)
	}
	if len(config.Skills()) != 1 || config.Skills()[0].ID().Name() != "local-review" {
		t.Fatalf("skills = %#v, want local-review", config.Skills())
	}
	locked, err := lockfile.Load(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedSkills(t, locked)) != 1 || testkit.LockedSkills(t, locked)[0].Name != "local-review" {
		t.Fatalf("locked skills = %#v, want local-review", testkit.LockedSkills(t, locked))
	}
	var applyStdout bytes.Buffer
	var applyStderr bytes.Buffer
	applyExitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run"}, &applyStdout, &applyStderr)
	if applyExitCode != 0 {
		t.Fatalf("apply exitCode = %d, stderr = %q, stdout = %q", applyExitCode, applyStderr.String(), applyStdout.String())
	}
	combinedApplyOutput := applyStdout.String() + applyStderr.String()
	if strings.Contains(combinedApplyOutput, "stale_lock") || strings.Contains(combinedApplyOutput, "next: run daem lock") {
		t.Fatalf("apply output = %q, want no stale lock guidance", combinedApplyOutput)
	}
	testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(tempDir, ".daem")))
}

func TestRunAddSkillRejectsRemovedRepairFlagWithoutWrites(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"codex\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)
	testkit.WriteFile(t, filepath.Join(tempDir, "skills", "oracle"), "skill.md", "---\nname: oracle\ndescription: Oracle\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", filepath.Join(tempDir, "skills", "oracle"),
		"--manifest", manifestPath,
		"--target", "codex",
		"--repair-skill-compat",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2; stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -repair-skill-compat") {
		t.Fatalf("stderr = %q, want removed flag diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.d"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".daem"))
	testkit.AssertDirectoryEntryMissingExact(t, filepath.Join(tempDir, "skills", "oracle"), "SKILL.md")
	testkit.AssertFileContent(t, filepath.Join(tempDir, "skills", "oracle", "skill.md"), "---\nname: oracle\ndescription: Oracle\n---\n")
}

func TestRunAddSkillRejectsRepairableIncompatibilityUntilManifestPolicyIsDeclared(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"opencode\"]\n"
	testkit.WriteFile(t, tempDir, "daem.toml", original)
	testkit.WriteFile(t, filepath.Join(tempDir, "skills", "oracle"), "skill.md", "---\ndescription: Oracle\n---\nBody\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "skill", filepath.Join(tempDir, "skills", "oracle"),
		"--manifest", manifestPath,
		"--target", "opencode",
		"--scope", "project",
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"add failed: lock prospective manifest:",
		`validate skill "oracle": skill source "local:skills/oracle?mode=vendor" is missing SKILL.md`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	testkit.AssertFileContent(t, manifestPath, original)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.d"))
	testkit.AssertDirectoryEntryMissingExact(t, filepath.Join(tempDir, "skills", "oracle"), "SKILL.md")
	testkit.AssertFileContent(t, filepath.Join(tempDir, "skills", "oracle", "skill.md"), "---\ndescription: Oracle\n---\nBody\n")
}
