//go:build darwin || linux

package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func nativeDelegateFixture(t *testing.T, scope target.Scope, mode, fault string) nativePiInvocationFixture {
	t.Helper()
	f := newNativePiInvocationFixtureAtScope(t, "", scope)
	base := fmt.Sprintf("version=1\ntargets=['pi','claude-code']\n[[extension]]\nid='tools'\ncarrier='pi-package'\ntargets=['pi']\nscope=%q\nsource={host_source='git:github.com/acme/pi-tools@v1'}\n", scope)
	keys := fmt.Sprintf("\n[[host_config]]\nid='preferences'\ntargets=['pi']\nscope=%q\npath='settings.json'\nsource=%q\nownership='keys'\n", scope, filepath.Join(f.workspace, "preferences.json"))
	testkit.WriteFile(t, f.workspace, "daem.toml", base+keys)
	f.require(t, "lock")
	if mode != "new" {
		f.require(t, "apply", "--yes")
	}
	if mode == "changed" {
		testkit.WriteFile(t, f.workspace, "preferences.json", `{"theme":"updated-theme"}`)
	}
	if mode == "retiring" {
		keys = ""
	}
	body := base + keys
	if mode == "whole" {
		testkit.WriteFile(t, f.workspace, "dedicated.json", `{"enabled":true}`)
		body += fmt.Sprintf("\n[[host_config]]\nid='whole'\ntargets=['pi']\nscope=%q\npath='dedicated.json'\nsource=%q\nownership='document'\n", scope, filepath.Join(f.workspace, "dedicated.json"))
		testkit.WriteFile(t, f.workspace, "daem.toml", body)
		f.require(t, "lock")
		f.require(t, "apply", "--yes")
	}
	for _, name := range []string{"first", "second"} {
		body += fmt.Sprintf("\n[[mcp_server]]\nname=%q\ntargets=['claude-code']\nscope='project'\ntransport='stdio'\ncommand='native-mcp-fixture'\nargs=['serve']\nenv={API_TOKEN={from_env='DAEM_TEST_DELEGATE_SECRET'}}\n", name)
	}
	testkit.WriteFile(t, f.workspace, "daem.toml", body)
	script := `#!/bin/sh
set -eu
[ "$#" -eq 1 ] && [ "$1" = serve ] || exit 97
[ -f .mcp.json ] && [ -f AGENTS.md ] || exit 96
n=0
if [ -f "$DAEM_TEST_DELEGATE_CALLS" ]; then
  while IFS= read -r line; do n=$((n+1)); done < "$DAEM_TEST_DELEGATE_CALLS"
fi
n=$((n+1))
printf 'delegate\n' >> "$DAEM_TEST_DELEGATE_CALLS"
/bin/cp "$DAEM_TEST_SETTINGS" "$DAEM_TEST_DELEGATE_CAPTURE/before-$n.json"
/bin/cp .daem/state.json "$DAEM_TEST_DELEGATE_CAPTURE/state-$n.json"
if [ -f "$DAEM_TEST_REGISTRY" ]; then /bin/cp "$DAEM_TEST_REGISTRY" "$DAEM_TEST_DELEGATE_CAPTURE/registry-$n.json"; fi
/bin/cp .mcp.json "$DAEM_TEST_DELEGATE_CAPTURE/mcp-$n.json"
printf '%s\n' "$API_TOKEN"
if [ "$n" -eq 1 ]; then
  case "$DAEM_TEST_DELEGATE_FAULT" in
    key) printf '%s\n' '{"theme":"changed","foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}' > "$DAEM_TEST_SETTINGS" ;;
    outside) printf '%s\n' '{"theme":"fixture-theme","foreign":{"retained":false},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}' > "$DAEM_TEST_SETTINGS" ;;
    mode) /bin/chmod 666 "$DAEM_TEST_SETTINGS" ;;
    exit) exit 19 ;;
    inaccessible) /bin/chmod 000 "$DAEM_TEST_SETTINGS" ;;
    loss) /bin/chmod 666 "$DAEM_TEST_SETTINGS"; kill -KILL "${DAEM_TEST_OPAQUE_PARENT_PID:?}" ;;
  esac
fi
`
	bin := filepath.Join(filepath.Dir(f.calls), "bin")
	testkit.WriteFile(t, bin, "native-mcp-fixture", script)
	if err := os.Chmod(filepath.Join(bin, "native-mcp-fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(filepath.Dir(f.calls), "delegate-capture")
	if err := os.Mkdir(capture, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAEM_TEST_DELEGATE_CALLS", filepath.Join(capture, "calls"))
	t.Setenv("DAEM_TEST_DELEGATE_CAPTURE", capture)
	t.Setenv("DAEM_TEST_DELEGATE_FAULT", fault)
	t.Setenv("DAEM_TEST_DELEGATE_SECRET", "fixture-credential-not-for-output")
	f.addInstructions(t, "delegate prerequisite\n")
	return f
}

func nativeDelegateLaunch(t *testing.T, f nativePiInvocationFixture, index int, theme string) durable.Snapshot {
	t.Helper()
	capture := os.Getenv("DAEM_TEST_DELEGATE_CAPTURE")
	f.launchRegistry = filepath.Join(capture, fmt.Sprintf("registry-%d.json", index))
	f.launchNativeProtection(t)
	var before struct {
		Theme string `json:"theme"`
	}
	if err := json.Unmarshal(testkit.ReadFile(t, filepath.Join(capture, fmt.Sprintf("before-%d.json", index))), &before); err != nil || before.Theme != theme {
		t.Fatal("native value was not retained before the actual delegate", err)
	}
	var mcp struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(testkit.ReadFile(t, filepath.Join(capture, fmt.Sprintf("mcp-%d.json", index))), &mcp); err != nil || len(mcp.Servers) != 2 {
		t.Fatal("delegate ran before both exact MCP projections", err)
	}
	state, err := statefile.Decode(testkit.ReadFile(t, filepath.Join(capture, fmt.Sprintf("state-%d.json", index))))
	ordinary := 0
	for _, row := range state.ManagedPaths() {
		if !aggregate.IsHostConfigDocumentSubject(row.Subject()) {
			ordinary++
		}
	}
	if err != nil || len(state.ManagedAggregates()) != 3 || ordinary != 2 {
		t.Fatalf("delegate prerequisite checkpoint: aggregates=%d paths=%d err=%v", len(state.ManagedAggregates()), len(state.ManagedPaths()), err)
	}
	return state
}

func TestNativeDelegatesProtectDistinctActorsWithIdenticalCommands(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, mode := range []string{"new", "current", "changed", "retiring", "whole"} {
			t.Run(string(scope)+"/"+mode, func(t *testing.T) {
				f := nativeDelegateFixture(t, scope, mode, "")
				code, stdout, stderr := f.run("apply", "--yes")
				if code != 0 {
					t.Fatalf("native delegate Apply failed: %d %s %s", code, stdout, stderr)
				}
				if strings.Contains(stdout+stderr, os.Getenv("DAEM_TEST_DELEGATE_SECRET")) {
					t.Fatal("delegate result exposed a resolved credential")
				}
				if got := string(testkit.ReadFile(t, os.Getenv("DAEM_TEST_DELEGATE_CALLS"))); got != "delegate\ndelegate\n" {
					t.Fatal("distinct subjects were deduplicated by their identical subprocess request")
				}
				theme := "fixture-theme"
				if mode == "changed" {
					theme = "updated-theme"
				}
				for index := 1; index <= 2; index++ {
					launched := nativeDelegateLaunch(t, f, index, theme)
					if mode == "whole" && len(launched.ManagedPaths()) != 3 {
						t.Fatal("delegate checkpoint discarded its retained whole document")
					}
				}
				state, err := statefile.LoadOptional(t.Context(), filepath.Join(f.workspace, ".daem", "state.json"))
				if err != nil || len(state.DelegateAttempts()) != 2 {
					t.Fatal("delegate diagnostics lost their exact actors", err)
				}
				native := 0
				for _, row := range state.ManagedAggregates() {
					if _, selected := aggregate.HostConfigSyntaxForCodec(row.Contribution().Contract().CodecContractID()); selected {
						native++
					}
				}
				want := 1
				if mode == "retiring" {
					want = 0
				}
				if native != want {
					t.Fatal("final native membership did not reflect late retirement")
				}
				f.require(t, "status", "--check", "--json")
				if mode == "current" {
					f.require(t, "apply", "--yes")
					if got := string(testkit.ReadFile(t, os.Getenv("DAEM_TEST_DELEGATE_CALLS"))); got != "delegate\ndelegate\ndelegate\ndelegate\n" {
						t.Fatal("current native/MCP state acquired delegate skip authority")
					}
					for index := 3; index <= 4; index++ {
						nativeDelegateLaunch(t, f, index, theme)
					}
				}
				testkit.AssertNoRecoveryArtifacts(t, f.workspace)
			})
		}
	}
}

func TestNativeDelegateBoundaryFailureStopsNextActorWithoutFalsifyingProcess(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, fault := range []string{"key", "outside", "mode", "exit"} {
			t.Run(string(scope)+"/"+fault, func(t *testing.T) {
				f := nativeDelegateFixture(t, scope, "retiring", fault)
				initial, err := os.Stat(f.settings)
				if err != nil {
					t.Fatal(err)
				}
				code, stdout, stderr := f.run("apply", "--yes", "--json")
				if code == 0 {
					t.Fatalf("failed delegate Apply succeeded: %s %s", stdout, stderr)
				}
				nativeDelegateLaunch(t, f, 1, "fixture-theme")
				calls := "delegate\n"
				if fault == "exit" {
					calls += "delegate\n"
				}
				if got := string(testkit.ReadFile(t, os.Getenv("DAEM_TEST_DELEGATE_CALLS"))); got != calls {
					t.Fatal("native failure did not stop before command2, or ordinary process failure changed continuation")
				}
				state, err := statefile.LoadOptional(t.Context(), filepath.Join(f.workspace, ".daem", "state.json"))
				if err != nil || len(state.ManagedAggregates()) != 3 || len(state.DelegateAttempts()) != strings.Count(calls, "delegate") {
					t.Fatal("partial delegate diagnostics or retiring native baseline were discarded", err)
				}
				if info, err := os.Stat(f.settings); err != nil || info.Mode().Perm() != initial.Mode().Perm() {
					t.Fatal("delegate permission containment failed", err)
				}
				var result struct {
					Attempts []struct {
						Status string `json:"status"`
					} `json:"delegate_attempts"`
				}
				if err := json.Unmarshal([]byte(stdout), &result); err != nil || len(result.Attempts) != strings.Count(calls, "delegate") {
					t.Fatal("partial result lost observed attempts", err)
				}
				want := "succeeded"
				if fault == "exit" {
					want = "failed"
				}
				if result.Attempts[0].Status != want {
					t.Fatal("native qualification changed the mechanical process result")
				}
				testkit.AssertNoRecoveryArtifacts(t, f.workspace)
			})
		}
	}
}

func TestNativeDelegateProcessLossRecoversPermissionsWithoutReplayingCommands(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			f := nativeDelegateFixture(t, scope, "retiring", "loss")
			initial, err := os.Stat(f.settings)
			if err != nil {
				t.Fatal(err)
			}
			recovery := nativeRecoveryFixture{manifest: f.manifest, destination: f.settings}
			if code, stdout, stderr := invokeNativeRecoveryCommand(t, recovery, "native-opaque-command", "apply", "--yes"); code == 0 {
				t.Fatalf("lost delegate parent reported success: %s %s", stdout, stderr)
			}
			nativeDelegateLaunch(t, f, 1, "fixture-theme")
			if code, _, _ := f.run("apply", "--yes"); code == 0 {
				t.Fatal("pending delegate protection did not block another invocation")
			}
			if got := string(testkit.ReadFile(t, os.Getenv("DAEM_TEST_DELEGATE_CALLS"))); got != "delegate\n" {
				t.Fatal("pending safety work launched command2")
			}
			f.require(t, "recover", "--dry-run")
			f.require(t, "recover", "--yes")
			if info, err := os.Stat(f.settings); err != nil || info.Mode().Perm() != initial.Mode().Perm() {
				t.Fatal("recovery did not contain delegate-introduced access", err)
			}
			if got := string(testkit.ReadFile(t, os.Getenv("DAEM_TEST_DELEGATE_CALLS"))); got != "delegate\n" {
				t.Fatal("permission recovery replayed a delegate")
			}
			t.Setenv("DAEM_TEST_DELEGATE_FAULT", "")
			f.require(t, "apply", "--yes")
			if got := string(testkit.ReadFile(t, os.Getenv("DAEM_TEST_DELEGATE_CALLS"))); got != "delegate\ndelegate\ndelegate\n" {
				t.Fatal("explicit Apply retry changed ordinary delegate replay semantics")
			}
			f.require(t, "status", "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, f.workspace)
		})
	}
}

