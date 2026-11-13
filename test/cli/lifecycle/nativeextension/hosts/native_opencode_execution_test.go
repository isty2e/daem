//go:build darwin || linux

package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

type nativeOpenCodeFixture struct {
	nativeOpaqueFixture
	config, declaration, payload string
}

func newNativeOpenCodeFixture(t *testing.T, scope, nativeScope target.Scope, installed bool) nativeOpenCodeFixture {
	t.Helper()
	return newNativeOpenCodeFixtureWithPi(t, scope, nativeScope, installed, false)
}

func newNativeOpenCodeFixtureWithPi(t *testing.T, scope, nativeScope target.Scope, installed, withPi bool) nativeOpenCodeFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	f := nativeOpenCodeFixture{nativeOpaqueFixture: nativeOpaqueFixture{workspace: workspace, manifest: filepath.Join(workspace, "daem.toml"), calls: filepath.Join(root, "calls"), keyBefore: filepath.Join(root, "before.toml"), launchState: filepath.Join(root, "launch-state.json")}, payload: filepath.Join(root, "payload.jsonc")}
	f.config = filepath.Join(workspace, ".opencode")
	if scope == target.ScopeGlobal {
		f.config = filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	}
	f.keyFile = filepath.Join(workspace, ".codex", "config.toml")
	if nativeScope == target.ScopeGlobal {
		f.keyFile = filepath.Join(os.Getenv("CODEX_HOME"), "config.toml")
	}
	testkit.WriteFile(t, filepath.Dir(f.keyFile), "config.toml", "foreign=true\n# native note\n")
	f.declaration = fmt.Sprintf("version=1\ntargets=['opencode','codex']\n[[extension]]\nid='alpha'\ntargets=['opencode']\ncarrier='opencode-plugin'\nscope=%q\nsource={host_source='@acme/alpha@1'}\n", scope)
	if withPi {
		f.declaration = strings.Replace(f.declaration, "targets=['opencode','codex']", "targets=['opencode','codex','pi']", 1)
		for _, id := range []string{"pi-alpha", "pi-beta"} {
			f.declaration += fmt.Sprintf("\n[[extension]]\nid=%q\ntargets=['pi']\ncarrier='pi-package'\nscope='project'\nsource={host_source=%q}\n", id, "npm:@acme/"+id+"@1")
		}
	}
	for _, name := range []string{"opencode.json", "opencode.jsonc", "tui.json", "tui.jsonc"} {
		testkit.WriteFile(t, f.config, name, `{"plugin":[],"keep":true}`)
	}
	f.setPayload(t, `{"plugin":["@acme/alpha@1","@acme/foreign@1"],"keep":true}`)
	bin := filepath.Join(root, "bin")
	testkit.WriteFile(t, bin, "opencode", `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '1.0.0\n'; exit 0; fi
[ "$1" = plugin ] || exit 97
printf '%s\n' "$2" >> "$DAEM_TEST_OC_CALLS"
if [ "$DAEM_TEST_OC_PHASE" = native ]; then
  cp "$DAEM_TEST_OC_KEY" "$DAEM_TEST_OC_BEFORE"
  cp .daem/state.json "$DAEM_TEST_OC_STATE"
fi
for name in opencode.json opencode.jsonc tui.json tui.jsonc; do
  cp "$DAEM_TEST_OC_PAYLOAD" "$DAEM_TEST_OC_CONFIG/$name"
done
if [ "$DAEM_TEST_OC_PHASE" = native ]; then
  case "$DAEM_TEST_OC_FAULT" in
    key) printf "model='unexpected'\nforeign=true\n# native note\n" > "$DAEM_TEST_OC_KEY" ;;
    outside) printf '\n[unexpected]\nvalue=true\n' >> "$DAEM_TEST_OC_KEY" ;;
    mode|mode-command) chmod 666 "$DAEM_TEST_OC_KEY" ;;
    command) exit 23 ;;
    terminate) chmod 666 "$DAEM_TEST_OC_KEY"; kill -KILL "$DAEM_TEST_OPAQUE_PARENT_PID"; exit 0 ;;
  esac
  [ "$DAEM_TEST_OC_FAULT" != mode-command ] || exit 23
fi
printf '{}\n'
`)
	if err := os.Chmod(filepath.Join(bin, "opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if withPi {
		settings := filepath.Join(workspace, ".pi", "settings.json")
		testkit.WriteFile(t, filepath.Dir(settings), "settings.json", `{"packages":[]}`)
		t.Setenv("DAEM_TEST_OC_PI_SETTINGS", settings)
		testkit.WriteFile(t, bin, "pi", `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '1.1.0\n'; exit 0; fi
[ "$1" = install ] || exit 97
case "$2" in
  *pi-alpha*) printf '%s\n' '{"packages":["npm:@acme/pi-alpha@1"]}' > "$DAEM_TEST_OC_PI_SETTINGS" ;;
  *pi-beta*) printf '%s\n' '{"packages":["npm:@acme/pi-alpha@1","npm:@acme/pi-beta@1"]}' > "$DAEM_TEST_OC_PI_SETTINGS" ;;
  *) exit 97 ;;
esac
`)
		if err := os.Chmod(filepath.Join(bin, "pi"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for name, value := range map[string]string{"DAEM_TEST_OC_CALLS": f.calls, "DAEM_TEST_OC_KEY": f.keyFile, "DAEM_TEST_OC_BEFORE": f.keyBefore, "DAEM_TEST_OC_STATE": f.launchState, "DAEM_TEST_OC_PAYLOAD": f.payload, "DAEM_TEST_OC_CONFIG": f.config, "DAEM_TEST_OC_PHASE": "baseline", "DAEM_TEST_OC_FAULT": ""} {
		t.Setenv(name, value)
	}
	testkit.WriteFile(t, workspace, "daem.toml", f.declaration)
	f.require(t, "lock")
	if installed {
		f.require(t, "apply", "--yes")
	}
	testkit.WriteFile(t, workspace, "preferences.toml", "model='native-model'\n")
	f.declaration += fmt.Sprintf("\n[[host_config]]\nid='prefs'\ntargets=['codex']\nscope=%q\npath='config.toml'\nsource=%q\nownership='keys'\n", nativeScope, filepath.Join(workspace, "preferences.toml"))
	testkit.WriteFile(t, workspace, "daem.toml", f.declaration)
	t.Setenv("DAEM_TEST_OC_PHASE", "native")
	f.require(t, "lock")
	return f
}

func (f nativeOpenCodeFixture) setPayload(t *testing.T, content string) {
	t.Helper()
	testkit.WriteFile(t, filepath.Dir(f.payload), filepath.Base(f.payload), content)
}

func (f nativeOpenCodeFixture) requireSources(t *testing.T, sources ...string) {
	t.Helper()
	for _, name := range []string{"opencode.json", "opencode.jsonc", "tui.json", "tui.jsonc"} {
		content := testkit.ReadFile(t, filepath.Join(f.config, name))
		doc, err := opencodeconfig.Parse(content)
		if err != nil {
			t.Fatal(err)
		}
		var actual []string
		for _, entry := range doc.Entries() {
			actual = append(actual, entry.Source())
		}
		if !slices.Equal(actual, sources) || !strings.Contains(string(content), `"keep":true`) {
			t.Fatalf("unexpected %s content: %s", name, content)
		}
	}
}

func TestNativeOpenCodeInstallAndRefresh(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, keyScope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(scope)+"/"+string(keyScope), func(t *testing.T) {
				f := newNativeOpenCodeFixture(t, scope, keyScope, false)
				f.require(t, "apply", "--yes", "--json")
				if !strings.Contains(string(testkit.ReadFile(t, f.keyBefore)), "native-model") {
					t.Fatal("consumer ran before selected keys")
				}
				snapshot, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, f.launchState))
				if err != nil || len(snapshot.PendingCarrierInstalls()) != 1 || !snapshot.ContainsHostConfig() {
					t.Fatal("invocation lost pending correlation or complete native state", err)
				}
				f.require(t, "refresh", "extension", "alpha", "--yes", "--json")
				f.require(t, "status", "--check", "--json")
				f.require(t, "apply", "--yes")
				f.requireSources(t, "@acme/alpha@1", "@acme/foreign@1")
				testkit.AssertFileContent(t, f.calls, "@acme/alpha@1\n@acme/alpha@1\n")
				testkit.AssertNoRecoveryArtifacts(t, f.workspace)
			})
		}
	}
}

