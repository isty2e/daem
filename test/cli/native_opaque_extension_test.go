//go:build darwin || linux

package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
)

type nativeOpaqueHost struct {
	target           target.Target
	scope            target.Scope
	carrier, command string
}

var nativeOpaqueHosts = []nativeOpaqueHost{
	{target.TargetClaudeCode, target.ScopeProject, "claude-code-plugin", "claude"},
	{target.TargetClaudeCode, target.ScopeGlobal, "claude-code-plugin", "claude"},
	{target.TargetAntigravityCLI, target.ScopeGlobal, "antigravity-cli-plugin", "agy"},
}

type nativeOpaqueFixture struct {
	workspace, manifest, keyFile, calls, keyBefore, launchState, launchRegistry, hostFile, present, absent, bundle string
	host                                                                                                           nativeOpaqueHost
}

func newNativeOpaqueFixture(t *testing.T, host nativeOpaqueHost, nativeScope target.Scope) nativeOpaqueFixture {
	t.Helper()
	return newNativeOpaqueFixtureWithHistory(t, host, nativeScope, true)
}

func newNativeOpaqueFixtureWithHistory(t *testing.T, host nativeOpaqueHost, nativeScope target.Scope, installedBefore bool) nativeOpaqueFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	setNativeRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	fixture := nativeOpaqueFixture{workspace: workspace, manifest: filepath.Join(workspace, "daem.toml"), calls: filepath.Join(root, "calls"), keyBefore: filepath.Join(root, "key-before.toml"), launchState: filepath.Join(root, "launch-state.json"), launchRegistry: filepath.Join(root, "launch-registry.json"), host: host}
	fixture.keyFile = filepath.Join(workspace, ".codex", "config.toml")
	if nativeScope == target.ScopeGlobal {
		fixture.keyFile = filepath.Join(os.Getenv("CODEX_HOME"), "config.toml")
	}
	testkit.WriteFile(t, filepath.Dir(fixture.keyFile), filepath.Base(fixture.keyFile), "foreign=true\n# retained native note\n")
	source := "marketplace"
	if host.target == target.TargetClaudeCode {
		fixture.hostFile = filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "plugins", "installed_plugins.json")
		hostScope := "project"
		if host.scope == target.ScopeGlobal {
			hostScope = "user"
		}
		row := map[string]string{"scope": hostScope}
		if host.scope == target.ScopeProject {
			row["projectPath"] = workspace
		}
		content, err := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{"tools@official": []map[string]string{row}}})
		if err != nil {
			t.Fatal(err)
		}
		fixture.present, fixture.absent = string(content), `{"version":2,"plugins":{}}`
	} else {
		source = "host_source"
		config := filepath.Join(os.Getenv("HOME"), ".gemini", "config")
		fixture.hostFile = filepath.Join(config, "import_manifest.json")
		fixture.bundle = filepath.Join(config, "plugins", "tools")
		fixture.present, fixture.absent = `{"imports":[{"name":"tools","source":"antigravity"}]}`, `{"imports":[]}`
	}
	declaration := fmt.Sprintf("version=1\ntargets=[%q,'codex']\n[[extension]]\nid='tools'\ntargets=[%q]\ncarrier=%q\nscope=%q\nsource={%s='tools@official'}\n", host.target, host.target, host.carrier, host.scope, source)
	testkit.WriteFile(t, workspace, "daem.toml", declaration)
	testkit.WriteFile(t, filepath.Dir(fixture.hostFile), filepath.Base(fixture.hostFile), fixture.absent)
	testkit.WriteFile(t, root, "host-present.json", fixture.present)
	testkit.WriteFile(t, root, "host-absent.json", fixture.absent)
	script := `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '2.1.0\n'; exit 0; fi
[ "$1" = plugin ] || exit 97
case "$2" in install|update|uninstall) ;; *) exit 97 ;; esac
printf '%s\n' "$2" >> "$DAEM_TEST_OPAQUE_CALLS"
if [ "$DAEM_TEST_OPAQUE_PHASE" = native ]; then
  /bin/cp "$DAEM_TEST_OPAQUE_KEY" "$DAEM_TEST_OPAQUE_KEY_BEFORE"
  /bin/cp .daem/state.json "$DAEM_TEST_OPAQUE_STATE"
  /bin/cp "$DAEM_TEST_OPAQUE_REGISTRY" "$DAEM_TEST_OPAQUE_LAUNCH_REGISTRY"
fi
if [ "$2" = uninstall ]; then
  /bin/cp "$DAEM_TEST_OPAQUE_ABSENT" "$DAEM_TEST_OPAQUE_HOST"
  if [ -n "$DAEM_TEST_OPAQUE_BUNDLE" ]; then rm -rf "$DAEM_TEST_OPAQUE_BUNDLE"; fi
else
  /bin/cp "$DAEM_TEST_OPAQUE_PRESENT" "$DAEM_TEST_OPAQUE_HOST"
  if [ -n "$DAEM_TEST_OPAQUE_BUNDLE" ]; then
    mkdir -p "$DAEM_TEST_OPAQUE_BUNDLE"
    printf '%s\n' '{"name":"tools","version":"1.0.0"}' > "$DAEM_TEST_OPAQUE_BUNDLE/plugin.json"
  fi
fi
if [ "$DAEM_TEST_OPAQUE_PHASE" = native ]; then
  case "$DAEM_TEST_OPAQUE_FAULT" in
    key) printf '%s\n' "model='changed'" 'foreign=true' '# retained native note' > "$DAEM_TEST_OPAQUE_KEY" ;;
    outside) printf '\n[unexpected]\nvalue=true\n' >> "$DAEM_TEST_OPAQUE_KEY" ;;
    mode|mode-command) /bin/chmod 666 "$DAEM_TEST_OPAQUE_KEY" ;;
    command) exit 23 ;;
    terminate) /bin/chmod 666 "$DAEM_TEST_OPAQUE_KEY"; kill -KILL "$DAEM_TEST_OPAQUE_PARENT_PID"; exit 0 ;;
  esac
  [ "$DAEM_TEST_OPAQUE_FAULT" != mode-command ] || exit 23
fi
printf '{}\n'
`
	bin := filepath.Join(root, "bin")
	testkit.WriteFile(t, bin, host.command, script)
	if err := os.Chmod(filepath.Join(bin, host.command), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	paths, err := daempaths.Resolve(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"DAEM_TEST_OPAQUE_CALLS": fixture.calls, "DAEM_TEST_OPAQUE_KEY": fixture.keyFile, "DAEM_TEST_OPAQUE_KEY_BEFORE": fixture.keyBefore, "DAEM_TEST_OPAQUE_STATE": fixture.launchState, "DAEM_TEST_OPAQUE_REGISTRY": paths.OwnershipRegistryPath, "DAEM_TEST_OPAQUE_LAUNCH_REGISTRY": fixture.launchRegistry, "DAEM_TEST_OPAQUE_HOST": fixture.hostFile, "DAEM_TEST_OPAQUE_PRESENT": filepath.Join(root, "host-present.json"), "DAEM_TEST_OPAQUE_ABSENT": filepath.Join(root, "host-absent.json"), "DAEM_TEST_OPAQUE_BUNDLE": fixture.bundle, "DAEM_TEST_OPAQUE_PHASE": "baseline", "DAEM_TEST_OPAQUE_FAULT": ""} {
		t.Setenv(name, value)
	}
	fixture.require(t, "lock")
	if installedBefore {
		fixture.require(t, "apply", "--yes")
		testkit.AssertFileContent(t, fixture.calls, "install\n")
	}
	testkit.WriteFile(t, filepath.Dir(fixture.hostFile), filepath.Base(fixture.hostFile), fixture.absent)
	if fixture.bundle != "" {
		if err := os.RemoveAll(fixture.bundle); err != nil {
			t.Fatal(err)
		}
	}
	testkit.WriteFile(t, workspace, "preferences.toml", "model='native-model'\n")
	declaration += fmt.Sprintf("\n[[host_config]]\nid='preferences'\ntargets=['codex']\nscope=%q\npath='config.toml'\nsource=%q\nownership='keys'\n", nativeScope, filepath.Join(workspace, "preferences.toml"))
	testkit.WriteFile(t, workspace, "daem.toml", declaration)
	t.Setenv("DAEM_TEST_OPAQUE_PHASE", "native")
	fixture.require(t, "lock")
	return fixture
}

