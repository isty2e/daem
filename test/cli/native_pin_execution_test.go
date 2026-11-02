//go:build darwin || linux

package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/statefile"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

const (
	nativePinBefore = "git:github.com/acme/pi-tools@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	nativePinAfter  = "git:github.com/acme/pi-tools@bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func nativePiPinFixture(t *testing.T, scope target.Scope, mode, mutation string) nativePiInvocationFixture {
	t.Helper()
	f := nativePiInvocationFixtureForSource(t, mutation, scope, nativePinBefore)
	testkit.WriteFile(t, filepath.Dir(f.settings), "settings.json", fmt.Sprintf(`{"foreign":{"retained":true},"packages":["npm:@acme/keep@1",%q]}`, nativePinBefore))
	if mode != "new" {
		f.require(t, "apply", "--yes")
	}
	if got := string(testkit.ReadFile(t, f.calls)); got != "install\n" {
		t.Fatal("settling native settings repeated the already installed old pin")
	}
	declaration := strings.ReplaceAll(string(testkit.ReadFile(t, f.manifest)), nativePinBefore, nativePinAfter)
	switch mode {
	case "changed":
		testkit.WriteFile(t, f.workspace, "preferences.json", `{"theme":"updated-theme"}`)
	case "retiring":
		declaration, _, _ = strings.Cut(declaration, "\n[[host_config]]")
	case "whole":
		testkit.WriteFile(t, f.workspace, "dedicated.json", `{"enabled":true}`)
		declaration += fmt.Sprintf("\n[[host_config]]\nid='dedicated'\nscope=%q\npath='dedicated.json'\nsource=%q\nownership='document'\n", scope, filepath.Join(f.workspace, "dedicated.json"))
		testkit.WriteFile(t, f.workspace, "daem.toml", strings.ReplaceAll(declaration, nativePinAfter, nativePinBefore))
		f.require(t, "lock")
		f.require(t, "apply", "--yes")
	}
	testkit.WriteFile(t, f.workspace, "daem.toml", declaration)
	command := filepath.Join(filepath.Dir(f.calls), "bin", "pi")
	script := strings.ReplaceAll(string(testkit.ReadFile(t, command)), nativePinBefore, nativePinAfter)
	if mode == "changed" {
		script = strings.ReplaceAll(script, "fixture-theme", "updated-theme")
	}
	testkit.WriteFile(t, filepath.Dir(command), "pi", script)
	if scope == target.ScopeGlobal {
		paths, err := daempaths.Resolve(f.manifest)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("DAEM_TEST_PIN_CLAIMS", paths.CarrierClaimRegistryPath)
		t.Setenv("DAEM_TEST_PIN_LAUNCH_CLAIMS", filepath.Join(filepath.Dir(f.calls), "launch-pin-claims.json"))
	}
	f.require(t, "lock")
	return f
}

func nativePinClaims(t *testing.T, f nativePiInvocationFixture, scope target.Scope, launched bool) []durablecarrier.ManagedCarrierClaim {
	t.Helper()
	paths, err := daempaths.Resolve(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if scope == target.ScopeGlobal {
		path := paths.CarrierClaimRegistryPath
		if launched {
			path = os.Getenv("DAEM_TEST_PIN_LAUNCH_CLAIMS")
		}
		store, err := carrierclaimstore.New(path)
		if err != nil {
			t.Fatal(err)
		}
		registry, err := store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return registry.Claims()
	}
	path := paths.StatefilePath
	if launched {
		path = f.launchState
	}
	state, err := statefile.Decode(testkit.ReadFile(t, path))
	if err != nil {
		t.Fatal(err)
	}
	return state.ManagedCarrierClaims()
}

func nativePinLaunch(t *testing.T, f nativePiInvocationFixture, scope target.Scope) durable.Snapshot {
	t.Helper()
	claims := nativePinClaims(t, f, scope, true)
	if len(claims) != 1 {
		t.Fatalf("pin launch claims=%d", len(claims))
	}
	pair, pending := claims[0].PendingPinTransition()
	if !pending || pair.Before().Identity().Carrier().Key().Source().Ref() != nativePinBefore ||
		pair.Identity().Carrier().Key().Source().Ref() != nativePinAfter ||
		claims[0].Identity().Carrier().Key().Source().Ref() != nativePinBefore {
		t.Fatal("command launched without exact durable pin intent and prior management")
	}
	f.launchNativeProtection(t)
	state, err := statefile.Decode(testkit.ReadFile(t, f.launchState))
	if err != nil || len(state.ManagedAggregates()) != 1 {
		t.Fatal("pin intent checkpoint lost its settled native key", err)
	}
	return state
}

func TestNativePiPinTransitionUsesProtectedInvocation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, mode := range []string{"new", "current", "changed", "retiring", "whole"} {
			t.Run(string(scope)+"/"+mode, func(t *testing.T) {
				f := nativePiPinFixture(t, scope, mode, "")
				f.addInstructions(t, "post-pin instructions\n")
				f.require(t, "apply", "--yes")
				launched := nativePinLaunch(t, f, scope)
				if mode == "whole" && len(launched.ManagedPaths()) != 1 {
					t.Fatal("pin intent checkpoint lost its settled whole-file prefix")
				}
				var before struct {
					Theme string `json:"theme"`
				}
				want := "fixture-theme"
				if mode == "changed" {
					want = "updated-theme"
				}
				if err := json.Unmarshal(testkit.ReadFile(t, f.beforeCommand), &before); err != nil || before.Theme != want {
					t.Fatal("selected key prefix did not precede the pin command", err)
				}
				if got := string(testkit.ReadFile(t, f.instructionsBefore)); got != "absent\n" {
					t.Fatal("ordinary suffix preceded the pin command")
				}
				claims := nativePinClaims(t, f, scope, false)
				if len(claims) != 1 || claims[0].Identity().Carrier().Key().Source().Ref() != nativePinAfter ||
					claims[0].Provenance() != durablecarrier.ClaimProvenancePinTransitionObserved {
					t.Fatal("qualified pin did not replace exact prior management")
				}
				if _, pending := claims[0].PendingPinTransition(); pending {
					t.Fatal("successful pin left its intent unsettled")
				}
				paths, err := daempaths.Resolve(f.manifest)
				if err != nil {
					t.Fatal(err)
				}
				state, err := statefile.Decode(testkit.ReadFile(t, paths.StatefilePath))
				keys := 1
				if mode == "retiring" {
					keys = 0
				}
				if err != nil || len(state.ManagedAggregates()) != keys {
					t.Fatal("pin completion/history/suffix lost native membership", err)
				}
				var settings map[string]json.RawMessage
				if err := json.Unmarshal(testkit.ReadFile(t, f.settings), &settings); err != nil {
					t.Fatal(err)
				}
				if _, theme := settings["theme"]; theme != (keys != 0) {
					t.Fatal("retiring key was not removed by the late suffix")
				}
				if mode == "whole" {
					if got := string(testkit.ReadFile(t, filepath.Join(filepath.Dir(f.settings), "dedicated.json"))); got != `{"enabled":true}` {
						t.Fatal("pin lost the dedicated whole-file prefix")
					}
				}
				f.require(t, "status", "--check", "--json")
				f.require(t, "apply", "--yes")
				if got := string(testkit.ReadFile(t, f.calls)); got != "install\ninstall\n" {
					t.Fatal("settled pin was skipped or replayed")
				}
				testkit.AssertNoRecoveryArtifacts(t, f.workspace)
			})
		}
	}
}

