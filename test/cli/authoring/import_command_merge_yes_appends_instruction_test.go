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

func TestRunImportMergeYesAppendsInstructionAndPreservesExistingGitSkill(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.toml")
	sourcePath := filepath.Join(tempDir, "daem.d", "instructions", "codex-project.md")
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project instructions\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "remote-review"
source = { git = "https://github.com/example/skills.git", path = "review", ref = "main" }
targets = ["codex"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--merge"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `merge resource="instructions/codex_project" status=add`) {
		t.Fatalf("stdout = %q, want instruction add merge row", stdout.String())
	}
	testkit.AssertFileContent(t, sourcePath, "project instructions\n")
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	for _, want := range []string{
		`git = "https://github.com/example/skills.git"`,
		`[instructions.codex_project]`,
		`source = "` + filepath.ToSlash(sourcePath) + `"`,
	} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("manifest = %s, want %q", content, want)
		}
	}
	if _, err := declarationmanifest.Decode(content); err != nil {
		t.Fatalf("merged manifest did not parse: %v\n%s", err, content)
	}
}

func TestRunImportMergeYesAppendsCompatibleSkillsAsSkillGroup(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	alphaSkill := filepath.Join(homeDir, ".codex", "skills", "alpha")
	betaSkill := filepath.Join(homeDir, ".codex", "skills", "beta")
	testkit.WriteFile(t, alphaSkill, "SKILL.md", "---\nname: alpha\ndescription: Alpha\n---\n")
	testkit.WriteFile(t, betaSkill, "SKILL.md", "---\nname: beta\ndescription: Beta\n---\n")
	outputPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath, "--merge"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	if !strings.Contains(string(content), "[[skill_group]]") {
		t.Fatalf("manifest = %s, want appended skill_group", content)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("merged manifest did not parse: %v\n%s", err, content)
	}
	if len(config.Skills()) != 2 {
		t.Fatalf("skills = %#v, want two expanded skills", config.Skills())
	}
}

func TestRunImportYesWritesCodexAndClaudeHookManifestWithoutSources(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, ".codex/hooks.json", `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "^Bash$",
        "hooks": [
          {
            "type": "command",
            "command": "python3 hooks/protect.py",
            "async": false,
            "timeout": 30,
            "statusMessage": "Checking Bash command"
          }
        ]
      }
    ]
  }
}
`)
	testkit.WriteFile(t, tempDir, ".claude/settings.json", `{
  "env": {
    "KEEP": "yes"
  },
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "python3 hooks/claude.py",
            "if": "tool_name == 'Edit'",
            "timeout": 7,
            "statusMessage": "checking"
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
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--target", "claude-code", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"imported: 2 resources",
		`resource="hook/codex_project_pretooluse_1_1"`,
		`resource="hook/claude_code_project_posttooluse_1_1"`,
		`live=".codex/hooks.json"`,
		`live=".claude/settings.json"`,
		`skip live="AGENTS.md" reason=missing`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
	testkit.AssertFileContent(t, filepath.Join(tempDir, ".codex/hooks.json"), `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "^Bash$",
        "hooks": [
          {
            "type": "command",
            "command": "python3 hooks/protect.py",
            "async": false,
            "timeout": 30,
            "statusMessage": "Checking Bash command"
          }
        ]
      }
    ]
  }
}
`)

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	if len(config.Hooks()) != 2 {
		t.Fatalf("hooks = %#v", config.Hooks())
	}
	codexHook := findImportedHook(t, config.Hooks(), "codex_project_pretooluse_1_1")
	if codexHook.Event() != "PreToolUse" || codexHook.Matcher() != "^Bash$" || codexHook.Command() != "python3 hooks/protect.py" || codexHook.TimeoutSeconds() != 30 || codexHook.StatusMessage() != "Checking Bash command" {
		t.Fatalf("codexHook = %#v", codexHook)
	}
	claudeHook := findImportedHook(t, config.Hooks(), "claude_code_project_posttooluse_1_1")
	if claudeHook.Event() != "PostToolUse" || claudeHook.Matcher() != "Edit|Write" || claudeHook.Command() != "python3 hooks/claude.py" || claudeHook.TimeoutSeconds() != 7 {
		t.Fatalf("claudeHook = %#v", claudeHook)
	}
	targets := claudeHook.Targets()
	if len(targets) != 1 {
		t.Fatalf("claude targets = %#v, want one target", targets)
	}
	effective, err := claudeHook.EffectiveMatch(targets[0])
	if err != nil || effective.Condition() != "tool_name == 'Edit'" {
		t.Fatalf("claude effective match = %#v, err = %v", effective, err)
	}
}

func TestRunImportReportsMalformedAndUnsupportedHookFormsAsSkipped(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, ".codex/hooks.json", `{"hooks":`)
	testkit.WriteFile(t, tempDir, ".claude/settings.json", `{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "make test",
            "async": true
          },
          {
            "type": "prompt",
            "command": "ask user"
          },
          {
            "type": "command",
            "command": "make lint"
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
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--target", "claude-code", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"import: 1 resources",
		`resource="hook/claude_code_project_stop_1_3"`,
		`skip live=".codex/hooks.json" reason=malformed_json`,
		`reason=event=Stop,group=1,handler=1,unsupported_async`,
		`reason=event=Stop,group=1,handler=2,unsupported_handler_type`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportSkipsCodexHookShapesApplyCannotRender(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, ".codex/hooks.json", `{
  "hooks": {
    "Stop": [
      {
        "matcher": ".*",
        "hooks": [
          {
            "type": "command",
            "command": "make stop"
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "make prompt"
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
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"import: 1 resources",
		`resource="hook/codex_project_userpromptsubmit_1_1"`,
		`reason=event=Stop,group=1,handler=1,unsupported_target_shape`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}
