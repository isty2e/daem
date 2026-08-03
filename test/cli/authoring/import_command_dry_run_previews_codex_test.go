package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunImportDryRunPreviewsCodexProjectInstructionsWithoutWrites(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	sourcePath := filepath.Join(tempDir, "daem.imported.d", "instructions", "codex-project.md")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"import: 1 resources",
		"summary: instructions=1 skills=0 hooks=0",
		"summary target=codex scope=project instructions=1 skills=0 hooks=0",
		"destination: manifest=" + outputPath + " source-dir=" + filepath.Join(tempDir, "daem.imported.d"),
		`resource="instructions/codex_project"`,
		`target=codex`,
		`scope=project`,
		`source="` + filepath.ToSlash(sourcePath) + `"`,
		`live="AGENTS.md"`,
		"next: rerun daem import without --dry-run",
		"note: import writes or merges the manifest and copied source files only; host files are written only by apply",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if strings.Contains(stdout.String(), "manifest diff:") {
		t.Fatalf("stdout = %q, want concise dry-run without manifest diff", stdout.String())
	}
	testkit.AssertPathMissing(t, outputPath)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "project instructions\n")
}

func TestRunImportDryRunRejectsSkillTreeDeeperThanWritableStaging(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	skillRoot := filepath.Join(tempDir, ".agents", "skills", "deep")
	testkit.WriteFile(t, skillRoot, "SKILL.md", "---\nname: deep\n---\n")
	nested := skillRoot
	for range 65 {
		nested = filepath.Join(nested, "nested")
		if err := os.Mkdir(nested, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	sourceDirectory := filepath.Join(tempDir, "daem.imported.d")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"import", "--target", "codex", "--manifest", outputPath, "--dry-run"},
		&stdout,
		&stderr,
	)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stderr = %q", exitCode, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no inadmissible preview", stdout.String())
	}
	if !strings.Contains(stderr.String(), "artifact tree exceeds maximum depth 64") {
		t.Fatalf("stderr = %q, want staging-depth rejection", stderr.String())
	}
	if strings.Contains(stderr.String(), "rerun daem import without --dry-run") {
		t.Fatalf("stderr = %q, want no write-mode recommendation", stderr.String())
	}
	testkit.AssertPathMissing(t, outputPath)
	testkit.AssertPathMissing(t, sourceDirectory)
}

func TestRunImportMergeConflictDryRunReportsResolutionNextStep(t *testing.T) {
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

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--merge", "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `merge resource="instructions/codex_project" status=conflict`) {
		t.Fatalf("stdout = %q, want merge conflict row", stdout.String())
	}
	if !strings.Contains(stdout.String(), "next: resolve reported import conflicts before rerunning import without --dry-run") {
		t.Fatalf("stdout = %q, want conflict resolution next step", stdout.String())
	}
	if strings.Contains(stdout.String(), "rerun this import command with --yes") ||
		strings.Contains(stdout.String(), "daem lock --manifest") ||
		strings.Contains(stdout.String(), "daem apply --manifest") {
		t.Fatalf("stdout = %q, want no write/apply next steps while conflicts are present", stdout.String())
	}
}