func (fixture nativeOpaqueFixture) run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := testkit.RunVerboseCLIWithOptions(append(args, "--manifest", fixture.manifest), clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr})
	return code, stdout.String(), stderr.String()
}

func (fixture nativeOpaqueFixture) require(t *testing.T, args ...string) {
	t.Helper()
	if code, stdout, stderr := fixture.run(args...); code != 0 {
		t.Fatalf("opaque CLI %q failed: %d %s %s", args, code, stdout, stderr)
	}
}

func TestNativeOpaqueExtensionsEstablishAndProtectBothKeyScopes(t *testing.T) {
	for _, host := range nativeOpaqueHosts {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(fmt.Sprintf("%s/%s/native-%s", host.target, host.scope, scope), func(t *testing.T) {
				fixture := newNativeOpaqueFixture(t, host, scope)
				fixture.require(t, "apply", "--yes", "--json")
				if !strings.Contains(string(testkit.ReadFile(t, fixture.keyBefore)), "native-model") {
					t.Fatal("opaque consumer started before its selected key settled")
				}
				snapshot, err := statefile.Decode(testkit.ReadFile(t, fixture.launchState))
				if err != nil || len(snapshot.PendingCarrierInstalls()) != 1 {
					t.Fatal("opaque invocation has no durable install correlation", err)
				}
				store, err := ownershipstore.New(fixture.launchRegistry)
				if err != nil {
					t.Fatal(err)
				}
				registry, err := store.LoadProtectionRegistry(t.Context())
				if err != nil || len(registry.PendingProtections()) != 1 || registry.PendingProtections()[0].Phase() != ownership.ProtectionBound {
					t.Fatal("opaque command was released without Bound protection", err)
				}
				fixture.require(t, "status", "--check", "--json")
				fixture.require(t, "apply", "--yes")
				testkit.AssertFileContent(t, fixture.calls, "install\ninstall\n")
			})
		}
	}
}

