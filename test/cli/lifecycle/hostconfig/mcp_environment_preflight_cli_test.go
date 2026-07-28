package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestMCPPublicCLIApplyNeverPrintsOrPersistsResolvedEnvironmentValue(t *testing.T) {
	for _, test := range []struct {
		name       string
		outputArgs []string
	}{
		{name: "human"},
		{name: "verbose", outputArgs: []string{"--verbose"}},
		{name: "json", outputArgs: []string{"--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			const (
				sourceName = "DAEM_TEST_SECRET_ENV_REF"
				secret     = "super-secret-value"
			)
			t.Setenv(sourceName, secret)
			project := newMCPCLIProject(t)
			spec := mcpManifestSpec{
				Command: "secret-boundary-daem-test",
				Args:    []string{"--serve", "context7"},
				Env:     map[string]string{"API_TOKEN": sourceName},
			}
			writeMCPManifest(t, project.root, spec)
			runMCPLock(t, project)

			args := []string{
				"apply",
				"--manifest", project.manifestPath,
				"--target", "claude-code",
				"--yes",
			}
			args = append(args, test.outputArgs...)
			delegateRuns := 0
			exitCode, stdout, stderr := runMCPCLIWithOptions(t, args, clipkg.RunOptions{
				ApplyExecuteOptions: applyworkflow.ExecuteOptions{
					DelegateExecutor: delegate.NewExecutor(delegate.Options{
						Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
							delegateRuns++
							if !slices.Contains(request.Env, "API_TOKEN="+secret) {
								t.Fatalf("delegate env = %#v, want launch-time API_TOKEN value", request.Env)
							}
							return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
						},
					}),
				},
			})
			if exitCode != 0 || stderr != "" || delegateRuns != 1 {
				t.Fatalf(
					"apply exitCode=%d delegateRuns=%d stdout=%q stderr=%q",
					exitCode,
					delegateRuns,
					stdout,
					stderr,
				)
			}
			assertMCPDelegateNoSecretLeak(t, test.name+" stdout", stdout)
			for _, path := range []string{
				project.lockfilePath,
				filepath.Join(project.root, ".mcp.json"),
				filepath.Join(project.root, ".daem", "state.json"),
			} {
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read %s: %v", path, err)
				}
				assertMCPDelegateNoSecretLeak(t, path, string(content))
			}
			recoveryPath := filepath.Join(project.root, ".daem", "recovery")
			entries, err := os.ReadDir(recoveryPath)
			if err != nil && !os.IsNotExist(err) {
				t.Fatalf("read recovery directory: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("recovery entries = %#v, want no persisted transaction artifacts", entries)
			}
		})
	}
}

