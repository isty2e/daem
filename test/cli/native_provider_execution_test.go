//go:build darwin || linux

package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output/ownership"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativePiAdapterMCPWithManagedKeys(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, reinstall := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reinstall=%t", scope, reinstall), func(t *testing.T) {
				fixture := newNativePiAdapterFixture(t, scope, reinstall)
				fixture.Require(t, "status", "--json")
				if _, err := applyworkflow.PlanDryRun(t.Context(), applyworkflow.CommandInput{ManifestPath: fixture.Manifest}); err != nil {
					t.Fatal("provider/native schedule admission", err)
				}
				fixture.Require(t, "apply", "--yes", "--json")
				var settings struct {
					Theme    string   `json:"theme"`
					Packages []string `json:"packages"`
				}
				if err := json.Unmarshal(testkit.ReadFile(t, fixture.Settings), &settings); err != nil || settings.Theme != "fixture-theme" {
					t.Fatal("adapter application lost its selected key", err)
				}
				var config struct {
					Servers map[string]struct {
						Args []string `json:"args"`
					} `json:"mcpServers"`
				}
				mcpPath := filepath.Join(filepath.Dir(fixture.Settings), "mcp.json")
				if err := json.Unmarshal(testkit.ReadFile(t, mcpPath), &config); err != nil || len(config.Servers["context7"].Args) != 1 || config.Servers["context7"].Args[0] != "updated-server.js" {
					t.Fatal("adapter MCP did not publish after provider qualification", err)
				}
				expectedCalls := "provider\n"
				if reinstall {
					expectedCalls += "provider\n"
					if row := fixture.LaunchProtection(t); row.Phase() != ownership.ProtectionBound {
						t.Fatal("provider was not bound to its durable protection before release")
					}
					var before struct {
						Theme string `json:"theme"`
					}
					if err := json.Unmarshal(testkit.ReadFile(t, fixture.BeforeCommand), &before); err != nil || before.Theme != "fixture-theme" {
						t.Fatal("provider installation preceded selected key settlement", err)
					}
				}
				fixture.Require(t, "status", "--check", "--json")
				fixture.Require(t, "apply", "--yes", "--json")
				testkit.AssertFileContent(t, fixture.Calls, expectedCalls)
				testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
			})
		}
	}
}

func TestNativePiAdapterProviderWorkflow(t *testing.T) {
	fixture := newNativePiAdapterFixture(t, target.ScopeProject, true)
	planned, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: fixture.Manifest})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyworkflow.ExecuteWithOptions(t.Context(), planned, applyworkflow.ExecuteOptions{}); err != nil {
		t.Fatal("provider workflow", err)
	}
}

func TestNativePiAdapterGlobalPendingCompletionWorkflow(t *testing.T) {
	fixture := newNativePiAdapterFixture(t, target.ScopeGlobal, true)
	t.Setenv("DAEM_TEST_PROVIDER_FAULT", "key")
	planned, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: fixture.Manifest})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyworkflow.ExecuteWithOptions(t.Context(), planned, applyworkflow.ExecuteOptions{}); err == nil {
		t.Fatal("bad key did not stop provider execution")
	}
	t.Setenv("DAEM_TEST_PROVIDER_FAULT", "")
	testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:pi-mcp-adapter@^2.13.0"]}`)
	planned, err = applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: fixture.Manifest})
	if err != nil {
		t.Fatal("global pending completion admission", err)
	}
	if _, err := applyworkflow.ExecuteWithOptions(t.Context(), planned, applyworkflow.ExecuteOptions{}); err != nil {
		t.Fatal("global pending completion execution", err)
	}
}

