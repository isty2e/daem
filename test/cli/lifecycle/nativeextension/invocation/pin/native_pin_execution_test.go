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
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

const (
	nativePinBefore = "git:github.com/acme/pi-tools@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	nativePinAfter  = "git:github.com/acme/pi-tools@bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func nativePiPinFixture(t *testing.T, scope target.Scope, mode, mutation string) nativefixture.PiInvocation {
	t.Helper()
	f := nativefixture.NewPiInvocationForSource(t, mutation, scope, nativePinBefore)
	testkit.WriteFile(t, filepath.Dir(f.Settings), "settings.json", fmt.Sprintf(`{"foreign":{"retained":true},"packages":["npm:@acme/keep@1",%q]}`, nativePinBefore))
	if mode != "new" {
		f.Require(t, "apply", "--yes")
	}
	if got := string(testkit.ReadFile(t, f.Calls)); got != "install\n" {
		t.Fatal("settling native settings repeated the already installed old pin")
	}
	declaration := strings.ReplaceAll(string(testkit.ReadFile(t, f.Manifest)), nativePinBefore, nativePinAfter)
	switch mode {
	case "changed":
		testkit.WriteFile(t, f.Workspace, "preferences.json", `{"theme":"updated-theme"}`)
	case "retiring":
		declaration, _, _ = strings.Cut(declaration, "\n[[host_config]]")
	case "whole":
		testkit.WriteFile(t, f.Workspace, "dedicated.json", `{"enabled":true}`)
		declaration += fmt.Sprintf("\n[[host_config]]\nid='dedicated'\nscope=%q\npath='dedicated.json'\nsource=%q\nownership='document'\n", scope, filepath.Join(f.Workspace, "dedicated.json"))
		testkit.WriteFile(t, f.Workspace, "daem.toml", strings.ReplaceAll(declaration, nativePinAfter, nativePinBefore))
		f.Require(t, "lock")
		f.Require(t, "apply", "--yes")
	}
	testkit.WriteFile(t, f.Workspace, "daem.toml", declaration)
	command := filepath.Join(filepath.Dir(f.Calls), "bin", "pi")
	script := strings.ReplaceAll(string(testkit.ReadFile(t, command)), nativePinBefore, nativePinAfter)
	if mode == "changed" {
		script = strings.ReplaceAll(script, "fixture-theme", "updated-theme")
	}
	testkit.WriteFile(t, filepath.Dir(command), "pi", script)
	if scope == target.ScopeGlobal {
		paths, err := daempaths.Resolve(f.Manifest)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("DAEM_TEST_PIN_CLAIMS", paths.CarrierClaimRegistryPath)
		t.Setenv("DAEM_TEST_PIN_LAUNCH_CLAIMS", filepath.Join(filepath.Dir(f.Calls), "launch-pin-claims.json"))
	}
	f.Require(t, "lock")
	return f
}

func nativePinClaims(t *testing.T, f nativefixture.PiInvocation, scope target.Scope, launched bool) []durablecarrier.ManagedCarrierClaim {
	t.Helper()
	paths, err := daempaths.Resolve(f.Manifest)
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
		path = f.LaunchState
	}
	state, err := statefile.Decode(testkit.ReadFile(t, path))
	if err != nil {
		t.Fatal(err)
	}
	return state.ManagedCarrierClaims()
}

func nativePinLaunch(t *testing.T, f nativefixture.PiInvocation, scope target.Scope) durable.Snapshot {
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
	f.LaunchNativeProtection(t)
	state, err := statefile.Decode(testkit.ReadFile(t, f.LaunchState))
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
				f.AddInstructions(t, "post-pin instructions\n")
				f.Require(t, "apply", "--yes")
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
				if err := json.Unmarshal(testkit.ReadFile(t, f.BeforeCommand), &before); err != nil || before.Theme != want {
					t.Fatal("selected key prefix did not precede the pin command", err)
				}
				if got := string(testkit.ReadFile(t, f.InstructionsBefore)); got != "absent\n" {
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
				paths, err := daempaths.Resolve(f.Manifest)
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
				if err := json.Unmarshal(testkit.ReadFile(t, f.Settings), &settings); err != nil {
					t.Fatal(err)
				}
				if _, theme := settings["theme"]; theme != (keys != 0) {
					t.Fatal("retiring key was not removed by the late suffix")
				}
				if mode == "whole" {
					if got := string(testkit.ReadFile(t, filepath.Join(filepath.Dir(f.Settings), "dedicated.json"))); got != `{"enabled":true}` {
						t.Fatal("pin lost the dedicated whole-file prefix")
					}
				}
				f.Require(t, "status", "--check", "--json")
				f.Require(t, "apply", "--yes")
				if got := string(testkit.ReadFile(t, f.Calls)); got != "install\ninstall\n" {
					t.Fatal("settled pin was skipped or replayed")
				}
				testkit.AssertNoRecoveryArtifacts(t, f.Workspace)
			})
		}
	}
}

