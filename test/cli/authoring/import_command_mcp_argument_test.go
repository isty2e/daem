package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunImportCreateSkipsInvalidMCPArgumentAndPublishesValidSibling(t *testing.T) {
	const canary = "CREATE_ARGUMENT_LEAK_CANARY"
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	outputPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, aggregate.ClaudeProjectMCPConfigPath, `{
  "mcpServers": {
    "invalid": {"type": "stdio", "command": "node", "args": ["\u0000`+canary+`"]},
    "valid": {"type": "stdio", "command": "node", "args": ["server.js"]}
  }
}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"import", "--target", "claude-code", "--manifest", outputPath},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("import exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "imported: 1 resources") ||
		!strings.Contains(stdout.String(), `skip live=".mcp.json#/mcpServers/invalid" reason=invalid_mcp_argument`) ||
		strings.Contains(stdout.String(), canary) {
		t.Fatalf("stdout = %q, want one safe typed invalid-argument skip", stdout.String())
	}
	manifest := string(testkit.ReadFile(t, outputPath))
	if strings.Contains(manifest, `name = "invalid"`) || strings.Contains(manifest, canary) {
		t.Fatalf("manifest published invalid MCP argument: %q", manifest)
	}
	server := readImportedMCPServer(t, outputPath, "valid")
	testkit.AssertSingleMCPStdioBinding(
		t,
		server,
		"valid",
		target.TargetClaudeCode,
		target.ScopeProject,
		"node",
		[]string{"server.js"},
	)
}

func TestRunImportMergeSkipsInvalidMCPArgumentAndMergesValidSibling(t *testing.T) {
	const canary = "MERGE_ARGUMENT_LEAK_CANARY"
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	outputPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")
	testkit.WriteFile(t, root, aggregate.ClaudeProjectMCPConfigPath, `{
  "mcpServers": {
    "invalid": {"type": "stdio", "command": "node", "args": ["\u202e`+canary+`"]},
    "valid": {"type": "stdio", "command": "node", "args": ["server.js"]}
  }
}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"import", "--target", "claude-code", "--manifest", outputPath, "--merge"},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("import --merge exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "imported: 1 resources") ||
		!strings.Contains(stdout.String(), `skip live=".mcp.json#/mcpServers/invalid" reason=invalid_mcp_argument`) ||
		strings.Contains(stdout.String(), canary) {
		t.Fatalf("stdout = %q, want one safe typed invalid-argument skip", stdout.String())
	}
	manifest := string(testkit.ReadFile(t, outputPath))
	if strings.Contains(manifest, `name = "invalid"`) || strings.Contains(manifest, canary) {
		t.Fatalf("manifest merged invalid MCP argument: %q", manifest)
	}
	server := readImportedMCPServer(t, outputPath, "valid")
	testkit.AssertSingleMCPStdioBinding(
		t,
		server,
		"valid",
		target.TargetClaudeCode,
		target.ScopeProject,
		"node",
		[]string{"server.js"},
	)
}

func TestRunImportWithOnlyInvalidMCPArgumentsWritesNothing(t *testing.T) {
	const canary = "ONLY_INVALID_ARGUMENT_LEAK_CANARY"
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	outputPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, aggregate.ClaudeProjectMCPConfigPath, `{
  "mcpServers": {
    "invalid": {"type": "stdio", "command": "node", "args": ["\u0000`+canary+`"]}
  }
}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"import", "--target", "claude-code", "--manifest", outputPath},
		&stdout,
		&stderr,
	)
	if exitCode != 1 || stdout.Len() != 0 {
		t.Fatalf("import exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "nothing to import") ||
		!strings.Contains(stderr.String(), `skip live=".mcp.json#/mcpServers/invalid" reason=invalid_mcp_argument target=claude-code scope=project category=action_required action_hint=repair_source`) ||
		strings.Contains(stderr.String(), canary) {
		t.Fatalf("stderr = %q, want safe nothing-to-import diagnostics", stderr.String())
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportWithOnlySecretMCPServerShowsTypedRemediation(t *testing.T) {
	const canary = "ONLY_SECRET_ARGUMENT_LEAK_CANARY"
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	outputPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, aggregate.ClaudeProjectMCPConfigPath, `{
  "mcpServers": {
    "secret": {"type": "stdio", "command": "node", "env": {"TOKEN": "`+canary+`"}}
  }
}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunCLI(
		[]string{"import", "--target", "claude-code", "--manifest", outputPath},
		&stdout,
		&stderr,
	)
	if exitCode != 1 || stdout.Len() != 0 {
		t.Fatalf("import exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		"nothing to import",
		`skip live=".mcp.json#/mcpServers/secret" reason=secret_literal_forbidden target=claude-code scope=project`,
		"next: replace literal secrets with symbolic environment references or leave this row unmanaged",
		"next: verify that the selected --target and --scope have live agent files to import",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if strings.Contains(stderr.String(), canary) {
		t.Fatalf("stderr disclosed literal secret: %q", stderr.String())
	}
	testkit.AssertPathMissing(t, outputPath)
}