func TestNativeBlockedDelegatePersistsWithoutFabricatingInvocation(t *testing.T) {
	f := nativeDelegateFixture(t, target.ScopeProject, "current", "")
	body := strings.ReplaceAll(string(testkit.ReadFile(t, f.manifest)), "command='native-mcp-fixture'", "command='absent-native-delegate-fixture'")
	testkit.WriteFile(t, f.workspace, "daem.toml", body)
	f.require(t, "lock")
	if code, _, _ := f.run("apply", "--yes", "--json"); code == 0 {
		t.Fatal("policy-blocked delegates reported success")
	}
	if _, err := os.Stat(os.Getenv("DAEM_TEST_DELEGATE_CALLS")); !os.IsNotExist(err) {
		t.Fatal("blocked delegate launched a runner", err)
	}
	state, err := statefile.LoadOptional(t.Context(), filepath.Join(f.workspace, ".daem", "state.json"))
	if err != nil || !state.ContainsHostConfig() || len(state.DelegateAttempts()) != 2 {
		t.Fatal("blocked diagnostic checkpoint lost complete native state", err)
	}
	f.require(t, "apply", "--dry-run", "--json")
	if _, err := os.Stat(os.Getenv("DAEM_TEST_DELEGATE_CALLS")); !os.IsNotExist(err) {
		t.Fatal("dry run invoked a delegate", err)
	}
	testkit.AssertNoRecoveryArtifacts(t, f.workspace)
}