func TestNativePiPinFailureDoesNotCompleteFromDesiredPackagePresence(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, mutation := range []string{"key", "outside", "mode", "command"} {
			t.Run(string(scope)+"/"+mutation, func(t *testing.T) {
				f := nativePiPinFixture(t, scope, "current", mutation)
				f.addInstructions(t, "must remain unapplied\n")
				initialMode, err := os.Stat(f.settings)
				if err != nil {
					t.Fatal(err)
				}
				if code, stdout, stderr := f.run("apply", "--yes", "--json"); code == 0 {
					t.Fatalf("unqualified pin succeeded: %s %s", stdout, stderr)
				}
				nativePinLaunch(t, f, scope)
				claims := nativePinClaims(t, f, scope, false)
				if len(claims) != 1 {
					t.Fatalf("failed pin claims=%d", len(claims))
				}
				if _, pending := claims[0].PendingPinTransition(); !pending || claims[0].Identity().Carrier().Key().Source().Ref() != nativePinBefore {
					t.Fatal("failed native qualification completed the desired pin passively")
				}
				if _, err := os.Stat(filepath.Join(f.workspace, "AGENTS.md")); !os.IsNotExist(err) {
					t.Fatal("pin failure did not stop ordinary suffix", err)
				}
				if info, err := os.Stat(f.settings); err != nil || info.Mode().Perm() != initialMode.Mode().Perm() {
					t.Fatal("pin failed to contain introduced permissions", err)
				}
				// Repair selected/outside values, but keep the already desired package.
				testkit.WriteFile(t, filepath.Dir(f.settings), "settings.json", fmt.Sprintf(`{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1",%q]}`, nativePinAfter))
				t.Setenv("DAEM_TEST_MUTATION", "")
				f.require(t, "lock")
				f.require(t, "apply", "--yes")
				if got := string(testkit.ReadFile(t, f.calls)); got != "install\ninstall\ninstall\n" {
					t.Fatal("uncertain pin retry skipped a real native invocation")
				}
				claims = nativePinClaims(t, f, scope, false)
				if len(claims) != 1 || claims[0].Identity().Carrier().Key().Source().Ref() != nativePinAfter {
					t.Fatal("authorized retry did not settle the exact desired pin")
				}
				if _, pending := claims[0].PendingPinTransition(); pending {
					t.Fatal("retry left a pending pin transition")
				}
				f.require(t, "status", "--check", "--json")
				testkit.AssertNoRecoveryArtifacts(t, f.workspace)
			})
		}
	}
}