func TestNativePiAdapterProviderFailuresStopMCPAndRetainActualEffects(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, fault := range []string{"nonzero", "key", "outside", "malformed", "mode", "badversion", "unversioned"} {
			t.Run(string(scope)+"/"+fault, func(t *testing.T) {
				fixture := newNativePiAdapterFixture(t, scope, true)
				mcpPath := filepath.Join(filepath.Dir(fixture.Settings), "mcp.json")
				beforeMCP := testkit.ReadFile(t, mcpPath)
				beforeMode, err := os.Stat(fixture.Settings)
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("DAEM_TEST_PROVIDER_FAULT", fault)
				if code, stdout, stderr := fixture.Run("apply", "--yes", "--json"); code == 0 {
					t.Fatalf("failed provider allowed MCP publication: %s %s", stdout, stderr)
				}
				fixture.LaunchProtection(t)
				testkit.AssertFileContent(t, mcpPath, string(beforeMCP))
				testkit.AssertFileContent(t, os.Getenv("DAEM_TEST_MCP_BEFORE"), string(beforeMCP))
				actual := testkit.ReadFile(t, fixture.Settings)
				if fault == "key" && !bytes.Contains(actual, []byte(`"host-changed"`)) || fault == "outside" && !bytes.Contains(actual, []byte(`"retained":false`)) || fault == "malformed" && string(actual) != "[" {
					t.Fatal("provider qualification silently repaired host content", string(actual))
				}
				afterMode, err := os.Stat(fixture.Settings)
				if err != nil || afterMode.Mode().Perm()&^beforeMode.Mode().Perm() != 0 {
					t.Fatal("provider left widened access bits", err)
				}

				t.Setenv("DAEM_TEST_PROVIDER_FAULT", "")
				packages := `["npm:pi-mcp-adapter@^2.13.0"]`
				if fault == "nonzero" {
					packages = `[]`
				}
				testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", fmt.Sprintf(`{"theme":"fixture-theme","foreign":{"retained":true},"packages":%s}`, packages))
				if fault == "badversion" || fault == "unversioned" {
					testkit.WriteFile(t, filepath.Dir(fixture.Settings), "npm/node_modules/pi-mcp-adapter/package.json", `{"name":"pi-mcp-adapter","version":"2.15.0"}`)
				}
				fixture.Require(t, "apply", "--yes", "--json")
				fixture.Require(t, "status", "--check", "--json")
				expectedCalls := "provider\nprovider\n"
				if fault == "nonzero" {
					expectedCalls += "provider\n"
				}
				testkit.AssertFileContent(t, fixture.Calls, expectedCalls)
				testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
			})
		}
	}
}

func TestNativePiAdapterProviderArtifactOnlyReplay(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newNativePiAdapterFixture(t, scope, false)
			packageRoot := filepath.Join(filepath.Dir(fixture.Settings), "npm", "node_modules", "pi-mcp-adapter")
			if err := os.Remove(filepath.Join(packageRoot, "package.json")); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(packageRoot); err != nil {
				t.Fatal(err)
			}
			fixture.Require(t, "apply", "--yes", "--json")
			fixture.LaunchProtection(t)
			fixture.Require(t, "status", "--check", "--json")
			fixture.Require(t, "apply", "--yes", "--json")
			testkit.AssertFileContent(t, fixture.Calls, "provider\nprovider\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiAdapterProviderSuffixProcessLossDoesNotReplayInstall(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, boundary := range []string{"host", "state"} {
			t.Run(string(scope)+"/"+boundary, func(t *testing.T) {
				fixture := newNativePiAdapterFixture(t, scope, true)
				mcpPath := filepath.Join(filepath.Dir(fixture.Settings), "mcp.json")
				before := testkit.ReadFile(t, mcpPath)
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, nativefixture.RecoveryFixture{Manifest: fixture.Manifest, Destination: mcpPath}, "native-ordinary-"+boundary, "apply", "--yes")
				if code != nativefixture.RecoveryExit {
					t.Fatalf("provider suffix missed process-loss boundary: %d %s %s", code, stdout, stderr)
				}
				fixture.LaunchProtection(t)
				fixture.Require(t, "recover", "--dry-run")
				fixture.Require(t, "recover", "--yes")
				if boundary == "host" {
					testkit.AssertFileContent(t, mcpPath, string(before))
				}
				fixture.Require(t, "apply", "--yes", "--json")
				fixture.Require(t, "status", "--check", "--json")
				testkit.AssertFileContent(t, fixture.Calls, "provider\nprovider\n")
				testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
			})
		}
	}
}

