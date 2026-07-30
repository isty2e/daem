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

func TestRunImportYesFallsBackToCodexGlobalBaseWhenOverrideEmpty(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", "")
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, homeDir, ".codex/AGENTS.override.md", " \n\t\n")
	testkit.WriteFile(t, homeDir, ".codex/AGENTS.md", "base global\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `live="`+filepath.Join(homeDir, ".codex", "AGENTS.md")+`"`) {
		t.Fatalf("stdout = %q, want fallback base live path", stdout.String())
	}
	if !strings.Contains(stdout.String(), "empty_instruction_file") {
		t.Fatalf("stdout = %q, want empty override skip", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "daem.imported.d", "instructions", "codex-global.md"), "base global\n")
}

func TestRunImportYesUsesCodexHomeForGlobalInstruction(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	codexHome := filepath.Join(tempDir, "codex-home")
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", codexHome)
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, codexHome, "AGENTS.md", "codex home global\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `live="`+filepath.Join(codexHome, "AGENTS.md")+`"`) {
		t.Fatalf("stdout = %q, want CODEX_HOME live path", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "daem.imported.d", "instructions", "codex-global.md"), "codex home global\n")
}

func TestRunImportYesWritesClaudeCodeGlobalInstruction(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, homeDir, ".claude/CLAUDE.md", "claude global\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	sourcePath := filepath.Join(tempDir, "daem.imported.d", "instructions", "claude-code-global.md")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "claude-code", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"imported: 1 resources",
		`resource="instructions/claude_code_global"`,
		`target=claude-code`,
		`scope=global`,
		`source="` + filepath.ToSlash(sourcePath) + `"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, sourcePath, "claude global\n")
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	instructions := findImportedInstruction(t, config.Instructions(), "claude_code_global")
	if instructions.Scope() != target.ScopeGlobal {
		t.Fatalf("Scope = %q, want global", instructions.Scope())
	}
	assertInstructionLocalSource(t, instructions, filepath.ToSlash(sourcePath))
	targets := instructions.Targets()
	if len(targets) != 1 || targets[0] != target.TargetClaudeCode {
		t.Fatalf("Targets = %#v, want claude-code", targets)
	}
}

func TestRunImportDryRunReportsMissingGlobalInstructionWhileImportingSkill(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", "")
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, filepath.Join(homeDir, ".agents", "skills", "alpha"), "SKILL.md", "---\nname: alpha\ndescription: Alpha\n---\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"import: 1 resources",
		`resource="skill/alpha"`,
		`skip live="` + filepath.Join(homeDir, ".codex", "AGENTS.md") + `" reason=` + "missing",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportDeduplicatesNormalizedHookNames(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, ".claude/settings.json", `{
  "hooks": {
    "My Event": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "make one"
          }
        ]
      }
    ],
    "My_Event": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "make two"
          }
        ]
      }
    ]
  }
}
`)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "claude-code", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	findImportedHook(t, config.Hooks(), "claude_code_project_my_event_1_1")
	findImportedHook(t, config.Hooks(), "claude_code_project_my_event_1_1_2")
}

func TestRunImportValidationErrors(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "target required", args: []string{"import", "--manifest", outputPath, "--dry-run"}, want: "--target is required"},
		{name: "removed yes", args: []string{"import", "--target", "codex", "--manifest", outputPath, "--yes"}, want: "flag provided but not defined: -yes"},
		{name: "diff requires dry run", args: []string{"import", "--target", "codex", "--manifest", outputPath, "--diff"}, want: "--diff requires --dry-run"},
		{name: "unknown target", args: []string{"import", "--target", "zed", "--manifest", outputPath, "--dry-run"}, want: "unknown target"},
		{name: "unsupported scope", args: []string{"import", "--target", "codex", "--scope", "workspace", "--manifest", outputPath, "--dry-run"}, want: "unknown scope"},
		{name: "source dir internal", args: []string{"import", "--target", "codex", "--manifest", outputPath, "--source-dir", ".daem/imported", "--dry-run"}, want: "source-dir must not be inside .daem"},
		{name: "output inside source dir", args: []string{"import", "--target", "codex", "--manifest", filepath.Join(tempDir, "generated", "daem.toml"), "--source-dir", ".", "--dry-run"}, want: "output must not be inside source-dir"},
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

func TestRunImportRejectsRemovedYesFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--yes"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -yes") {
		t.Fatalf("stderr = %q, want removed --yes diagnostic", stderr.String())
	}
}

func TestRunImportHelpShowsStaticWorkspaceContract(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--help"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	normalizedHelp := strings.Join(strings.Fields(stdout.String()), " ")
	for _, want := range []string{
		"Without --manifest, non-merge import creates ./daem.toml",
		"--merge uses existing-workspace selection",
		"Extensions are imported only when daem can recover their exact install source",
		"other observed extensions are reported and skipped",
		"daem import --target codex",
	} {
		if !strings.Contains(normalizedHelp, want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if strings.Contains(stdout.String(), "Default:") {
		t.Fatalf("stdout = %q, static help must not inspect a dynamic default path", stdout.String())
	}
}
