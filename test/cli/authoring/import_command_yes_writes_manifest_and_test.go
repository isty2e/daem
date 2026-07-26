package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunImportYesWritesManifestAndSourceOnly(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	sourcePath := filepath.Join(tempDir, "daem.imported.d", "instructions", "codex-project.md")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"imported: 1 resources",
		"manifest: " + outputPath,
		"source-dir: " + filepath.Join(tempDir, "daem.imported.d"),
		"next: run daem lock --manifest " + outputPath + " --dry-run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "project instructions\n")
	testkit.AssertFileContent(t, sourcePath, "project instructions\n")
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".daem"))

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	if len(config.Instructions()) != 1 {
		t.Fatalf("instructions = %#v", config.Instructions())
	}
	instructions := config.Instructions()[0]
	if instructions.ID().Name() != "codex_project" {
		t.Fatalf("instructions = %#v", instructions)
	}
	assertInstructionLocalSource(t, instructions, filepath.ToSlash(sourcePath))
}

func TestRunImportYesWritesAntigravityAlternateInstructionRenderTo(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "GEMINI.md", "gemini guidance\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	sourcePath := filepath.Join(tempDir, "daem.imported.d", "instructions", "antigravity-cli-project-gemini.md")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "antigravity-cli", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`resource="instructions/antigravity_cli_project_gemini"`,
		`target=antigravity-cli`,
		`source="` + filepath.ToSlash(sourcePath) + `"`,
		`live="GEMINI.md"`,
		`render_to="GEMINI.md"`,
		`skip live="AGENTS.md" reason=missing`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, sourcePath, "gemini guidance\n")

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	for _, want := range []string{
		`[instructions.antigravity_cli_project_gemini]`,
		`targets = ["antigravity-cli"]`,
		`[instructions.antigravity_cli_project_gemini.target.antigravity-cli]`,
		`render_to = "GEMINI.md"`,
	} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("manifest = %s, want %q", content, want)
		}
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	instruction := findImportedInstruction(t, config.Instructions(), "antigravity_cli_project_gemini")
	targets := instruction.Targets()
	renderings := instruction.Renderings()
	if len(targets) != 1 {
		t.Fatalf("instruction targets = %#v, want one target", targets)
	}
	rendering, ok := renderings[targets[0]]
	if !ok || rendering.RenderTo() != "GEMINI.md" {
		t.Fatalf("instruction renderings = %#v, want antigravity GEMINI.md rendering", renderings)
	}
}

func TestRunImportYesWritesAntigravityGlobalInstruction(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, filepath.Join(homeDir, ".gemini"), "GEMINI.md", "global antigravity guidance\n")
	testkit.WriteFile(t, filepath.Join(homeDir, ".gemini", "config"), "AGENTS.md", "poison config agents\n")
	testkit.WriteFile(t, filepath.Join(homeDir, ".gemini", "config"), "GEMINI.md", "poison config gemini\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	sourcePath := filepath.Join(tempDir, "daem.imported.d", "instructions", "antigravity-cli-global.md")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "antigravity-cli", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		`summary target=antigravity-cli scope=global instructions=1`,
		`resource="instructions/antigravity_cli_global"`,
		`target=antigravity-cli`,
		`scope=global`,
		`source="` + filepath.ToSlash(sourcePath) + `"`,
		`live="` + filepath.Join(homeDir, ".gemini", "GEMINI.md") + `"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	for _, unwanted := range []string{
		`render_to=`,
		`config/AGENTS.md`,
		`config/GEMINI.md`,
	} {
		if strings.Contains(stdout.String(), unwanted) {
			t.Fatalf("stdout = %q, want no %q", stdout.String(), unwanted)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".gemini", "GEMINI.md"), "global antigravity guidance\n")
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".gemini", "config", "AGENTS.md"), "poison config agents\n")
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".gemini", "config", "GEMINI.md"), "poison config gemini\n")
	testkit.AssertFileContent(t, sourcePath, "global antigravity guidance\n")
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".daem"))

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	for _, want := range []string{
		`[instructions.antigravity_cli_global]`,
		`source = "` + filepath.ToSlash(sourcePath) + `"`,
		`targets = ["antigravity-cli"]`,
		`scope = "global"`,
	} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("manifest = %s, want %q", content, want)
		}
	}
	if strings.Contains(string(content), `render_to`) {
		t.Fatalf("manifest = %s, want no render_to for default global placement", content)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	instruction := findImportedInstruction(t, config.Instructions(), "antigravity_cli_global")
	if instruction.Scope() != "global" {
		t.Fatalf("instruction scope = %q, want global", instruction.Scope())
	}
	assertInstructionLocalSource(t, instruction, filepath.ToSlash(sourcePath))
	if renderings := instruction.Renderings(); len(renderings) != 0 {
		t.Fatalf("instruction renderings = %#v, want none for default global placement", renderings)
	}
}

func TestRunImportDefaultsManifestAndSourceDirToWorkingDirectory(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	workingDir, err := filepath.EvalSymlinks(tempDir)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	outputPath := filepath.Join(workingDir, "daem.toml")
	sourcePath := filepath.Join(workingDir, "daem.d", "instructions", "codex-project.md")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"imported: 1 resources",
		"manifest: " + outputPath,
		"source-dir: " + filepath.Join(workingDir, "daem.d"),
		`source="` + filepath.ToSlash(sourcePath) + `"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, sourcePath, "project instructions\n")

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	instructions := findImportedInstruction(t, config.Instructions(), "codex_project")
	assertInstructionLocalSource(t, instructions, filepath.ToSlash(sourcePath))
}

