package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunImportReportsCodexInlineHookConfigAsSkipped(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, ".codex/hooks.json", `{
  "hooks": {
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
	testkit.WriteFile(t, tempDir, ".codex/config.toml", `
[hooks]

[[hooks.PreToolUse]]
matcher = "Bash"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "make inline"
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
		`skip live=".codex/config.toml" reason=unsupported_inline_hooks`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportReportsCodexInlineOnlyHookConfigAsNothingToImport(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, ".codex/config.toml", `
[hooks]

[[hooks.PreToolUse]]
matcher = "Bash"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "make inline"
`)
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
		"action_required=0 unsupported=1 informational=4",
		"informational target=codex reason=missing count=4",
		"unsupported target=codex reason=unsupported_inline_hooks count=1",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportReportsMalformedCodexInlineConfigWithoutBlockingHooksJSON(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, ".codex/hooks.json", `{
  "hooks": {
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
	testkit.WriteFile(t, tempDir, ".codex/config.toml", "[hooks\n")
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
		`skip live=".codex/config.toml" reason=inline_config_malformed`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportSkipsClaudeExecFormArgsWithoutDroppingRepresentableSiblings(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, ".claude/settings.json", `{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "node",
            "args": ["${CLAUDE_PROJECT_DIR}/hooks/check.js"]
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
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "claude-code", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"import: 1 resources",
		`resource="hook/claude_code_project_posttooluse_1_2"`,
		`reason=event=PostToolUse,group=1,handler=1,unsupported_handler_field_args`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportReportsMultipleHookJSONValuesAsSkipped(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, ".claude/settings.json", `{"hooks":{}} {}`)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "claude-code", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), ".claude/settings.json: multiple_json_values") {
		t.Fatalf("stderr = %q, want multiple_json_values skip summary", stderr.String())
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportReportsDuplicateHookJSONAsSkippedWithoutPartialHook(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "project guidance\n")
	testkit.WriteFile(t, tempDir, ".codex/hooks.json", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"must-not-import"}]}]},"hooks":{}}`)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"summary: instructions=1 skills=0 hooks=0",
		`skip live=".codex/hooks.json" reason=duplicate_json_key`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if strings.Contains(stdout.String(), "must-not-import") || strings.Contains(stdout.String(), `resource="hook/`) {
		t.Fatalf("stdout = %q, imported part of an ambiguous hook document", stdout.String())
	}
	testkit.AssertPathMissing(t, outputPath)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
}

func TestRunImportReportsOversizedMCPDocumentWithoutPartialImport(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("Claude project MCP placement is missing")
	}
	codec, ok := aggregatecodec.Catalog().Lookup(placement.CodecContractID())
	if !ok {
		t.Fatalf("MCP codec %q is missing", placement.CodecContractID())
	}
	livePath := filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath)
	file, err := os.OpenFile(livePath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(codec.MaximumDocumentBytes() + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(tempDir, "daem.imported.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "claude-code", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 || stdout.Len() != 0 {
		t.Fatalf("exitCode = %d, stdout = %q, stderr = %q, want no-import failure", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), aggregate.ClaudeProjectMCPConfigPath+": mcp_config_too_large") {
		t.Fatalf("stderr = %q, want MCP document size skip", stderr.String())
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportDryRunImportsGlobalHookConfig(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, homeDir, ".codex/hooks.json", `{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "make global"
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
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"import: 1 resources",
		`resource="hook/codex_global_stop_1_1"`,
		`scope=global`,
		`live="~/.codex/hooks.json"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportYesWritesCodexGlobalOverrideInstruction(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", "")
	testkit.WithWorkingDirectory(t, tempDir)
	testkit.WriteFile(t, homeDir, ".codex/AGENTS.md", "base global\n")
	testkit.WriteFile(t, homeDir, ".codex/AGENTS.override.md", "override global\n")
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	sourcePath := filepath.Join(tempDir, "daem.imported.d", "instructions", "codex-global.md")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"imported: 1 resources",
		`resource="instructions/codex_global"`,
		`target=codex`,
		`scope=global`,
		`source="` + filepath.ToSlash(sourcePath) + `"`,
		`live="` + filepath.Join(homeDir, ".codex", "AGENTS.override.md") + `"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, sourcePath, "override global\n")

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	instructions := findImportedInstruction(t, config.Instructions(), "codex_global")
	if instructions.Scope() != target.ScopeGlobal {
		t.Fatalf("Scope = %q, want global", instructions.Scope())
	}
	assertInstructionLocalSource(t, instructions, filepath.ToSlash(sourcePath))
	targets := instructions.Targets()
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
