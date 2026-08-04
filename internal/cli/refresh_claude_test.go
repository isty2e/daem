package cli

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/isty2e/daem/internal/subprocess"
	lockworkflow "github.com/isty2e/daem/internal/workflow/lock"
)

func TestClaudeRefreshCLIUsesOneJSONResultEnvelope(t *testing.T) {
	manifestPath := writeClaudeCLIRefreshFixture(t)
	dryRun := runHostRouteRefreshCLI(t, manifestPath, "context7", true, nil, 0)
	if dryRun.Result.Class != "planned" || dryRun.Result.Detail != "" {
		t.Fatalf("dry-run result = %#v", dryRun.Result)
	}

	failure := runHostRouteRefreshCLI(
		t,
		manifestPath,
		"context7",
		false,
		func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{
				Started:     true,
				Stderr:      "token=claude-refresh-secret",
				ExitCode:    23,
				HasExitCode: true,
			}
		},
		1,
	)
	if failure.Result.Class != "failed" ||
		failure.Result.ReasonCode != "command_failed" ||
		failure.Result.Detail != "delegated host command result: nonzero_exit" {
		t.Fatalf("failure result = %#v", failure.Result)
	}
}

func writeClaudeCLIRefreshFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	manifest := []byte(`version = 1
targets = ["claude-code"]

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
scope = "project"
source = { marketplace = "context7@market" }
`)
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	inventoryPath := filepath.Join(configRoot, "plugins", "installed_plugins.json")
	if err := os.MkdirAll(filepath.Dir(inventoryPath), 0o700); err != nil {
		t.Fatalf("MkdirAll inventory directory: %v", err)
	}
	inventory := `{"version":2,"plugins":{"context7@market":[{"scope":"project","projectPath":` +
		strconv.Quote(filepath.Dir(manifestPath)) + `}]}}`
	if err := os.WriteFile(inventoryPath, []byte(inventory), 0o600); err != nil {
		t.Fatalf("WriteFile inventory: %v", err)
	}
	if _, err := lockworkflow.RunLock(
		context.Background(),
		lockworkflow.LockInput{ManifestPath: manifestPath},
	); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	return manifestPath
}