func TestNativePiAdapterProviderSuffixReturnedErrorsKeepAttempt(t *testing.T) {
	for _, boundary := range []string{"return-error", "return-state-error"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newNativePiAdapterFixture(t, target.ScopeProject, true)
			mcpPath := filepath.Join(filepath.Dir(fixture.Settings), "mcp.json")
			before := testkit.ReadFile(t, mcpPath)
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, nativefixture.RecoveryFixture{Manifest: fixture.Manifest, Destination: mcpPath}, "native-ordinary-"+boundary, "apply", "--yes", "--json")
			if code == 0 || code == nativefixture.RecoveryExit {
				t.Fatalf("expected returned MCP publication error: %d %s %s", code, stdout, stderr)
			}
			var result struct {
				ActionCount int `json:"action_count"`
				Attempts    []struct {
					Class    string `json:"result_class"`
					ExitCode *int   `json:"exit_code"`
				} `json:"host_route_attempts"`
			}
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatal(err)
			}
			expectedCount := 1
			if boundary == "return-state-error" {
				expectedCount = 2
			} else {
				testkit.AssertFileContent(t, mcpPath, string(before))
			}
			if result.ActionCount != expectedCount || len(result.Attempts) != 1 || result.Attempts[0].Class != "attempted_observed_present" || result.Attempts[0].ExitCode == nil || *result.Attempts[0].ExitCode != 0 {
				t.Fatalf("MCP failure lost actual retained work or the completed provider: %s", stdout)
			}
			fixture.Require(t, "apply", "--yes", "--json")
			fixture.Require(t, "status", "--check", "--json")
			testkit.AssertFileContent(t, fixture.Calls, "provider\nprovider\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiAdapterFirstInstallWithKeysAndPendingRetry(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, failKey := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/key-failure=%t", scope, failKey), func(t *testing.T) {
				fixture := prepareNativePiAdapterFixture(t, scope)
				declareNativePiAdapterKeys(t, fixture, scope)
				if failKey {
					t.Setenv("DAEM_TEST_PROVIDER_FAULT", "key")
				}
				code, stdout, stderr := fixture.Run("apply", "--yes", "--json")
				if failKey {
					if code == 0 {
						t.Fatal("first provider changed selected key without refusal")
					}
					t.Setenv("DAEM_TEST_PROVIDER_FAULT", "")
					testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:pi-mcp-adapter@^2.13.0"]}`)
					fixture.Require(t, "apply", "--yes", "--json")
				} else if code != 0 {
					t.Fatalf("first protected provider install failed: %d %s %s", code, stdout, stderr)
				}
				fixture.LaunchProtection(t)
				fixture.Require(t, "status", "--check", "--json")
				fixture.Require(t, "apply", "--yes", "--json")
				testkit.AssertFileContent(t, fixture.Calls, "provider\n")
				testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
			})
		}
	}
}