func TestNativePiPinSuffixRecoveryDoesNotRepeatCompletedPin(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, boundary := range []string{"host", "state"} {
			t.Run(string(scope)+"/"+boundary, func(t *testing.T) {
				f := nativePiPinFixture(t, scope, "current", "")
				f.addInstructions(t, "recoverable pin suffix\n")
				recovery := nativeRecoveryFixture{manifest: f.manifest, destination: filepath.Join(f.workspace, "AGENTS.md")}
				code, stdout, stderr := invokeNativeRecoveryCommand(t, recovery, "native-ordinary-"+boundary, "apply", "--yes")
				if code != nativeRecoveryExit {
					t.Fatalf("pin suffix did not reach its process-loss boundary: %d %s %s", code, stdout, stderr)
				}
				nativePinLaunch(t, f, scope)
				f.require(t, "recover", "--dry-run")
				f.require(t, "recover", "--yes")
				f.require(t, "apply", "--yes")
				if got := string(testkit.ReadFile(t, recovery.destination)); got != "recoverable pin suffix\n" {
					t.Fatal("ordinary recovery lost the post-pin file")
				}
				if got := string(testkit.ReadFile(t, f.calls)); got != "install\ninstall\n" {
					t.Fatal("ordinary recovery replayed a completed pin")
				}
				claims := nativePinClaims(t, f, scope, false)
				if len(claims) != 1 || claims[0].Identity().Carrier().Key().Source().Ref() != nativePinAfter {
					t.Fatal("recovery reverted settled pin management")
				}
				if _, pending := claims[0].PendingPinTransition(); pending {
					t.Fatal("recovery restored an already completed pin intent")
				}
				f.require(t, "status", "--check", "--json")
				testkit.AssertNoRecoveryArtifacts(t, f.workspace)
			})
		}
	}
}

func TestNativePiPinCommandProcessLossRetainsUncertainIntent(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			f := nativePiPinFixture(t, scope, "current", "")
			f.addInstructions(t, "post-retry suffix\n")
			initial, err := os.Stat(f.settings)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("DAEM_TEST_MUTATION", "pin-parent-loss")
			recovery := nativeRecoveryFixture{manifest: f.manifest, destination: filepath.Join(f.workspace, "AGENTS.md")}
			if code, stdout, stderr := invokeNativeRecoveryCommand(t, recovery, "native-opaque-command", "apply", "--yes"); code == 0 {
				t.Fatalf("lost parent reported success: %s %s", stdout, stderr)
			}
			nativePinLaunch(t, f, scope)
			claims := nativePinClaims(t, f, scope, false)
			if len(claims) != 1 {
				t.Fatalf("interrupted pin claims=%d", len(claims))
			}
			if _, pending := claims[0].PendingPinTransition(); !pending || claims[0].Identity().Carrier().Key().Source().Ref() != nativePinBefore {
				t.Fatal("process loss silently completed the pin")
			}
			if info, err := os.Stat(f.settings); err != nil || info.Mode().Perm() != 0o666 {
				t.Fatal("fixture did not lose its parent after widening access", err)
			}
			f.require(t, "recover", "--dry-run")
			f.require(t, "recover", "--yes")
			if info, err := os.Stat(f.settings); err != nil || info.Mode().Perm() != initial.Mode().Perm() {
				t.Fatal("permission recovery did not restore the recorded ceiling", err)
			}
			if got := string(testkit.ReadFile(t, f.calls)); got != "install\ninstall\n" {
				t.Fatal("permission recovery invoked a pin command")
			}
			t.Setenv("DAEM_TEST_MUTATION", "")
			f.require(t, "apply", "--yes")
			if got := string(testkit.ReadFile(t, f.calls)); got != "install\ninstall\ninstall\n" {
				t.Fatal("uncertain command was completed from passive package observation")
			}
			f.require(t, "status", "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, f.workspace)
		})
	}
}
