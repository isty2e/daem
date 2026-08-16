package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestClaudePluginBundlesAreNotImportCandidates(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	testkit.WithWorkingDirectory(t, tempDir)
	t.Setenv("HOME", homeDir)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(homeDir, ".claude"))
	testkit.SetDefaultRootEnv(t, tempDir)
	writeClaudePluginBundleTrap(t, filepath.Join(tempDir, ".claude", "plugins", "cache", "context7", "1.0.0"))
	writeClaudePluginBundleTrap(t, filepath.Join(homeDir, ".claude", "plugins", "cache", "context7", "1.0.0"))
	canary := execcheck.New(t, "claude", "npx", "node", "bundled-hook-trap")

	for _, mode := range []struct {
		name string
		flag string
	}{{name: "dry-run", flag: "--dry-run"}, {name: "write"}} {
		t.Run(mode.name, func(t *testing.T) {
			outputPath := filepath.Join(tempDir, "imported-"+mode.name+".toml")
			args := []string{
				"import",
				"--target", "claude-code",
				"--scope", "project",
				"--scope", "global",
				"--manifest", outputPath,
			}
			if mode.flag != "" {
				args = append(args, mode.flag)
			}
			args = append(args, "--json")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), "nothing to import") {
				t.Fatalf("stdout=%q stderr=%q, want nothing-to-import failure", stdout.String(), stderr.String())
			}
			assertNoClaudePluginBundleIdentityPromotion(t, stderr.String())
			assertNoPluginRuntimeReadinessClaim(t, stderr.String())
			testkit.AssertPathMissing(t, outputPath)
			testkit.AssertPathMissing(t, strings.TrimSuffix(outputPath, ".toml")+".d")
			execcheck.AssertClean(t, canary, "import Claude plugin bundle "+mode.name)
		})
	}
}

func TestClaudePluginBundlesDoNotBecomeCurrentInventoryOrReadiness(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WithWorkingDirectory(t, tempDir)
	t.Setenv("HOME", homeDir)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(homeDir, ".claude"))
	testkit.SetDefaultRootEnv(t, tempDir)
	testkit.WriteFile(t, tempDir, "daem.toml", claudeExtensionManifest())
	writeClaudePluginBundleTrap(t, filepath.Join(tempDir, ".claude", "plugins", "cache", "context7", "1.0.0"))
	writeClaudePluginBundleTrap(t, filepath.Join(homeDir, ".claude", "plugins", "cache", "context7", "1.0.0"))
	canary := execcheck.New(t, "claude", "npx", "node", "bundled-hook-trap")

	commands := []struct {
		name string
		args []string
	}{
		{name: "doctor", args: []string{"doctor", "--manifest", manifestPath, "--target", "claude-code", "--json"}},
		{name: "list", args: []string{"list", "resources", "--manifest", manifestPath}},
		{name: "lock", args: []string{"lock", "--manifest", manifestPath, "--json"}},
		{name: "status", args: []string{"status", "--manifest", manifestPath, "--json"}},
	}

	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(command.args, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("exitCode = %d, stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			assertNoClaudePluginBundleIdentityPromotion(t, stdout.String())
			assertNoPluginRuntimeReadinessClaim(t, stdout.String())
			switch command.name {
			case "lock":
				payload := clijson.DecodeLock(t, stdout.Bytes())
				if payload.EntryCounts.Subjects != 1 {
					t.Fatalf("lock entry counts = %#v, want one carrier subject and no standalone resources", payload.EntryCounts)
				}
			case "status":
				payload := clijson.DecodePlan(t, stdout.Bytes())
				assertClaudeExtensionCarrierMissingCreateAction(t, payload.RelationActions)
			}
			execcheck.AssertClean(t, canary, command.name+" with Claude plugin bundle")
		})
	}

	lockedContent, err := os.ReadFile(lockfilePath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	assertNoClaudePluginBundleIdentityPromotion(t, string(lockedContent))
	assertNoPluginRuntimeReadinessClaim(t, string(lockedContent))
}

func TestMalformedClaudeInstalledInventoryBlocksApplyBeforeHostRoute(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	configRoot := filepath.Join(root, "claude-config")
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	testkit.WriteFile(t, root, "daem.toml", claudeExtensionManifest())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	testkit.WriteFile(t, configRoot, "plugins/installed_plugins.json", `{"version":2,"plugins":{"context7@market":null}}`)
	canary := execcheck.New(t, "claude")

	stdout.Reset()
	stderr.Reset()
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "apply failed: apply was refused before effects") ||
		!strings.Contains(stderr.String(), "rows must be an array") {
		t.Fatalf("apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	execcheck.AssertClean(t, canary, "malformed Claude installed inventory")
	testkit.AssertPathMissing(t, filepath.Join(root, ".daem", "state.json"))
}

func writeClaudePluginBundleTrap(t *testing.T, root string) {
	t.Helper()
	testkit.WriteFile(t, root, ".claude-plugin/plugin.json", `{
  "name": "bundle-promotion-trap",
  "mcpServers": {
    "bundled-mcp-trap": {"command": "npx", "args": ["-y", "@example/bundled-mcp-trap"]}
  }
}`)
	testkit.WriteFile(t, root, "skills/bundled-skill-trap/SKILL.md", `---
name: bundled-skill-trap
description: Must remain provider scoped.
---
`)
	testkit.WriteFile(t, root, "hooks/hooks.json", `{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"bundled-hook-trap"}]}]}}`)
	testkit.WriteFile(t, root, "commands/bundled-command-trap.md", "Must remain provider scoped.\n")
}

func assertNoClaudePluginBundleIdentityPromotion(t *testing.T, output string) {
	t.Helper()
	lower := strings.ToLower(output)
	for _, forbidden := range []string{
		"bundle-promotion-trap",
		"bundled-mcp-trap",
		"bundled-skill-trap",
		"bundled-hook-trap",
		"bundled-command-trap",
		"plugin_contribution",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("output promoted Claude plugin bundle identity %q:\n%s", forbidden, output)
		}
	}
}

func assertNoPluginRuntimeReadinessClaim(t *testing.T, output string) {
	t.Helper()
	lower := strings.ToLower(output)
	for _, forbidden := range []string{
		"runtime ready",
		"plugin ready",
		"authenticated",
		"trusted",
		"healthy",
		"tools available",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("output promoted plugin runtime readiness fact %q:\n%s", forbidden, output)
		}
	}
}
