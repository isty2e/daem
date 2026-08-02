package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunImportConflictsFailBeforeWrites(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, root string, outputPath string)
		want  string
	}{
		{
			name: "output exists",
			setup: func(t *testing.T, root string, outputPath string) {
				testkit.WriteFile(t, root, "daem.imported.toml", "existing\n")
			},
			want: "output manifest already exists",
		},
		{
			name: "source exists",
			setup: func(t *testing.T, root string, outputPath string) {
				testkit.WriteFile(t, root, filepath.Join("daem.imported.d", "instructions", "codex-project.md"), "existing\n")
			},
			want: "imported source already exists",
		},
		{
			name: "live path is directory",
			setup: func(t *testing.T, root string, outputPath string) {
				if err := os.Remove(filepath.Join(root, "AGENTS.md")); err != nil {
					t.Fatalf("Remove returned error: %v", err)
				}
				if err := os.Mkdir(filepath.Join(root, "AGENTS.md"), 0o700); err != nil {
					t.Fatalf("Mkdir returned error: %v", err)
				}
			},
			want: "AGENTS.md: instruction_not_regular_file",
		},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			tempDir := t.TempDir()
			testkit.WithWorkingDirectory(t, tempDir)
			testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
			outputPath := filepath.Join(tempDir, "daem.imported.toml")
			scenario.setup(t, tempDir, outputPath)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath}, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), scenario.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), scenario.want)
			}
			if scenario.name != "output exists" {
				testkit.AssertPathMissing(t, outputPath)
			}
		})
	}
}

func TestRunImportMissingInputFailsWithoutWrites(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		"nothing to import",
		"AGENTS.md",
		"next: verify that the selected --target and --scope have live agent files to import",
		"next: try another selection, such as --scope global or a different --target",
		"next: choose the destination with --manifest <path>, or add --merge when importing into an existing manifest",
		"next: if there is no live config to import, initialize the selected manifest and add resources explicitly",
		"next: run " + testkit.ExpectedShellCommand(t, "daem", "init", "--manifest", outputPath, "--dry-run"),
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
}

func TestRunImportMergeMissingInputSuggestsExistingManifestDestination(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--merge", "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		"nothing to import",
		"next: confirm --manifest points to the existing manifest you want to merge into",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunImportWriteMergeConflictReportsDryRunRemediation(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.codex_project]
source = "instructions/other.md"
targets = ["codex"]
scope = "project"
`)
	original, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--merge"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty before human conflict rendering", stdout.String())
	}
	if want := "run daem import --merge --dry-run to inspect conflicts"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
	testkit.AssertFileContent(t, outputPath, string(original))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.d"))
}

func TestRunImportEmptyHookConfigFailsWithoutWrites(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, ".codex/hooks.json", `{"hooks":{}}`+"\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "hooks_empty") {
		t.Fatalf("stderr = %q, want hooks_empty skip summary", stderr.String())
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportDeduplicatesRepeatedTarget(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--target", "codex", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Count(stdout.String(), `resource="instructions/codex_project"`) != 1 {
		t.Fatalf("stdout = %q, want one imported resource", stdout.String())
	}
}

func TestRunImportCopiesInstructionBytesExactly(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	liveContent := []byte{0xEF, 0xBB, 0xBF, 'a', '\r', '\n', 0x00, 0xFF}
	if err := os.WriteFile(filepath.Join(tempDir, "AGENTS.md"), liveContent, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	sourcePath := filepath.Join(tempDir, "daem.imported.d", "instructions", "codex-project.md")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}

	importedContent, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(importedContent, liveContent) {
		t.Fatalf("importedContent = %#v, want %#v", importedContent, liveContent)
	}
}

func TestRunImportYesVendorsCodexGlobalSymlinkedSkill(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	sharedSkill := filepath.Join(homeDir, ".agent", "skills", "shared", "review")
	testkit.WriteFile(t, sharedSkill, "SKILL.md", "---\nname: review\ndescription: Review code\n---\n")
	testkit.WriteFile(t, sharedSkill, filepath.Join("scripts", "run.sh"), "#!/bin/sh\n")
	codexSkillRoot := filepath.Join(homeDir, ".codex", "skills")
	if err := os.MkdirAll(codexSkillRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.Symlink(sharedSkill, filepath.Join(codexSkillRoot, "review")); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	copiedSkill := filepath.Join(tempDir, "daem.imported.d", "skills", "review", strings.ReplaceAll(testkit.HashDirectory(t, sharedSkill), ":", "-"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"imported: 1 resources",
		`resource="skill/review"`,
		`target=codex`,
		`scope=global`,
		`source="` + filepath.ToSlash(copiedSkill) + `"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}

	info, err := os.Lstat(copiedSkill)
	if err != nil {
		t.Fatalf("Lstat copied skill returned error: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("copied skill is a symlink, want real directory")
	}
	testkit.AssertFileContent(t, filepath.Join(copiedSkill, "SKILL.md"), "---\nname: review\ndescription: Review code\n---\n")
	testkit.AssertFileContent(t, filepath.Join(copiedSkill, "scripts", "run.sh"), "#!/bin/sh\n")

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	if len(config.Skills()) != 1 {
		t.Fatalf("skills = %#v", config.Skills())
	}
	skill := config.Skills()[0]
	if skill.ID().Name() != "review" || skill.Scope() != target.ScopeGlobal || skill.InstallMode() != desiredskill.InstallModeCopy {
		t.Fatalf("skill = %#v", skill)
	}
	assertSkillLocalSource(t, skill, filepath.ToSlash(copiedSkill))
	targets := skill.Targets()
	if len(targets) != 1 || targets[0] != target.TargetCodex {
		t.Fatalf("Targets = %#v, want codex", targets)
	}

	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	lockExitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", outputPath, "--dry-run"}, &lockStdout, &lockStderr)
	if lockExitCode != 0 {
		t.Fatalf("lock exitCode = %d, stderr = %q, stdout = %q", lockExitCode, lockStderr.String(), lockStdout.String())
	}
}