func TestNativeDelegatesPreserveCodexValuesUntilLateRetirement(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			f := nativeDelegateFixture(t, scope, "new", "")
			testkit.WriteFile(t, filepath.Dir(f.settings), "settings.json", `{"foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`)
			f.settings = filepath.Join(f.workspace, ".codex", "config.toml")
			if scope == target.ScopeGlobal {
				f.settings = filepath.Join(os.Getenv("CODEX_HOME"), "config.toml")
			}
			testkit.WriteFile(t, f.workspace, "preferences.toml", "[ui]\ntheme='fixture-theme'\n")
			body := string(testkit.ReadFile(t, f.manifest))
			body = strings.Replace(body, "targets=['pi','claude-code']", "targets=['pi','claude-code','codex']", 1)
			body = strings.Replace(body, "id='preferences'\ntargets=['pi']", "id='preferences'\ntargets=['codex']", 1)
			body = strings.Replace(body, "path='settings.json'", "path='config.toml'", 1)
			body = strings.Replace(body, filepath.Join(f.workspace, "preferences.json"), filepath.Join(f.workspace, "preferences.toml"), 1)
			testkit.WriteFile(t, f.workspace, "daem.toml", body)
			t.Setenv("DAEM_TEST_SETTINGS", f.settings)
			f.require(t, "lock")
			f.require(t, "apply", "--yes")
			before, rest, found := strings.Cut(body, "\n[[host_config]]")
			_, mcp, next := strings.Cut(rest, "\n[[mcp_server]]")
			if !found || !next {
				t.Fatal("invalid Codex delegate fixture")
			}
			testkit.WriteFile(t, f.workspace, "daem.toml", before+"\n[[mcp_server]]"+mcp)
			f.require(t, "lock")
			f.require(t, "apply", "--yes")
			for index := 1; index <= 4; index++ {
				capture := os.Getenv("DAEM_TEST_DELEGATE_CAPTURE")
				f.launchRegistry = filepath.Join(capture, fmt.Sprintf("registry-%d.json", index))
				f.launchNativeProtection(t)
				var values struct {
					UI struct{ Theme string } `toml:"ui"`
				}
				if _, err := toml.Decode(string(testkit.ReadFile(t, filepath.Join(capture, fmt.Sprintf("before-%d.json", index)))), &values); err != nil || values.UI.Theme != "fixture-theme" {
					t.Fatal("Codex value was absent before its delegate finished", err)
				}
			}
			state, err := statefile.LoadOptional(t.Context(), filepath.Join(f.workspace, ".daem", "state.json"))
			if err != nil || len(state.ManagedAggregates()) != 2 || len(state.DelegateAttempts()) != 2 {
				t.Fatal("Codex retirement lost complete delegate/MCP state", err)
			}
			f.require(t, "status", "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, f.workspace)
		})
	}
}