func TestNativePiPinFailureDoesNotCompleteFromDesiredPackagePresence(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, mutation := range []string{"key", "outside", "mode", "command"} {
			t.Run(string(scope)+"/"+mutation, func(t *testing.T) {
				f := nativePiPinFixture(t, scope, "current", mutation)
				f.AddInstructions(t, "must remain unapplied\n")
				initialMode, err := os.Stat(f.Settings)
				if err != nil {
					t.Fatal(err)
				}
				if code, stdout, stderr := f.Run("apply", "--yes", "--json"); code == 0 {
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
				if _, err := os.Stat(filepath.Join(f.Workspace, "AGENTS.md")); !os.IsNotExist(err) {
					t.Fatal("pin failure did not stop ordinary suffix", err)
				}
				if info, err := os.Stat(f.Settings); err != nil || info.Mode().Perm() != initialMode.Mode().Perm() {
					t.Fatal("pin failed to contain introduced permissions", err)
				}
				// Repair selected/outside values, but keep the already desired package.
				testkit.WriteFile(t, filepath.Dir(f.Settings), "settings.json", fmt.Sprintf(`{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1",%q]}`, nativePinAfter))
				t.Setenv("DAEM_TEST_MUTATION", "")
				f.Require(t, "lock")
				f.Require(t, "apply", "--yes")
				if got := string(testkit.ReadFile(t, f.Calls)); got != "install\ninstall\ninstall\n" {
					t.Fatal("uncertain pin retry skipped a real native invocation")
				}
				claims = nativePinClaims(t, f, scope, false)
				if len(claims) != 1 || claims[0].Identity().Carrier().Key().Source().Ref() != nativePinAfter {
					t.Fatal("authorized retry did not settle the exact desired pin")
				}
				if _, pending := claims[0].PendingPinTransition(); pending {
					t.Fatal("retry left a pending pin transition")
				}
				f.Require(t, "status", "--check", "--json")
				testkit.AssertNoRecoveryArtifacts(t, f.Workspace)
			})
		}
	}
}

func TestNativePiPinSuffixRecoveryDoesNotRepeatCompletedPin(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, boundary := range []string{"host", "state"} {
			t.Run(string(scope)+"/"+boundary, func(t *testing.T) {
				f := nativePiPinFixture(t, scope, "current", "")
				f.AddInstructions(t, "recoverable pin suffix\n")
				recovery := nativefixture.RecoveryFixture{Manifest: f.Manifest, Destination: filepath.Join(f.Workspace, "AGENTS.md")}
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, recovery, "native-ordinary-"+boundary, "apply", "--yes")
				if code != nativefixture.RecoveryExit {
					t.Fatalf("pin suffix did not reach its process-loss boundary: %d %s %s", code, stdout, stderr)
				}
				nativePinLaunch(t, f, scope)
				f.Require(t, "recover", "--dry-run")
				f.Require(t, "recover", "--yes")
				f.Require(t, "apply", "--yes")
				if got := string(testkit.ReadFile(t, recovery.Destination)); got != "recoverable pin suffix\n" {
					t.Fatal("ordinary recovery lost the post-pin file")
				}
				if got := string(testkit.ReadFile(t, f.Calls)); got != "install\ninstall\n" {
					t.Fatal("ordinary recovery replayed a completed pin")
				}
				claims := nativePinClaims(t, f, scope, false)
				if len(claims) != 1 || claims[0].Identity().Carrier().Key().Source().Ref() != nativePinAfter {
					t.Fatal("recovery reverted settled pin management")
				}
				if _, pending := claims[0].PendingPinTransition(); pending {
					t.Fatal("recovery restored an already completed pin intent")
				}
				f.Require(t, "status", "--check", "--json")
				testkit.AssertNoRecoveryArtifacts(t, f.Workspace)
			})
		}
	}
}

func TestNativePiPinCommandProcessLossRetainsUncertainIntent(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			f := nativePiPinFixture(t, scope, "current", "")
			f.AddInstructions(t, "post-retry suffix\n")
			initial, err := os.Stat(f.Settings)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("DAEM_TEST_MUTATION", "pin-parent-loss")
			recovery := nativefixture.RecoveryFixture{Manifest: f.Manifest, Destination: filepath.Join(f.Workspace, "AGENTS.md")}
			if code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, recovery, "native-opaque-command", "apply", "--yes"); code == 0 {
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
			if info, err := os.Stat(f.Settings); err != nil || info.Mode().Perm() != 0o666 {
				t.Fatal("fixture did not lose its parent after widening access", err)
			}
			f.Require(t, "recover", "--dry-run")
			f.Require(t, "recover", "--yes")
			if info, err := os.Stat(f.Settings); err != nil || info.Mode().Perm() != initial.Mode().Perm() {
				t.Fatal("permission recovery did not restore the recorded ceiling", err)
			}
			if got := string(testkit.ReadFile(t, f.Calls)); got != "install\ninstall\n" {
				t.Fatal("permission recovery invoked a pin command")
			}
			t.Setenv("DAEM_TEST_MUTATION", "")
			f.Require(t, "apply", "--yes")
			if got := string(testkit.ReadFile(t, f.Calls)); got != "install\ninstall\ninstall\n" {
				t.Fatal("uncertain command was completed from passive package observation")
			}
			f.Require(t, "status", "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, f.Workspace)
		})
	}
}