func TestNativeOpenCodeDirectRemovalAndOrder(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			f := newNativeOpenCodeFixture(t, scope, target.ScopeGlobal, true)
			f.require(t, "apply", "--yes")
			f.declaration += fmt.Sprintf("\n[[extension]]\nid='beta'\ntargets=['opencode']\ncarrier='opencode-plugin'\nscope=%q\nsource={host_source='@acme/beta@1'}\n", scope)
			testkit.WriteFile(t, f.workspace, "daem.toml", f.declaration)
			f.setPayload(t, "// retained OpenCode note\n"+`{"plugin":["@acme/beta@1","@acme/alpha@1","@acme/foreign@1"],"keep":true}`)
			f.require(t, "lock")
			f.require(t, "apply", "--yes", "--json")
			f.requireSources(t, "@acme/alpha@1", "@acme/beta@1", "@acme/foreign@1")
			for _, name := range []string{"opencode.json", "opencode.jsonc", "tui.json", "tui.jsonc"} {
				if !strings.Contains(string(testkit.ReadFile(t, filepath.Join(f.config, name))), "retained OpenCode note") {
					t.Fatal("order lost comment")
				}
			}
			f.require(t, "remove", "extension", "alpha")
			f.require(t, "lock")
			f.require(t, "apply", "--yes", "--json")
			f.requireSources(t, "@acme/beta@1", "@acme/foreign@1")
			f.require(t, "status", "--check", "--json")
			f.require(t, "apply", "--yes")
			testkit.AssertFileContent(t, f.calls, "@acme/alpha@1\n@acme/beta@1\n")
			testkit.AssertNoRecoveryArtifacts(t, f.workspace)
		})
	}
}