func TestRunImportDryRunDiffShowsGeneratedManifestDelta(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--dry-run", "--diff"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"manifest diff:",
		"--- /dev/null",
		"+++ " + outputPath,
		"+version = 1",
		`+targets = ["codex"]`,
		"+  [instructions.codex_project]",
		`+    source = "`,
		`+    targets = ["codex"]`,
		`+    scope = "project"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportAcceptsRepeatedTargetsAndScopes(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	codexHome := filepath.Join(tempDir, "codex-home")
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", codexHome)
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "codex project instructions\n")
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "claude project instructions\n")
	testkit.WriteFile(t, codexHome, "AGENTS.md", "codex global instructions\n")
	testkit.WriteFile(t, filepath.Join(homeDir, ".claude"), "CLAUDE.md", "claude global instructions\n")

	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"import",
		"--target", "codex",
		"--target", "claude-code",
		"--target", "codex",
		"--scope", "global",
		"--scope", "project",
		"--scope", "project",
		"--manifest", outputPath,
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"import: 4 resources",
		"summary: instructions=4 skills=0 hooks=0",
		"summary target=codex scope=project instructions=1 skills=0 hooks=0",
		"summary target=codex scope=global instructions=1 skills=0 hooks=0",
		"summary target=claude-code scope=project instructions=1 skills=0 hooks=0",
		"summary target=claude-code scope=global instructions=1 skills=0 hooks=0",
		`resource="instructions/codex_project"`,
		`resource="instructions/codex_global"`,
		`resource="instructions/claude_code_project"`,
		`resource="instructions/claude_code_global"`,
		`target=codex`,
		`target=claude-code`,
		`scope=project`,
		`scope=global`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportYesImportsOpenCodeProjectSkillWithUnimplementedInstructionImportSkip(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, filepath.Join(tempDir, ".opencode", "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "opencode", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`resource="skill/oracle"`,
		`target=opencode`,
		`skip live="AGENTS.md" reason=missing`,
		`skip live="CLAUDE.md" reason=missing`,
		`skip live="opencode:project:hooks" reason=unsupported_hooks_surface`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	skill := findImportedSkill(t, config.Skills(), "oracle")
	targets := skill.Targets()
	if len(targets) != 1 || targets[0] != target.TargetOpenCode {
		t.Fatalf("targets = %#v, want opencode", targets)
	}
}

func TestRunImportYesImportsPiGlobalSkillWithUnimplementedInstructionImportSkip(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, filepath.Join(homeDir, ".pi", "agent", "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "pi", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`resource="skill/oracle"`,
		`target=pi`,
		`skip live="` + filepath.Join(homeDir, ".pi", "agent", "AGENTS.md") + `" reason=missing`,
		`skip live="pi:global:hooks" reason=unsupported_hooks_surface`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	skill := findImportedSkill(t, config.Skills(), "oracle")
	targets := skill.Targets()
	if len(targets) != 1 || targets[0] != target.TargetPi || skill.Scope() != target.ScopeGlobal {
		t.Fatalf("skill = %#v, want global pi skill", skill)
	}
}

func TestRunImportYesCoalescesOpenCodeAndPiSharedSkillRoot(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, filepath.Join(tempDir, ".agents", "skills", "shared-review"), "SKILL.md", "---\nname: shared-review\ndescription: Shared\n---\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "opencode", "--target", "pi", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `resource="skill/shared-review"`) {
		t.Fatalf("stdout = %q, want shared skill import", stdout.String())
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	skill := findImportedSkill(t, config.Skills(), "shared-review")
	targets := skill.Targets()
	if len(targets) != 2 || targets[0] != target.TargetOpenCode || targets[1] != target.TargetPi {
		t.Fatalf("targets = %#v, want opencode/pi", targets)
	}
}

func TestRunImportRejectsCommaScopeTokenWithoutExpansion(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "project,workspace", "--manifest", filepath.Join(tempDir, "daem.imported.toml"), "--dry-run"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), `unknown scope "project,workspace"`) {
		t.Fatalf("stderr = %q, want whole-token scope diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunImportYesWritesClaudeCodeProjectInstruction(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "CLAUDE.md", "claude project instructions\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	sourcePath := filepath.Join(tempDir, "daem.imported.d", "instructions", "claude-code-project.md")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "claude-code", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"imported: 1 resources",
		`resource="instructions/claude_code_project"`,
		`target=claude-code`,
		`source="` + filepath.ToSlash(sourcePath) + `"`,
		`live="CLAUDE.md"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "CLAUDE.md"), "claude project instructions\n")
	testkit.AssertFileContent(t, sourcePath, "claude project instructions\n")
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
	if instructions.ID().Name() != "claude_code_project" {
		t.Fatalf("instructions = %#v", instructions)
	}
	assertInstructionLocalSource(t, instructions, filepath.ToSlash(sourcePath))
	targets := instructions.Targets()
	if len(targets) != 1 || targets[0] != target.TargetClaudeCode {
		t.Fatalf("Targets = %#v, want claude-code", targets)
	}
}