func TestNativePiAdapterProviderWithEmptyKeySelection(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := prepareNativePiAdapterFixture(t, scope)
			declareNativePiAdapterKeys(t, fixture, scope)
			testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{}`)
			fixture.Require(t, "lock")
			t.Setenv("DAEM_TEST_PROVIDER_RESULT", `{"foreign":{"retained":true},"packages":["npm:pi-mcp-adapter@^2.13.0"]}`)
			planned, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: fixture.Manifest})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := applyworkflow.ExecuteWithOptions(t.Context(), planned, applyworkflow.ExecuteOptions{}); err != nil {
				t.Fatal("empty key provider continuation", err)
			}
			fixture.Require(t, "status", "--check", "--json")
			fixture.Require(t, "apply", "--yes", "--json")
			testkit.AssertFileContent(t, fixture.Calls, "provider\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiAdapterProviderPrecedesLastKeyRetirement(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newNativePiAdapterFixture(t, scope, false)
			fixture.Require(t, "apply", "--yes", "--json")
			testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{}`)
			testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":[]}`)
			packageRoot := filepath.Join(filepath.Dir(fixture.Settings), "npm", "node_modules", "pi-mcp-adapter")
			if err := os.Remove(filepath.Join(packageRoot, "package.json")); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(packageRoot); err != nil {
				t.Fatal(err)
			}
			fixture.Require(t, "lock")
			fixture.Require(t, "apply", "--yes", "--json")
			fixture.LaunchProtection(t)
			var before, after map[string]json.RawMessage
			if err := json.Unmarshal(testkit.ReadFile(t, fixture.BeforeCommand), &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(testkit.ReadFile(t, fixture.Settings), &after); err != nil {
				t.Fatal(err)
			}
			if _, present := before["theme"]; !present {
				t.Fatal("provider could not consume the retiring key")
			}
			if _, retained := after["theme"]; retained {
				t.Fatal("retiring key remained after provider qualification")
			}
			fixture.Require(t, "status", "--check", "--json")
			fixture.Require(t, "apply", "--yes", "--json")
			testkit.AssertFileContent(t, fixture.Calls, "provider\nprovider\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiAdapterMultipleConsumersShareOneProtectedInstall(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := prepareNativePiAdapterFixture(t, scope)
			declareNativePiAdapterKeys(t, fixture, scope)
			body := string(testkit.ReadFile(t, fixture.Manifest)) + fmt.Sprintf("\n[[mcp_server]]\nname='other'\ntargets=['pi']\nscope=%q\nbackend='adapter'\ntransport='stdio'\ncommand='node'\nargs=['other.js']\n", scope)
			testkit.WriteFile(t, fixture.Workspace, "daem.toml", body)
			fixture.Require(t, "lock")
			fixture.Require(t, "apply", "--yes", "--json")
			fixture.LaunchProtection(t)
			fixture.Require(t, "status", "--check", "--json")
			fixture.Require(t, "apply", "--yes", "--json")
			testkit.AssertFileContent(t, fixture.Calls, "provider\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiAdapterUnversionedPackageDoesNotAuthorizeReinstall(t *testing.T) {
	fixture := newNativePiAdapterFixture(t, target.ScopeProject, false)
	testkit.WriteFile(t, filepath.Dir(fixture.Settings), "npm/node_modules/pi-mcp-adapter/package.json", `{"name":"pi-mcp-adapter"}`)
	before := testkit.ReadFile(t, fixture.Settings)
	if code, _, _ := fixture.Run("apply", "--yes", "--json"); code == 0 {
		t.Fatal("unobservable version became a missing-package repair grant")
	}
	testkit.AssertFileContent(t, fixture.Settings, string(before))
	testkit.AssertFileContent(t, fixture.Calls, "provider\n")
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiAdapterProviderThenAnotherProtectedExtension(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := prepareNativePiAdapterFixture(t, scope)
			declareNativePiAdapterKeys(t, fixture, scope)
			fixture.Require(t, "add", "extension", "extra", "npm:@acme/extra@1", "--target", "pi", "--scope", string(scope))
			fixture.Require(t, "lock")
			fixture.Require(t, "apply", "--yes", "--json")
			fixture.LaunchProtection(t)
			fixture.Require(t, "status", "--check", "--json")
			fixture.Require(t, "apply", "--yes", "--json")
			testkit.AssertFileContent(t, fixture.Calls, "provider\nextra\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func prepareNativePiAdapterFixture(t *testing.T, scope target.Scope) nativefixture.PiInvocation {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	base := filepath.Join(workspace, ".pi")
	if scope == target.ScopeGlobal {
		base = os.Getenv("PI_CODING_AGENT_DIR")
	}
	fixture := nativefixture.PiInvocation{Workspace: workspace, Manifest: filepath.Join(workspace, "daem.toml"), Settings: filepath.Join(base, "settings.json"), BeforeCommand: filepath.Join(root, "provider-before.json"), Calls: filepath.Join(root, "provider-calls")}
	fixture.LaunchState, fixture.LaunchRegistry = filepath.Join(root, "provider-state.json"), filepath.Join(root, "provider-registry.json")
	body := fmt.Sprintf(`version=1
targets=['pi']
[[extension]]
id='adapter-provider'
carrier='pi-package'
targets=['pi']
scope=%q
source={ host_source='npm:pi-mcp-adapter@^2.13.0' }
[[mcp_server]]
name='context7'
targets=['pi']
scope=%q
backend='adapter'
transport='stdio'
command='node'
args=['server.js']
`, scope, scope)
	testkit.WriteFile(t, workspace, "daem.toml", body)
	testkit.WriteFile(t, base, "settings.json", `{"foreign":{"retained":true},"packages":[]}`)
	bin := filepath.Join(root, "bin")
	testkit.WriteFile(t, bin, "pi", `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '0.85.1\n'; exit 0; fi
[ "$1" = install ] || exit 97
case "$2" in 'npm:pi-mcp-adapter@^2.13.0'|'npm:@acme/extra@1') ;; *) exit 97 ;; esac
if [ "$DAEM_TEST_SCOPE" = project ]; then
  [ "$#" -eq 3 ] && [ "$3" = -l ] || exit 97
else
  [ "$#" -eq 2 ] || exit 97
fi
if [ -f "$DAEM_TEST_SETTINGS" ]; then /bin/cp "$DAEM_TEST_SETTINGS" "$DAEM_TEST_COMMAND_BEFORE"; fi
if [ "$2" = 'npm:@acme/extra@1' ]; then printf 'extra\n' >> "$DAEM_TEST_COMMAND_CALLS"; else printf 'provider\n' >> "$DAEM_TEST_COMMAND_CALLS"; fi
if [ "$DAEM_TEST_INSTALL_PHASE" = native ]; then
  /bin/cp .daem/state.json "$DAEM_TEST_LAUNCH_STATE"
  if [ -f "$DAEM_TEST_REGISTRY" ]; then /bin/cp "$DAEM_TEST_REGISTRY" "$DAEM_TEST_LAUNCH_REGISTRY"; fi
  if [ -f "$DAEM_TEST_PROVIDER_BASE/mcp.json" ]; then
    /bin/cp "$DAEM_TEST_PROVIDER_BASE/mcp.json" "$DAEM_TEST_MCP_BEFORE"
  else
    printf 'absent\n' > "$DAEM_TEST_MCP_BEFORE"
  fi
  if [ "$DAEM_TEST_PROVIDER_FAULT" = nonzero ]; then exit 23; fi
fi
if [ "$2" = 'npm:@acme/extra@1' ]; then
  printf '%s\n' '{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:pi-mcp-adapter@^2.13.0","npm:@acme/extra@1"]}' > "$DAEM_TEST_SETTINGS"
  exit 0
fi
/bin/mkdir -p "$DAEM_TEST_PROVIDER_BASE/npm/node_modules/pi-mcp-adapter"
printf '%s\n' '{"name":"pi-mcp-adapter","version":"2.15.0"}' > "$DAEM_TEST_PROVIDER_BASE/npm/node_modules/pi-mcp-adapter/package.json"
if [ "$DAEM_TEST_INSTALL_PHASE" = baseline ]; then
  printf '%s\n' '{"foreign":{"retained":true},"packages":["npm:pi-mcp-adapter@^2.13.0"]}' > "$DAEM_TEST_SETTINGS"
else
  printf '%s\n' "$DAEM_TEST_PROVIDER_RESULT" > "$DAEM_TEST_SETTINGS"
  case "$DAEM_TEST_PROVIDER_FAULT" in
    key) printf '%s\n' '{"theme":"host-changed","foreign":{"retained":true},"packages":["npm:pi-mcp-adapter@^2.13.0"]}' > "$DAEM_TEST_SETTINGS" ;;
    outside) printf '%s\n' '{"theme":"fixture-theme","foreign":{"retained":false},"packages":["npm:pi-mcp-adapter@^2.13.0"]}' > "$DAEM_TEST_SETTINGS" ;;
    malformed) printf '[' > "$DAEM_TEST_SETTINGS" ;;
    mode) /bin/chmod 666 "$DAEM_TEST_SETTINGS" ;;
    badversion) printf '%s\n' '{"name":"pi-mcp-adapter","version":"99.0.0"}' > "$DAEM_TEST_PROVIDER_BASE/npm/node_modules/pi-mcp-adapter/package.json" ;;
    unversioned) printf '%s\n' '{"name":"pi-mcp-adapter"}' > "$DAEM_TEST_PROVIDER_BASE/npm/node_modules/pi-mcp-adapter/package.json" ;;
  esac
fi
`)
	if err := os.Chmod(filepath.Join(bin, "pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DAEM_TEST_SCOPE", string(scope))
	t.Setenv("DAEM_TEST_SETTINGS", fixture.Settings)
	t.Setenv("DAEM_TEST_COMMAND_BEFORE", fixture.BeforeCommand)
	t.Setenv("DAEM_TEST_COMMAND_CALLS", fixture.Calls)
	t.Setenv("DAEM_TEST_PROVIDER_BASE", base)
	t.Setenv("DAEM_TEST_PROVIDER_FAULT", "")
	t.Setenv("DAEM_TEST_PROVIDER_RESULT", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:pi-mcp-adapter@^2.13.0"]}`)
	t.Setenv("DAEM_TEST_MCP_BEFORE", filepath.Join(root, "mcp-before.json"))
	t.Setenv("DAEM_TEST_LAUNCH_STATE", fixture.LaunchState)
	t.Setenv("DAEM_TEST_LAUNCH_REGISTRY", fixture.LaunchRegistry)
	paths, err := daempaths.Resolve(fixture.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAEM_TEST_REGISTRY", paths.OwnershipRegistryPath)
	t.Setenv("DAEM_TEST_INSTALL_PHASE", "baseline")
	return fixture
}

func newNativePiAdapterFixture(t *testing.T, scope target.Scope, reinstall bool) nativefixture.PiInvocation {
	t.Helper()
	fixture := prepareNativePiAdapterFixture(t, scope)
	base := filepath.Dir(fixture.Settings)
	fixture.Require(t, "lock")
	fixture.Require(t, "apply", "--yes")
	if reinstall {
		testkit.WriteFile(t, base, "settings.json", `{"foreign":{"retained":true},"packages":[]}`)
		if err := os.Remove(filepath.Join(base, "npm", "node_modules", "pi-mcp-adapter", "package.json")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(base, "npm", "node_modules", "pi-mcp-adapter")); err != nil {
			t.Fatal(err)
		}
	}
	declareNativePiAdapterKeys(t, fixture, scope)
	return fixture
}

func declareNativePiAdapterKeys(t *testing.T, fixture nativefixture.PiInvocation, scope target.Scope) {
	t.Helper()
	workspace := fixture.Workspace
	body := fmt.Sprintf(`version=1
targets=['pi']
[[extension]]
id='adapter-provider'
carrier='pi-package'
targets=['pi']
scope=%q
source={ host_source='npm:pi-mcp-adapter@^2.13.0' }
[[mcp_server]]
name='context7'
targets=['pi']
scope=%q
backend='adapter'
transport='stdio'
command='node'
args=['updated-server.js']
[[host_config]]
id='preferences'
scope=%q
path='settings.json'
source=%q
ownership='keys'
`, scope, scope, scope, filepath.Join(workspace, "preferences.json"))
	testkit.WriteFile(t, workspace, "daem.toml", body)
	testkit.WriteFile(t, workspace, "preferences.json", `{"theme":"fixture-theme"}`)
	t.Setenv("DAEM_TEST_INSTALL_PHASE", "native")
	fixture.Require(t, "lock")
}