func TestNativeDelegateRetirementSuffixProcessLossDoesNotReplayDuringRecovery(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, boundary := range []string{"host", "state"} {
			t.Run(string(scope)+"/"+boundary, func(t *testing.T) {
				f := nativeDelegateFixture(t, scope, "retiring", "")
				recovery := nativeRecoveryFixture{manifest: f.manifest, destination: f.settings}
				code, stdout, stderr := invokeNativeRecoveryCommand(t, recovery, "native-ordinary-"+boundary, "apply", "--yes")
				if code != nativeRecoveryExit {
					t.Fatalf("late native suffix did not reach process-loss boundary: %d %s %s", code, stdout, stderr)
				}
				for index := 1; index <= 2; index++ {
					nativeDelegateLaunch(t, f, index, "fixture-theme")
				}
				f.require(t, "recover", "--dry-run")
				f.require(t, "recover", "--yes")
				if got := string(testkit.ReadFile(t, os.Getenv("DAEM_TEST_DELEGATE_CALLS"))); got != "delegate\ndelegate\n" {
					t.Fatal("native journal recovery replayed an opaque delegate")
				}
				f.require(t, "apply", "--yes")
				if got := string(testkit.ReadFile(t, os.Getenv("DAEM_TEST_DELEGATE_CALLS"))); got != "delegate\ndelegate\ndelegate\ndelegate\n" {
					t.Fatal("explicit retry changed existing delegate invocation policy")
				}
				f.require(t, "status", "--check", "--json")
				testkit.AssertNoRecoveryArtifacts(t, f.workspace)
			})
		}
	}
}

