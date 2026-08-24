package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

const importSkipSecretCanary = "IMPORT_SKIP_SECRET_CANARY"

func TestRunImportDefaultCompactsBenignSkipsAndShowsActionableMultiTargetRow(t *testing.T) {
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	writeImportSkipClassificationFixture(t, root)
	outputPath := filepath.Join(root, "daem.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunCLI(
		[]string{
			"import",
			"--target", "codex",
			"--target", "claude-code",
			"--manifest", outputPath,
			"--dry-run",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("import exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	text := stdout.String()
	for _, expected := range []string{
		"skipped: action_required=1 unsupported=1 informational=",
		"action required:",
		`skip live=".mcp.json#/mcpServers/secret" reason=secret_literal_forbidden target=claude-code scope=project`,
		"next: replace literal secrets with symbolic environment references or leave this row unmanaged",
		"unsupported:",
		"target=claude-code reason=unsupported_mcp_managed_field count=1",
		"informational:",
		"target=claude-code reason=missing count=4",
		"target=codex reason=missing count=5",
		"skipped detail: rerun with --verbose to inspect every skipped path",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("stdout = %q, want %q", text, expected)
		}
	}
	for _, forbidden := range []string{
		".mcp.json#/mcpServers/remote",
		importSkipSecretCanary,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("stdout = %q, unexpectedly contains %q", text, forbidden)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
}

func TestRunImportJSONRetainsEveryClassifiedSkipWithoutSecretValues(t *testing.T) {
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	writeImportSkipClassificationFixture(t, root)
	outputPath := filepath.Join(root, "daem.toml")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunCLI(
		[]string{
			"import",
			"--target", "codex",
			"--target", "claude-code",
			"--manifest", outputPath,
			"--dry-run",
			"--json",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("import exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), importSkipSecretCanary) {
		t.Fatalf("JSON disclosed literal secret: %s", stdout.String())
	}
	payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	var actionable int
	var unsupported int
	var informational int
	for _, skipped := range payload.Skipped {
		switch {
		case skipped.Reason == "secret_literal_forbidden":
			actionable++
			if skipped.Target != "claude-code" || skipped.Scope != "project" ||
				skipped.Category != "action_required" ||
				skipped.ActionHint != "use_symbolic_environment_reference" {
				t.Fatalf("actionable skip = %#v", skipped)
			}
		case skipped.Reason == "unsupported_mcp_managed_field":
			unsupported++
			if skipped.Target != "claude-code" || skipped.Scope != "project" ||
				skipped.Category != "unsupported" || skipped.ActionHint != "" {
				t.Fatalf("unsupported skip = %#v", skipped)
			}
		case skipped.Reason == "missing":
			informational++
			if skipped.Category != "informational" || skipped.ActionHint != "" {
				t.Fatalf("informational skip = %#v", skipped)
			}
		}
	}
	if actionable != 1 || unsupported != 1 || informational != 9 {
		t.Fatalf("classified skips action=%d unsupported=%d informational=%d; all=%#v", actionable, unsupported, informational, payload.Skipped)
	}
	testkit.AssertPathMissing(t, outputPath)
}

func writeImportSkipClassificationFixture(t *testing.T, root string) {
	t.Helper()
	testkit.WriteFile(t, root, aggregate.ClaudeProjectMCPConfigPath, `{
  "mcpServers": {
    "valid": {"type": "stdio", "command": "node", "args": ["server.js"]},
    "remote": {"type": "http", "url": "https://example.invalid/mcp"},
    "secret": {"type": "stdio", "command": "node", "env": {"TOKEN": "`+importSkipSecretCanary+`"}}
  }
}`)
}