func TestNativeOpenCodeAndPiOrderCompletionIsPerClass(t *testing.T) {
	f := newNativeOpenCodeFixtureWithPi(t, target.ScopeProject, target.ScopeGlobal, true, true)
	f.declaration += "\n[[extension]]\nid='beta'\ntargets=['opencode']\ncarrier='opencode-plugin'\nscope='project'\nsource={host_source='@acme/beta@1'}\n"
	testkit.WriteFile(t, f.workspace, "daem.toml", f.declaration)
	f.setPayload(t, `{"plugin":["@acme/alpha@1","@acme/beta@1","@acme/foreign@1"],"keep":true}`)
	f.require(t, "lock")
	f.require(t, "apply", "--yes")
	settings := filepath.Join(f.workspace, ".pi", "settings.json")
	testkit.WriteFile(t, filepath.Dir(settings), "settings.json", `{"packages":["npm:@acme/pi-beta@1","npm:@acme/pi-alpha@1"],"foreign":{"retained":true}}`)
	f.require(t, "apply", "--yes", "--json")
	nativefixture.RequirePiOrderValues(t, settings, "", "npm:@acme/pi-alpha@1", "npm:@acme/pi-beta@1")
	testkit.WriteFile(t, filepath.Dir(settings), "settings.json", `{"packages":["npm:@acme/pi-beta@1","npm:@acme/pi-alpha@1"],"foreign":{"retained":true}}`)
	f.declaration += fmt.Sprintf("\n[[host_config]]\nid='pi-prefs'\ntargets=['pi']\nscope='project'\npath='settings.json'\nsource=%q\nownership='keys'\n", filepath.Join(f.workspace, "pi-prefs.json"))
	testkit.WriteFile(t, f.workspace, "pi-prefs.json", `{"theme":"next-theme"}`)
	testkit.WriteFile(t, f.workspace, "daem.toml", f.declaration)
	for _, name := range []string{"opencode.json", "opencode.jsonc", "tui.json", "tui.jsonc"} {
		testkit.WriteFile(t, f.config, name, `{"plugin":["@acme/beta@1","@acme/alpha@1","@acme/foreign@1"],"keep":true}`)
	}
	f.require(t, "lock")
	code, stdout, stderr := f.run("apply", "--yes", "--json")
	if code != 0 {
		t.Fatalf("mixed order failed: %d %s %s", code, stdout, stderr)
	}
	result := clijson.DecodeApplyResult(t, []byte(stdout))
	if result.ActionCount != 6 {
		t.Fatalf("joint order work was dropped or charged twice: %s", stdout)
	}
	seen := map[string]bool{}
	for _, row := range result.RelationOrderResults {
		if row.Outcome != "converged" || !row.Changed {
			t.Fatalf("lost completed order outcome: %+v", row)
		}
		seen[row.Target] = true
	}
	if !seen["pi"] || !seen["opencode"] {
		t.Fatalf("missing order classes: %s", stdout)
	}
	f.requireSources(t, "@acme/alpha@1", "@acme/beta@1", "@acme/foreign@1")
	nativefixture.RequirePiOrderValues(t, settings, "next-theme", "npm:@acme/pi-alpha@1", "npm:@acme/pi-beta@1")
	f.require(t, "status", "--check", "--json")
	f.require(t, "apply", "--yes")
	testkit.AssertFileContent(t, f.calls, "@acme/alpha@1\n@acme/beta@1\n")
}