func TestNativeOpaquePreparedExecutionUsesSameBoundary(t *testing.T) {
	fixture := newNativeOpaqueFixture(t, nativeOpaqueHosts[2], target.ScopeGlobal)
	prepared, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: fixture.manifest})
	if err != nil {
		t.Fatal("prepare opaque execution", err)
	}
	defer prepared.Close()
	result, err := applyworkflow.ExecuteWithOptions(t.Context(), prepared, applyworkflow.ExecuteOptions{})
	if err != nil {
		t.Fatalf("execute opaque preparation: %v (%#v), started=%t", err, err, result.ExecutionAttempted)
	}
	fixture.require(t, "status", "--check", "--json")
	fixture.require(t, "remove", "extension", "tools")
	fixture.require(t, "lock")
	removal, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: fixture.manifest})
	if err != nil {
		t.Fatal("prepare opaque removal", err)
	}
	defer removal.Close()
	removed, err := applyworkflow.ExecuteWithOptions(t.Context(), removal, applyworkflow.ExecuteOptions{})
	if err != nil {
		t.Fatalf("execute opaque removal: %v (%#v), started=%t", err, err, removed.ExecutionAttempted)
	}
}

func TestNativeOpaqueFirstInstallWithKeys(t *testing.T) {
	for _, host := range nativeOpaqueHosts {
		t.Run(fmt.Sprintf("%s/%s", host.target, host.scope), func(t *testing.T) {
			fixture := newNativeOpaqueFixtureWithHistory(t, host, target.ScopeGlobal, false)
			fixture.require(t, "apply", "--yes", "--json")
			fixture.require(t, "status", "--check", "--json")
			fixture.require(t, "apply", "--yes")
			testkit.AssertFileContent(t, fixture.calls, "install\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}

func TestNativeOpaqueRefreshAndRemovalPreserveLifecycle(t *testing.T) {
	for _, host := range nativeOpaqueHosts {
		for _, retire := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%s/retire=%t", host.target, host.scope, retire), func(t *testing.T) {
				fixture := newNativeOpaqueFixture(t, host, target.ScopeGlobal)
				fixture.require(t, "apply", "--yes")
				fixture.require(t, "refresh", "extension", "tools", "--dry-run", "--json")
				fixture.require(t, "refresh", "extension", "tools", "--yes", "--json")
				store, err := ownershipstore.New(fixture.launchRegistry)
				if err != nil {
					t.Fatal(err)
				}
				registry, err := store.LoadProtectionRegistry(t.Context())
				if err != nil || len(registry.PendingProtections()) != 1 || registry.PendingProtections()[0].Phase() != ownership.ProtectionBound {
					t.Fatal("refresh started without Bound protection", err)
				}
				fixture.require(t, "remove", "extension", "tools")
				if retire {
					testkit.WriteFile(t, fixture.workspace, "daem.toml", fmt.Sprintf("version=1\ntargets=[%q,'codex']\n", host.target))
				}
				fixture.require(t, "lock")
				fixture.require(t, "apply", "--yes", "--json")
				paths, err := daempaths.Resolve(fixture.manifest)
				if err != nil {
					t.Fatal(err)
				}
				after, err := statefile.Decode(testkit.ReadFile(t, paths.StatefilePath))
				if err != nil || after.ContainsHostConfig() == retire || len(after.PendingCarrierRemovals()) != 0 {
					t.Fatal("removal lost the complete native/state settlement", err)
				}
				fixture.require(t, "status", "--check", "--json")
				fixture.require(t, "apply", "--yes")
				refresh := "update"
				if host.target == target.TargetAntigravityCLI {
					refresh = "install"
				}
				testkit.AssertFileContent(t, fixture.calls, "install\ninstall\n"+refresh+"\nuninstall\n")
				testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
			})
		}
	}
}

func TestNativeOpaqueFailuresDoNotRepairOrLeaveExpandedAccess(t *testing.T) {
	for _, host := range nativeOpaqueHosts {
		for _, fault := range []string{"key", "outside", "mode", "command", "mode-command"} {
			t.Run(fmt.Sprintf("%s/%s/%s", host.target, host.scope, fault), func(t *testing.T) {
				fixture := newNativeOpaqueFixture(t, host, target.ScopeGlobal)
				t.Setenv("DAEM_TEST_OPAQUE_FAULT", fault)
				if code, stdout, stderr := fixture.run("apply", "--yes", "--json"); code == 0 {
					t.Fatalf("unqualified opaque execution succeeded: %s %s", stdout, stderr)
				}
				info, err := os.Stat(fixture.keyFile)
				if err != nil || info.Mode().Perm()&0o022 != 0 {
					t.Fatal("opaque command left expanded write access", err)
				}
				if fault == "key" && !strings.Contains(string(testkit.ReadFile(t, fixture.keyFile)), "changed") {
					t.Fatal("selected value was silently repaired")
				}
				testkit.AssertFileContent(t, fixture.calls, "install\ninstall\n")
			})
		}
	}
}

func TestNativeOpaqueNamespaceAliasRefusesBeforePrefix(t *testing.T) {
	fixture := newNativeOpaqueFixture(t, nativeOpaqueHosts[1], target.ScopeGlobal)
	alias := filepath.Join(filepath.Dir(fixture.workspace), "host-alias")
	if err := os.Symlink(filepath.Dir(fixture.keyFile), alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", alias)
	if code, _, _ := fixture.run("apply", "--yes", "--json"); code == 0 {
		t.Fatal("unprofiled native/host namespace alias was admitted")
	}
	testkit.AssertFileContent(t, fixture.keyFile, "foreign=true\n# retained native note\n")
	testkit.AssertFileContent(t, fixture.calls, "install\n")
}

func TestNativeOpaqueInterruptedCommandsRecoverWithoutReplay(t *testing.T) {
	for _, host := range []nativeOpaqueHost{nativeOpaqueHosts[1], nativeOpaqueHosts[2]} {
		t.Run(string(host.target), func(t *testing.T) {
			fixture := newNativeOpaqueFixture(t, host, target.ScopeGlobal)
			t.Setenv("DAEM_TEST_OPAQUE_FAULT", "terminate")
			code, stdout, stderr := invokeNativeRecoveryCommand(t, nativeRecoveryFixture{manifest: fixture.manifest, destination: fixture.keyFile}, "native-opaque-command", "apply", "--yes", "--json")
			if code == 0 || code == nativeRecoveryExit {
				t.Fatalf("opaque Apply was not interrupted: %d %s %s", code, stdout, stderr)
			}
			store, err := ownershipstore.New(os.Getenv("DAEM_TEST_OPAQUE_REGISTRY"))
			if err != nil {
				t.Fatal(err)
			}
			registry, err := store.LoadProtectionRegistry(t.Context())
			if err != nil || len(registry.PendingProtections()) != 1 || registry.PendingProtections()[0].Phase() != ownership.ProtectionBound {
				t.Fatal("lost opaque command has no durable Bound protection", err)
			}
			if err := os.Remove(filepath.Join(fixture.workspace, "preferences.toml")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DAEM_TEST_OPAQUE_FAULT", "")
			if host.target == target.TargetClaudeCode {
				otherRoot := filepath.Join(filepath.Dir(fixture.workspace), "other-workspace")
				other := nativeOpaqueFixture{workspace: otherRoot, manifest: filepath.Join(otherRoot, "daem.toml")}
				testkit.WriteFile(t, otherRoot, "daem.toml", "version=1\ntargets=['claude-code']\n[[extension]]\nid='tools'\ncarrier='claude-code-plugin'\nscope='global'\nsource={marketplace='tools@official'}\n")
				other.require(t, "lock")
				other.require(t, "refresh", "extension", "tools", "--dry-run", "--json")
				original := os.Getenv("CLAUDE_CONFIG_DIR")
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Dir(fixture.keyFile))
				testkit.WriteFile(t, filepath.Join(filepath.Dir(fixture.keyFile), "plugins"), "installed_plugins.json", fixture.present)
				code, stdout, stderr := other.run("refresh", "extension", "tools", "--yes", "--json")
				var result struct {
					Result struct {
						Reason    string `json:"reason_code"`
						Attempted bool   `json:"attempted"`
					} `json:"result"`
				}
				if err := json.Unmarshal([]byte(stdout), &result); err != nil || code == 0 || result.Result.Attempted || result.Result.Reason != "native_settings_unqualified" {
					t.Fatal("ordinary Refresh bypassed overlapping foreign protection", err, stdout, stderr)
				}
				t.Setenv("CLAUDE_CONFIG_DIR", original)
			}
			fixture.require(t, "recover", "--dry-run")
			fixture.require(t, "recover", "--yes")
			info, err := os.Stat(fixture.keyFile)
			if err != nil || info.Mode().Perm()&0o022 != 0 {
				t.Fatal("source-independent recovery left broadened access", err)
			}
			testkit.WriteFile(t, fixture.workspace, "preferences.toml", "model='native-model'\n")
			fixture.require(t, "apply", "--yes")
			fixture.require(t, "status", "--check", "--json")
			testkit.AssertFileContent(t, fixture.calls, "install\ninstall\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}