func TestRunImportDefaultOutputConflictFailsWithoutWrites(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	testkit.WriteFile(t, tempDir, "daem.toml", "existing\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "output manifest already exists") {
		t.Fatalf("stderr = %q, want output conflict", stderr.String())
	}
}

func TestRunImportMergeYesUpdatesExistingInstructionTargets(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.toml")
	sourcePath := filepath.Join(tempDir, "daem.d", "instructions", "codex-project.md")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	testkit.WriteFile(t, filepath.Dir(sourcePath), filepath.Base(sourcePath), "project instructions\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["claude-code"]

[instructions.codex_project]
source = "`+filepath.ToSlash(sourcePath)+`"
targets = ["claude-code"]
scope = "project"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--merge"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`merge resource="instructions/codex_project" status=merge_targets`,
		`detail="add targets codex to existing resource"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(content), `targets = ["claude-code", "codex"]`) {
		t.Fatalf("manifest = %s, want merged instruction targets", content)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("merged manifest did not parse: %v\n%s", err, content)
	}
	instruction := findImportedInstruction(t, config.Instructions(), "codex_project")
	if targets := instruction.Targets(); len(targets) != 2 {
		t.Fatalf("targets = %#v, want two targets", targets)
	}
}

func TestRunImportMergeYesUpdatesExistingSkillTargets(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	liveSkill := filepath.Join(homeDir, ".codex", "skills", "review")
	testkit.WriteFile(t, liveSkill, "SKILL.md", "---\nname: review\ndescription: Review\n---\n")
	copiedSkill := filepath.Join(tempDir, "daem.d", "skills", "review", strings.ReplaceAll(testkit.HashDirectory(t, liveSkill), ":", "-"))
	testkit.WriteFile(t, copiedSkill, "SKILL.md", "---\nname: review\ndescription: Review\n---\n")
	outputPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["claude-code"]

[[skill]]
name = "review"
source = { path = "`+filepath.ToSlash(copiedSkill)+`", mode = "vendor" }
targets = ["claude-code"]
scope = "global"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath, "--merge"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `merge resource="skill/review" status=merge_targets`) {
		t.Fatalf("stdout = %q, want skill merge target row", stdout.String())
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(content), `targets = ["claude-code", "codex"]`) {
		t.Fatalf("manifest = %s, want merged skill targets", content)
	}
}

func TestRunImportMergeDryRunReportsDivergentSameNameConflict(t *testing.T) {
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
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--merge", "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`merge resource="instructions/codex_project" status=conflict`,
		"existing instruction has the same name with a different source, scope, or rendering",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, outputPath, string(original))
}

func TestRunImportMergeDryRunReportsNoopForExistingMatch(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.toml")
	sourcePath := filepath.Join(tempDir, "daem.d", "instructions", "codex-project.md")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	testkit.WriteFile(t, filepath.Dir(sourcePath), filepath.Base(sourcePath), "project instructions\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.codex_project]
source = "`+filepath.ToSlash(sourcePath)+`"
targets = ["codex"]
scope = "project"
`)
	original, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--merge", "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `merge resource="instructions/codex_project" status=noop`) {
		t.Fatalf("stdout = %q, want noop merge row", stdout.String())
	}
	testkit.AssertFileContent(t, outputPath, string(original))
}