func TestNativeOrderPublishesAfterDelegateConsumers(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			order := newNativePiOrderFixture(t, scope)
			workspace := filepath.Dir(order.manifest)
			bin := filepath.Join(filepath.Dir(workspace), "bin")
			testkit.WriteFile(t, bin, "native-order-reader-fixture", "#!/bin/sh\nset -eu\n[ -f .mcp.json ]\n/bin/cp \"$DAEM_TEST_ORDER_SETTINGS\" \"$DAEM_TEST_ORDER_BEFORE\"\n/bin/cp \"$DAEM_TEST_ORDER_REGISTRY\" \"$DAEM_TEST_ORDER_BOUND\"\n")
			if err := os.Chmod(filepath.Join(bin, "native-order-reader-fixture"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			paths, err := daempaths.Resolve(order.manifest)
			if err != nil {
				t.Fatal(err)
			}
			before, bound := filepath.Join(workspace, "order-before.json"), filepath.Join(workspace, "order-bound.json")
			t.Setenv("DAEM_TEST_ORDER_SETTINGS", order.destination)
			t.Setenv("DAEM_TEST_ORDER_BEFORE", before)
			t.Setenv("DAEM_TEST_ORDER_REGISTRY", paths.OwnershipRegistryPath)
			t.Setenv("DAEM_TEST_ORDER_BOUND", bound)
			runNativeLifecycleCommand(t, "add", "mcp-server", "order-reader", "native-order-reader-fixture", "--target", "claude-code", "--scope", "project", "--manifest", order.manifest)
			runNativeLifecycleCommand(t, "lock", "--manifest", order.manifest)
			result := runNativeLifecycleCommand(t, "apply", "--manifest", order.manifest, "--yes", "--json")
			requireNativePiOrderResult(t, result, "converged", true, 3)
			nativePiInvocationFixture{launchRegistry: bound}.launchNativeProtection(t)
			requireNativePiOrderValues(t, before, "next-theme", "npm:@acme/beta@1", "npm:@acme/foreign@1", "npm:@acme/alpha@1")
			requireNativePiOrderValues(t, order.destination, "next-theme", "npm:@acme/alpha@1", "npm:@acme/foreign@1", "npm:@acme/beta@1")
			runNativeLifecycleCommand(t, "status", "--manifest", order.manifest, "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, workspace)
		})
	}
}