func TestNativeOpenCodeFailuresPreserveSafety(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		for _, fault := range []string{"key", "outside", "mode", "command", "mode-command"} {
			t.Run(fmt.Sprintf("refresh=%t/%s", refresh, fault), func(t *testing.T) {
				f := newNativeOpenCodeFixture(t, target.ScopeProject, target.ScopeGlobal, false)
				args := []string{"apply", "--yes", "--json"}
				if refresh {
					f.require(t, "apply", "--yes")
					args = []string{"refresh", "extension", "alpha", "--yes", "--json"}
				}
				t.Setenv("DAEM_TEST_OC_FAULT", fault)
				if code, stdout, stderr := f.run(args...); code == 0 {
					t.Fatalf("failed qualification became success: %s %s", stdout, stderr)
				}
				info, err := os.Stat(f.keyFile)
				if err != nil || info.Mode().Perm()&0o022 != 0 {
					t.Fatal("expanded access remained", info, err)
				}
				if fault == "key" && !strings.Contains(string(testkit.ReadFile(t, f.keyFile)), "unexpected") {
					t.Fatal("key conflict was silently repaired")
				}
			})
		}
	}
}

func TestNativeOpenCodeRemovalRetainsPendingUntilVerified(t *testing.T) {
	f := newNativeOpenCodeFixture(t, target.ScopeGlobal, target.ScopeProject, true)
	f.require(t, "apply", "--yes")
	f.require(t, "remove", "extension", "alpha")
	f.require(t, "lock")
	var stdout, stderr bytes.Buffer
	code := testkit.RunVerboseCLIWithOptions([]string{"apply", "--manifest", f.manifest, "--yes", "--json"}, clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr, ApplyExecuteOptions: applyworkflow.ExecuteOptions{CarrierRemovalObserver: func(context.Context, durablecarrier.PendingCarrierRemoval, []durablecarrier.ManagedCarrierClaim) assurancehostroute.ObservationFact {
		return assurancehostroute.ObservationFact{}
	}}})
	if code == 0 {
		t.Fatal("unverified direct removal became success")
	}
	f.requireSources(t, "@acme/foreign@1")
	snapshot, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, filepath.Join(f.workspace, ".daem", "state.json")))
	if err != nil || len(snapshot.PendingCarrierRemovals()) != 1 || !snapshot.ContainsHostConfig() {
		t.Fatalf("pending removal or native state was lost: %v", err)
	}
	f.require(t, "apply", "--yes", "--json")
	f.require(t, "status", "--check", "--json")
	testkit.AssertFileContent(t, f.calls, "@acme/alpha@1\n")
}