func TestMCPPublicCLICrossTargetGlobalEnvironmentReferencesRemainIsolated(t *testing.T) {
	const (
		sharedSource     = "DAEM_TEST_CROSS_TARGET_SHARED"
		ambientSource    = "DAEM_TEST_CROSS_TARGET_AMBIENT"
		secretCanary     = "cross-target-secret-canary"
		claudeServerID   = "claude-global"
		codexServerID    = "codex-global"
		openCodeServerID = "opencode-global"
		ambientServerID  = "antigravity-global"
	)
	unsetEnvForMCPDelegateTest(t, sharedSource)
	t.Setenv(ambientSource, "")
	project := newMCPCLIProject(t)
	homeDir := filepath.Join(project.root, "host-home")
	t.Setenv("HOME", homeDir)
	writeCrossTargetGlobalMCPEnvironmentManifest(t, project.root, sharedSource, ambientSource)
	runMCPLock(t, project)

	claudeConfigPath := expandMCPGlobalConfigPath(t, homeDir, aggregate.ClaudeGlobalMCPConfigPath)
	codexConfigPath := expandMCPGlobalConfigPath(t, homeDir, aggregate.CodexGlobalMCPConfigPath)
	openCodeConfigPath := expandMCPGlobalConfigPath(t, homeDir, aggregate.OpenCodeGlobalMCPConfigPath)
	antigravityConfigPath := expandMCPGlobalConfigPath(
		t,
		homeDir,
		aggregate.AntigravityGlobalMCPConfigPath,
	)
	globalConfigPaths := []string{
		claudeConfigPath,
		codexConfigPath,
		openCodeConfigPath,
		antigravityConfigPath,
	}
	applyArgs := []string{
		"apply",
		"--manifest", project.manifestPath,
		"--target", "claude-code",
		"--target", "codex",
		"--target", "opencode",
		"--target", "antigravity-cli",
		"--yes",
		"--json",
	}

	exitCode, stdout, stderr := runMCPCLI(t, applyArgs...)
	if exitCode != 1 || stderr != "" {
		t.Fatalf("missing-source apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	if !payload.HasErrors || payload.ActionCount != 0 || len(payload.Errors) != 1 ||
		!strings.Contains(payload.Errors[0].Message, sharedSource) ||
		strings.Count(payload.Errors[0].Message, sharedSource) != 1 ||
		strings.Contains(payload.Errors[0].Message, ambientSource) {
		t.Fatalf("missing-source payload = %#v", payload)
	}
	for _, path := range append(globalConfigPaths, filepath.Join(project.root, ".daem", "state.json")) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s stat error = %v, want absent after cross-target preflight failure", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(project.root, ".daem", "recovery")); !os.IsNotExist(err) {
		t.Fatalf("recovery stat error = %v, want absent after cross-target preflight failure", err)
	}

	t.Setenv(sharedSource, secretCanary)
	exitCode, stdout, stderr = runMCPCLI(t, applyArgs...)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("cross-target apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	payload = clijson.DecodeApplyResult(t, []byte(stdout))
	if payload.HasErrors || payload.ActionCount != 4 {
		t.Fatalf("cross-target payload = %#v, want four committed projections", payload)
	}

	configs := make(map[string][]byte, len(globalConfigPaths))
	for _, path := range globalConfigPaths {
		configs[path] = testkit.ReadFile(t, path)
	}
	assertCrossTargetMCPEnvironmentProjection(
		t,
		configs[claudeConfigPath],
		claudeServerID,
		`"TOKEN"`,
		"${"+sharedSource+"}",
	)
	assertCrossTargetMCPEnvironmentProjection(
		t,
		configs[codexConfigPath],
		codexServerID,
		"env_vars",
		sharedSource,
	)
	assertCrossTargetMCPEnvironmentProjection(
		t,
		configs[openCodeConfigPath],
		openCodeServerID,
		"{env:"+sharedSource+"}",
		`"environment"`,
	)
	antigravityConfig := string(configs[antigravityConfigPath])
	if !strings.Contains(antigravityConfig, ambientServerID) ||
		strings.Contains(antigravityConfig, ambientSource) ||
		strings.Contains(antigravityConfig, `"env"`) {
		t.Fatalf("Antigravity ambient projection = %s", antigravityConfig)
	}

	statePath := filepath.Join(project.root, ".daem", "state.json")
	for label, content := range map[string]string{
		"stdout":   stdout,
		"lockfile": string(testkit.ReadFile(t, project.lockfilePath)),
		"state":    string(testkit.ReadFile(t, statePath)),
	} {
		if strings.Contains(content, secretCanary) {
			t.Fatalf("%s persisted cross-target environment value", label)
		}
	}
	for path, content := range configs {
		if strings.Contains(string(content), secretCanary) {
			t.Fatalf("%s persisted cross-target environment value", path)
		}
	}
	assertMCPRecoveryDirectoryEmpty(t, filepath.Join(project.root, ".daem", "recovery"))

	beforeState := testkit.ReadFile(t, statePath)
	if err := os.Unsetenv(sharedSource); err != nil {
		t.Fatalf("unset %s: %v", sharedSource, err)
	}
	exitCode, stdout, stderr = runMCPCLI(t, applyArgs...)
	if exitCode != 1 || stderr != "" {
		t.Fatalf("stale-source apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	payload = clijson.DecodeApplyResult(t, []byte(stdout))
	if !payload.HasErrors || payload.ActionCount != 0 || len(payload.Errors) != 1 ||
		!strings.Contains(payload.Errors[0].Message, sharedSource) {
		t.Fatalf("stale-source payload = %#v", payload)
	}
	for path, before := range configs {
		if got := testkit.ReadFile(t, path); !slices.Equal(got, before) {
			t.Fatalf("stale-source preflight changed %s", path)
		}
	}
	if got := testkit.ReadFile(t, statePath); !slices.Equal(got, beforeState) {
		t.Fatal("stale-source preflight changed state")
	}
	assertMCPRecoveryDirectoryEmpty(t, filepath.Join(project.root, ".daem", "recovery"))
}

func TestMCPPublicCLIApplyDelegatedRouteMissingEnvDoesNotLaunchRunner(t *testing.T) {
	missingEnvName := "DAEM_TEST_MISSING_ENV_REF"
	unsetEnvForMCPDelegateTest(t, missingEnvName)
	canary := execcheck.New(t, "must-not-run-daem-test")
	project := newMCPCLIProject(t)
	spec := mcpManifestSpec{
		Command: "must-not-run-daem-test",
		Args:    []string{"--serve", "context7"},
		Env:     map[string]string{"API_TOKEN": missingEnvName},
	}
	writeMCPManifest(t, project.root, spec)
	runMCPLock(t, project)

	exitCode, stdout, stderr := runMCPCLI(
		t,
		"apply",
		"--manifest",
		project.manifestPath,
		"--target",
		"claude-code",
		"--yes",
		"--json",
	)
	if exitCode != 1 || stderr != "" {
		t.Fatalf("missing-env apply exitCode=%d stdout=%q stderr=%q, want preflight failure JSON only", exitCode, stdout, stderr)
	}
	payload := clijson.DecodeApplyResult(t, []byte(stdout))
	if !payload.HasErrors || len(payload.Errors) != 1 ||
		!strings.Contains(payload.Errors[0].Message, missingEnvName) {
		t.Fatalf("payload errors = %#v, want missing environment source", payload.Errors)
	}
	if payload.ActionCount != 0 || len(payload.Actions) != 1 ||
		len(payload.DelegateActions) != 1 || len(payload.DelegateAttempts) != 0 {
		t.Fatalf(
			"payload = %#v, want disclosed plan but no committed action or delegate attempt",
			payload,
		)
	}
	assertMCPDelegateActionDisclosure(t, payload.DelegateActions[0], "scheduled", "allow", true, "plain", spec)
	execcheck.AssertClean(t, canary, "missing env attempt")
	for _, path := range []string{
		filepath.Join(project.root, ".mcp.json"),
		filepath.Join(project.root, ".daem", "state.json"),
		filepath.Join(project.root, ".daem", "recovery"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s stat error = %v, want absent after preflight failure", path, err)
		}
	}
}

func writeCrossTargetGlobalMCPEnvironmentManifest(
	t *testing.T,
	root string,
	sharedSource string,
	ambientSource string,
) {
	t.Helper()
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["claude-code", "codex", "opencode", "antigravity-cli"]

[[mcp_server]]
name = "claude-global"
targets = ["claude-code"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@example/claude-mcp"]
env = { TOKEN = { from_env = "`+sharedSource+`" } }

[[mcp_server]]
name = "codex-global"
targets = ["codex"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@example/codex-mcp"]
env = { `+sharedSource+` = { from_env = "`+sharedSource+`" } }

[[mcp_server]]
name = "opencode-global"
targets = ["opencode"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@example/opencode-mcp"]
env = { TOKEN = { from_env = "`+sharedSource+`" } }

[[mcp_server]]
name = "antigravity-global"
targets = ["antigravity-cli"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@example/antigravity-mcp"]
env = { `+ambientSource+` = { from_env = "`+ambientSource+`" } }
`)
}

func expandMCPGlobalConfigPath(t *testing.T, homeDir string, path string) string {
	t.Helper()
	if !strings.HasPrefix(path, "~/") {
		t.Fatalf("global MCP config path %q is not home-relative", path)
	}
	return filepath.Join(homeDir, strings.TrimPrefix(path, "~/"))
}

func assertCrossTargetMCPEnvironmentProjection(
	t *testing.T,
	content []byte,
	serverID string,
	requiredFragments ...string,
) {
	t.Helper()
	rendered := string(content)
	for _, fragment := range append([]string{serverID}, requiredFragments...) {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("projection for %s is missing %q: %s", serverID, fragment, rendered)
		}
	}
}

func assertMCPRecoveryDirectoryEmpty(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read recovery directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("recovery entries = %#v, want empty", entries)
	}
}

func TestMCPPublicCLICodexGlobalApplyPreflightsSameNameEnvironment(t *testing.T) {
	const sourceName = "DAEM_TEST_CODEX_GLOBAL_TOKEN"

	t.Run("missing blocks before mutation", func(t *testing.T) {
		unsetEnvForMCPDelegateTest(t, sourceName)
		project := newMCPCLIProject(t)
		homeDir := filepath.Join(project.root, "host-home")
		t.Setenv("HOME", homeDir)
		writeMCPManifest(t, project.root, mcpManifestSpec{
			Target:  "codex",
			Scope:   "global",
			Command: "npx",
			Args:    []string{"-y", "@example/mcp-server"},
			Env:     map[string]string{sourceName: sourceName},
		})
		runMCPLock(t, project)

		exitCode, stdout, stderr := runMCPCLI(
			t,
			"apply",
			"--manifest",
			project.manifestPath,
			"--target",
			"codex",
			"--yes",
			"--json",
		)
		if exitCode != 1 || stderr != "" {
			t.Fatalf("missing-env apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
		payload := clijson.DecodeApplyResult(t, []byte(stdout))
		if !payload.HasErrors || len(payload.Errors) != 1 ||
			!strings.Contains(payload.Errors[0].Message, sourceName) {
			t.Fatalf("payload errors = %#v, want missing Codex environment source", payload.Errors)
		}
		if payload.ActionCount != 0 {
			t.Fatalf("action_count = %d, want no committed action", payload.ActionCount)
		}
		for _, path := range []string{
			filepath.Join(homeDir, strings.TrimPrefix(aggregate.CodexGlobalMCPConfigPath, "~/")),
			filepath.Join(project.root, ".daem", "state.json"),
			filepath.Join(project.root, ".daem", "recovery"),
		} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("%s stat error = %v, want absent after preflight failure", path, err)
			}
		}
	})

	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "present empty is admitted"},
		{name: "present value is not persisted", value: "codex-secret-canary"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(sourceName, test.value)
			project := newMCPCLIProject(t)
			homeDir := filepath.Join(project.root, "host-home")
			t.Setenv("HOME", homeDir)
			writeMCPManifest(t, project.root, mcpManifestSpec{
				Target:  "codex",
				Scope:   "global",
				Command: "npx",
				Args:    []string{"-y", "@example/mcp-server"},
				Env:     map[string]string{sourceName: sourceName},
			})
			runMCPLock(t, project)

			exitCode, stdout, stderr := runMCPCLI(
				t,
				"apply",
				"--manifest",
				project.manifestPath,
				"--target",
				"codex",
				"--yes",
				"--json",
			)
			if exitCode != 0 || stderr != "" {
				t.Fatalf("apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			configPath := filepath.Join(
				homeDir,
				strings.TrimPrefix(aggregate.CodexGlobalMCPConfigPath, "~/"),
			)
			config := string(testkit.ReadFile(t, configPath))
			if !strings.Contains(config, `env_vars = ["`+sourceName+`"]`) {
				t.Fatalf("Codex config = %q, want canonical env_vars name", config)
			}
			if strings.Contains(config, "\nenv =") {
				t.Fatalf("Codex config = %q, want no literal environment table", config)
			}
			statePath := filepath.Join(project.root, ".daem", "state.json")
			state := string(testkit.ReadFile(t, statePath))
			if test.value != "" {
				for label, content := range map[string]string{
					"stdout":   stdout,
					"lockfile": string(testkit.ReadFile(t, project.lockfilePath)),
					"config":   config,
					"state":    state,
				} {
					if strings.Contains(content, test.value) {
						t.Fatalf("%s persisted resolved Codex environment value", label)
					}
				}
			}
		})
	}
}

func TestMCPPublicCLIAntigravityGlobalApplyPreflightsSameNameAmbientEnvironment(t *testing.T) {
	const sourceName = "DAEM_TEST_ANTIGRAVITY_GLOBAL_TOKEN"

	t.Run("missing blocks before mutation", func(t *testing.T) {
		unsetEnvForMCPDelegateTest(t, sourceName)
		project := newMCPCLIProject(t)
		homeDir := filepath.Join(project.root, "host-home")
		t.Setenv("HOME", homeDir)
		writeMCPManifest(t, project.root, mcpManifestSpec{
			Target:  "antigravity-cli",
			Scope:   "global",
			Command: "npx",
			Args:    []string{"-y", "@example/mcp-server"},
			Env:     map[string]string{sourceName: sourceName},
		})
		runMCPLock(t, project)

		lockfile := string(testkit.ReadFile(t, project.lockfilePath))
		if !strings.Contains(lockfile, `mcp_environment_sources = ["`+sourceName+`"]`) {
			t.Fatalf("lockfile = %q, want Antigravity ambient source", lockfile)
		}
		exitCode, stdout, stderr := runMCPCLI(
			t,
			"apply",
			"--manifest",
			project.manifestPath,
			"--target",
			"antigravity-cli",
			"--yes",
			"--json",
		)
		if exitCode != 1 || stderr != "" {
			t.Fatalf("missing-env apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
		payload := clijson.DecodeApplyResult(t, []byte(stdout))
		if !payload.HasErrors || payload.ActionCount != 0 || len(payload.Errors) != 1 ||
			!strings.Contains(payload.Errors[0].Message, sourceName) {
			t.Fatalf("payload = %#v, want missing Antigravity environment source", payload)
		}
		for _, path := range []string{
			filepath.Join(
				homeDir,
				strings.TrimPrefix(aggregate.AntigravityGlobalMCPConfigPath, "~/"),
			),
			filepath.Join(project.root, ".daem", "state.json"),
			filepath.Join(project.root, ".daem", "recovery"),
		} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("%s stat error = %v, want absent after preflight failure", path, err)
			}
		}
	})

	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "present empty is admitted"},
		{name: "present value remains runtime only", value: "antigravity-secret-canary"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(sourceName, test.value)
			project := newMCPCLIProject(t)
			homeDir := filepath.Join(project.root, "host-home")
			t.Setenv("HOME", homeDir)
			writeMCPManifest(t, project.root, mcpManifestSpec{
				Target:  "antigravity-cli",
				Scope:   "global",
				Command: "npx",
				Args:    []string{"-y", "@example/mcp-server"},
				Env:     map[string]string{sourceName: sourceName},
			})
			runMCPLock(t, project)

			exitCode, stdout, stderr := runMCPCLI(
				t,
				"apply",
				"--manifest",
				project.manifestPath,
				"--target",
				"antigravity-cli",
				"--yes",
				"--json",
			)
			if exitCode != 0 || stderr != "" {
				t.Fatalf("apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			configPath := filepath.Join(
				homeDir,
				strings.TrimPrefix(aggregate.AntigravityGlobalMCPConfigPath, "~/"),
			)
			config := testkit.ReadFile(t, configPath)
			entry, present, err := mcpcodec.ExtractAntigravityGlobalMCPServerProjection(config, "context7")
			if err != nil || !present {
				t.Fatalf("Antigravity projection = %#v, present=%t, err=%v", entry, present, err)
			}
			if entry.Command != "npx" || len(entry.Args) != 2 {
				t.Fatalf("Antigravity projection = %#v, want command/args", entry)
			}
			for label, content := range map[string]string{
				"stdout":   stdout,
				"lockfile": string(testkit.ReadFile(t, project.lockfilePath)),
				"config":   string(config),
				"state":    string(testkit.ReadFile(t, filepath.Join(project.root, ".daem", "state.json"))),
			} {
				if test.value != "" && strings.Contains(content, test.value) {
					t.Fatalf("%s persisted resolved Antigravity environment value", label)
				}
				if label == "config" && (strings.Contains(content, sourceName) || strings.Contains(content, `"env"`)) {
					t.Fatalf("Antigravity config materialized ambient environment metadata: %s", content)
				}
			}

			beforeConfig := append([]byte(nil), config...)
			beforeState := testkit.ReadFile(t, filepath.Join(project.root, ".daem", "state.json"))
			if err := os.Unsetenv(sourceName); err != nil {
				t.Fatalf("unset %s: %v", sourceName, err)
			}
			exitCode, stdout, stderr = runMCPCLI(
				t,
				"apply",
				"--manifest",
				project.manifestPath,
				"--target",
				"antigravity-cli",
				"--yes",
				"--json",
			)
			if exitCode != 1 || stderr != "" {
				t.Fatalf("fresh preflight exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			payload := clijson.DecodeApplyResult(t, []byte(stdout))
			if !payload.HasErrors || payload.ActionCount != 0 ||
				len(payload.Errors) != 1 ||
				!strings.Contains(payload.Errors[0].Message, sourceName) {
				t.Fatalf("fresh preflight payload = %#v", payload)
			}
			if got := testkit.ReadFile(t, configPath); !slices.Equal(got, beforeConfig) {
				t.Fatalf("fresh preflight changed Antigravity config:\ngot  %s\nwant %s", got, beforeConfig)
			}
			if got := testkit.ReadFile(t, filepath.Join(project.root, ".daem", "state.json")); !slices.Equal(got, beforeState) {
				t.Fatalf("fresh preflight changed state:\ngot  %s\nwant %s", got, beforeState)
			}
		})
	}
}

func TestMCPPublicCLIClaudeGlobalApplyPreflightsAliasedEnvironment(t *testing.T) {
	const (
		childName  = "API_TOKEN"
		sourceName = "DAEM_TEST_CLAUDE_GLOBAL_TOKEN"
	)

	t.Run("missing blocks before mutation", func(t *testing.T) {
		unsetEnvForMCPDelegateTest(t, sourceName)
		project := newMCPCLIProject(t)
		homeDir := filepath.Join(project.root, "host-home")
		t.Setenv("HOME", homeDir)
		writeMCPManifest(t, project.root, mcpManifestSpec{
			Target:  "claude-code",
			Scope:   "global",
			Command: "npx",
			Args:    []string{"-y", "@example/mcp-server"},
			Env:     map[string]string{childName: sourceName},
		})
		runMCPLock(t, project)

		exitCode, stdout, stderr := runMCPCLI(
			t,
			"apply",
			"--manifest",
			project.manifestPath,
			"--target",
			"claude-code",
			"--yes",
			"--json",
		)
		if exitCode != 1 || stderr != "" {
			t.Fatalf("missing-env apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
		payload := clijson.DecodeApplyResult(t, []byte(stdout))
		if !payload.HasErrors || len(payload.Errors) != 1 ||
			!strings.Contains(payload.Errors[0].Message, sourceName) {
			t.Fatalf("payload errors = %#v, want missing Claude environment source", payload.Errors)
		}
		if payload.ActionCount != 0 {
			t.Fatalf("action_count = %d, want no committed action", payload.ActionCount)
		}
		for _, path := range []string{
			filepath.Join(homeDir, ".claude.json"),
			filepath.Join(project.root, ".daem", "state.json"),
			filepath.Join(project.root, ".daem", "recovery"),
		} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("%s stat error = %v, want absent after preflight failure", path, err)
			}
		}
	})

	t.Run("current row still requires source", func(t *testing.T) {
		t.Setenv(sourceName, "initial-value")
		project := newMCPCLIProject(t)
		homeDir := filepath.Join(project.root, "host-home")
		t.Setenv("HOME", homeDir)
		writeMCPManifest(t, project.root, mcpManifestSpec{
			Target:  "claude-code",
			Scope:   "global",
			Command: "npx",
			Args:    []string{"-y", "@example/mcp-server"},
			Env:     map[string]string{childName: sourceName},
		})
		runMCPLock(t, project)
		exitCode, stdout, stderr := runMCPCLI(
			t,
			"apply",
			"--manifest",
			project.manifestPath,
			"--target",
			"claude-code",
			"--yes",
			"--json",
		)
		if exitCode != 0 || stderr != "" {
			t.Fatalf("initial apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
		configPath := filepath.Join(homeDir, ".claude.json")
		statePath := filepath.Join(project.root, ".daem", "state.json")
		beforeConfig := string(testkit.ReadFile(t, configPath))
		beforeState := string(testkit.ReadFile(t, statePath))
		unsetEnvForMCPDelegateTest(t, sourceName)

		exitCode, stdout, stderr = runMCPCLI(
			t,
			"apply",
			"--manifest",
			project.manifestPath,
			"--target",
			"claude-code",
			"--yes",
			"--json",
		)
		if exitCode != 1 || stderr != "" {
			t.Fatalf("current-row apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
		payload := clijson.DecodeApplyResult(t, []byte(stdout))
		if !payload.HasErrors || payload.ActionCount != 0 ||
			len(payload.Errors) != 1 ||
			!strings.Contains(payload.Errors[0].Message, sourceName) {
			t.Fatalf("current-row payload = %#v, want missing-source preflight failure", payload)
		}
		if config := string(testkit.ReadFile(t, configPath)); config != beforeConfig {
			t.Fatalf("current-row preflight changed config:\ngot  %s\nwant %s", config, beforeConfig)
		}
		if state := string(testkit.ReadFile(t, statePath)); state != beforeState {
			t.Fatalf("current-row preflight changed state:\ngot  %s\nwant %s", state, beforeState)
		}
	})

	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "present empty is admitted"},
		{name: "present value is not persisted", value: "claude-global-secret-canary"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(sourceName, test.value)
			project := newMCPCLIProject(t)
			homeDir := filepath.Join(project.root, "host-home")
			t.Setenv("HOME", homeDir)
			writeMCPManifest(t, project.root, mcpManifestSpec{
				Target:  "claude-code",
				Scope:   "global",
				Command: "npx",
				Args:    []string{"-y", "@example/mcp-server"},
				Env:     map[string]string{childName: sourceName},
			})
			runMCPLock(t, project)

			exitCode, stdout, stderr := runMCPCLI(
				t,
				"apply",
				"--manifest",
				project.manifestPath,
				"--target",
				"claude-code",
				"--yes",
				"--json",
			)
			if exitCode != 0 || stderr != "" {
				t.Fatalf("apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			configPath := filepath.Join(homeDir, ".claude.json")
			config := testkit.ReadFile(t, configPath)
			entry, present, err := mcpcodec.ExtractClaudeGlobalMCPServerProjection(config, "context7")
			if err != nil || !present {
				t.Fatalf("Claude global projection = %#v, present=%t, err=%v", entry, present, err)
			}
			if len(entry.Env) != 1 || entry.Env[childName] != "${"+sourceName+"}" {
				t.Fatalf("Claude global env = %#v, want exact native reference", entry.Env)
			}
			state := testkit.ReadFile(t, filepath.Join(project.root, ".daem", "state.json"))
			if test.value != "" {
				for label, content := range map[string]string{
					"stdout":   stdout,
					"lockfile": string(testkit.ReadFile(t, project.lockfilePath)),
					"config":   string(config),
					"state":    string(state),
				} {
					if strings.Contains(content, test.value) {
						t.Fatalf("%s persisted resolved Claude environment value", label)
					}
				}
			}
		})
	}
}