func TestNativeOpenCodeInterruptedCommandRecoversWithoutSourceOrReplay(t *testing.T) {
	f := newNativeOpenCodeFixture(t, target.ScopeGlobal, target.ScopeGlobal, false)
	t.Setenv("DAEM_TEST_OC_FAULT", "terminate")
	code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, nativefixture.RecoveryFixture{Manifest: f.manifest, Destination: f.keyFile}, "native-opaque-command", "apply", "--yes")
	if code == 0 || code == nativefixture.RecoveryExit {
		t.Fatalf("command did not kill its parent: %d %s %s", code, stdout, stderr)
	}
	if err := os.Remove(filepath.Join(f.workspace, "preferences.toml")); err != nil {
		t.Fatal(err)
	}
	f.require(t, "recover", "--dry-run")
	f.require(t, "recover", "--yes")
	info, err := os.Stat(f.keyFile)
	if err != nil || info.Mode().Perm()&0o022 != 0 {
		t.Fatal("recovery left expanded access", info, err)
	}
	testkit.WriteFile(t, f.workspace, "preferences.toml", "model='native-model'\n")
	t.Setenv("DAEM_TEST_OC_FAULT", "")
	f.require(t, "apply", "--yes", "--json")
	f.require(t, "status", "--check", "--json")
	testkit.AssertFileContent(t, f.calls, "@acme/alpha@1\n")
	testkit.AssertNoRecoveryArtifacts(t, f.workspace)
}

func TestNativeOpenCodeRemovalWithNativeRetirement(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			f := newNativeOpenCodeFixture(t, scope, target.ScopeGlobal, true)
			f.require(t, "apply", "--yes")
			f.require(t, "remove", "extension", "alpha")
			testkit.WriteFile(t, f.workspace, "preferences.toml", "")
			f.require(t, "lock")
			f.require(t, "apply", "--yes", "--json")
			f.requireSources(t, "@acme/foreign@1")
			if strings.Contains(string(testkit.ReadFile(t, f.keyFile)), "model") {
				t.Fatal("selected native key did not retire")
			}
			f.require(t, "status", "--check", "--json")
			testkit.AssertFileContent(t, f.calls, "@acme/alpha@1\n")
		})
	}
}

func TestNativeOpenCodeRemovalDoesNotCreateAbsentAlternatives(t *testing.T) {
	f := newNativeOpenCodeFixture(t, target.ScopeProject, target.ScopeProject, true)
	f.require(t, "apply", "--yes")
	for _, name := range []string{"opencode.jsonc", "tui.jsonc"} {
		if err := os.Remove(filepath.Join(f.config, name)); err != nil {
			t.Fatal(err)
		}
	}
	f.require(t, "remove", "extension", "alpha")
	f.require(t, "lock")
	f.require(t, "apply", "--yes", "--json")
	for _, name := range []string{"opencode.jsonc", "tui.jsonc"} {
		if _, err := os.Stat(filepath.Join(f.config, name)); !os.IsNotExist(err) {
			t.Fatal("absent alternative was created", name, err)
		}
	}
	for _, name := range []string{"opencode.json", "tui.json"} {
		doc, err := opencodeconfig.Parse(testkit.ReadFile(t, filepath.Join(f.config, name)))
		if err != nil || doc.ExactSourceCount("@acme/alpha@1") != 0 || doc.ExactSourceCount("@acme/foreign@1") != 1 {
			t.Fatal("direct removal lost its exact source boundary", err)
		}
	}
	f.require(t, "status", "--check", "--json")
}
