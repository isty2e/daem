//go:build darwin || linux

package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativePiRemovalWithManagedKeys(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, update := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/update=%t", scope, update), func(t *testing.T) {
				fixture, theme := nativePiRemovalFixture(t, scope, update)
				fixture.Require(t, "apply", "--yes", "--json")
				nativefixture.RequirePiOrderValues(t, fixture.Settings, theme, "npm:@acme/keep@1")
				var before struct {
					Theme string `json:"theme"`
				}
				if err := json.Unmarshal(testkit.ReadFile(t, fixture.BeforeCommand), &before); err != nil || before.Theme != theme {
					t.Fatal("removal did not consume the settled selected key", err)
				}
				snapshot, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, fixture.LaunchState))
				if err != nil || len(snapshot.PendingCarrierRemovals()) != 1 || len(snapshot.PendingCarrierInstalls()) != 0 {
					t.Fatal("removal was released without its exact pending removal", err)
				}
				store, err := ownershipstore.New(fixture.LaunchRegistry)
				if err != nil {
					t.Fatal(err)
				}
				registry, err := store.LoadProtectionRegistry(t.Context())
				if err != nil || len(registry.PendingProtections()) != 1 {
					t.Fatal("removal had no durable protection record", err)
				}
				row := registry.PendingProtections()[0]
				if _, bound := row.Execution(); !bound || row.Phase() != ownership.ProtectionBound {
					t.Fatal("removal started before protection was bound")
				}
				if row.Correlation().RequestFingerprint() != strings.TrimPrefix(snapshot.PendingCarrierRemovals()[0].RemoveRequest().CanonicalRequestHash(), "sha256:") {
					t.Fatal("protection and pending removal name different route requests")
				}
				fixture.Require(t, "status", "--check", "--json")
				fixture.Require(t, "apply", "--yes", "--json")
				testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nremove\n")
				paths, err := daempaths.Resolve(fixture.Manifest)
				if err != nil {
					t.Fatal(err)
				}
				settled, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, paths.StatefilePath))
				if err != nil || len(settled.PendingCarrierRemovals()) != 0 {
					t.Fatal("pending removal was not retired", err)
				}
				testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
			})
		}
	}
}

func TestNativePiRemovalFailuresRetainPendingAndRecoverWithoutBlindReplay(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, fault := range []string{"command", "key", "outside", "malformed", "mode"} {
			t.Run(string(scope)+"/"+fault, func(t *testing.T) {
				fixture, theme := nativePiRemovalFixture(t, scope, false)
				before, err := os.Stat(fixture.Settings)
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("DAEM_TEST_REMOVAL_FAULT", fault)
				if code, stdout, stderr := fixture.Run("apply", "--yes", "--json"); code == 0 {
					t.Fatalf("removal failure was accepted: %s %s", stdout, stderr)
				}
				paths, err := daempaths.Resolve(fixture.Manifest)
				if err != nil {
					t.Fatal(err)
				}
				state, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, paths.StatefilePath))
				if err != nil || len(state.PendingCarrierRemovals()) != 1 {
					t.Fatal("failed removal lost its exact pending obligation", err)
				}
				after, err := os.Stat(fixture.Settings)
				if err != nil || after.Mode().Perm()&^before.Mode().Perm() != 0 {
					t.Fatal("failed removal retained expanded permissions", err)
				}
				actual := string(testkit.ReadFile(t, fixture.Settings))
				if fault == "key" && !strings.Contains(actual, `"theme":"changed"`) || fault == "outside" && !strings.Contains(actual, `"retained":false`) || fault == "malformed" && actual != "[" {
					t.Fatal("removal qualification silently repaired host-written content", actual)
				}
				calls := string(testkit.ReadFile(t, fixture.Calls))
				if strings.Count(calls, "remove\n") != 1 {
					t.Fatal("failed removal was repeated", calls)
				}
				t.Setenv("DAEM_TEST_REMOVAL_FAULT", "")
				if fault != "command" {
					testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", fmt.Sprintf(`{"theme":%q,"foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`, theme))
				}
				fixture.Require(t, "apply", "--yes", "--json")
				fixture.Require(t, "status", "--check", "--json")
				nativefixture.RequirePiOrderValues(t, fixture.Settings, theme, "npm:@acme/keep@1")
				wanted := 1
				if fault == "command" {
					wanted = 2
				}
				if got := strings.Count(string(testkit.ReadFile(t, fixture.Calls)), "remove\n"); got != wanted {
					t.Fatalf("retry issued %d removals, want %d", got, wanted)
				}
				testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
			})
		}
	}
}

func TestNativePiRemovalPrecedesKeyRetirementAndOrdinarySuffix(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture, _ := nativePiRemovalFixture(t, scope, false)
			testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{}`)
			fixture.AddInstructions(t, "instructions after removal\n")
			fixture.Require(t, "apply", "--yes", "--json")
			var before, after struct {
				Theme *string `json:"theme"`
			}
			if err := json.Unmarshal(testkit.ReadFile(t, fixture.BeforeCommand), &before); err != nil || before.Theme == nil || *before.Theme != "fixture-theme" {
				t.Fatal("retiring key disappeared before removal consumed it", err)
			}
			if err := json.Unmarshal(testkit.ReadFile(t, fixture.Settings), &after); err != nil || after.Theme != nil {
				t.Fatal("retiring key was not removed in the actual suffix", err)
			}
			testkit.AssertFileContent(t, fixture.InstructionsBefore, "absent\n")
			testkit.AssertFileContent(t, filepath.Join(fixture.Workspace, "AGENTS.md"), "instructions after removal\n")
			fixture.Require(t, "status", "--check", "--json")
			fixture.Require(t, "apply", "--yes", "--json")
			testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nremove\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiRemovalProcessLossRecoversWithoutCommandReplay(t *testing.T) {
	fixture, theme := nativePiRemovalFixture(t, target.ScopeProject, false)
	t.Setenv("DAEM_TEST_REMOVAL_FAULT", "terminate")
	code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, nativefixture.RecoveryFixture{Manifest: fixture.Manifest, Destination: fixture.Settings}, "native-removal-command", "apply", "--yes")
	if code == 0 || code == nativefixture.RecoveryExit {
		t.Fatalf("expected loss of the isolated CLI process: %d %s %s", code, stdout, stderr)
	}
	nativefixture.RequirePiOrderValues(t, fixture.Settings, theme, "npm:@acme/keep@1")
	store, err := ownershipstore.New(fixture.LaunchRegistry)
	if err != nil {
		t.Fatal(err)
	}
	launched, err := store.LoadProtectionRegistry(t.Context())
	if err != nil || len(launched.PendingProtections()) != 1 || launched.PendingProtections()[0].Phase() != ownership.ProtectionBound {
		t.Fatal("lost CLI process did not leave its bound protection evidence", err)
	}
	paths, err := daempaths.Resolve(fixture.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, paths.StatefilePath))
	if err != nil || len(pending.PendingCarrierRemovals()) != 1 {
		t.Fatal("lost removal did not remain pending", err)
	}
	t.Setenv("DAEM_TEST_REMOVAL_FAULT", "")
	fixture.Require(t, "recover", "--dry-run")
	fixture.Require(t, "recover", "--yes")
	fixture.Require(t, "apply", "--yes", "--json")
	fixture.Require(t, "status", "--check", "--json")
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nremove\n")
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiRemovalSettlesVerifiedAbsenceAfterFailedCommand(t *testing.T) {
	fixture, theme := nativePiRemovalFixture(t, target.ScopeProject, false)
	t.Setenv("DAEM_TEST_REMOVAL_FAULT", "command-after")
	code, stdout, stderr := fixture.Run("apply", "--yes", "--json")
	if code != 0 {
		t.Fatalf("verified removal was hidden by its mechanical exit: %d %s %s", code, stdout, stderr)
	}
	var result struct {
		Attempts []struct {
			Class       string `json:"result_class"`
			Reason      string `json:"reason"`
			Observation string `json:"observation"`
			ExitCode    *int   `json:"exit_code"`
		} `json:"host_route_attempts"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Attempts) != 1 || result.Attempts[0].Class != "failed" || result.Attempts[0].Reason != "nonzero_exit" || result.Attempts[0].Observation != "missing" || result.Attempts[0].ExitCode == nil || *result.Attempts[0].ExitCode != 23 {
		t.Fatalf("settlement erased the failed command's actual result: %s", stdout)
	}
	nativefixture.RequirePiOrderValues(t, fixture.Settings, theme, "npm:@acme/keep@1")
	fixture.Require(t, "status", "--check", "--json")
	fixture.Require(t, "apply", "--yes", "--json")
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nremove\n")
}

func TestNativePiRemovalAndInstallShareOneProtectedSchedule(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture, theme := nativePiRemovalFixture(t, scope, true)
			fixture.Require(t, "add", "extension", "extra", "npm:@acme/extra@1", "--target", "pi", "--scope", string(scope))
			fixture.Require(t, "lock")
			resultPath := filepath.Join(fixture.Workspace, "extra-result.json")
			testkit.WriteFile(t, fixture.Workspace, "extra-result.json", fmt.Sprintf(`{"theme":%q,"foreign":{"retained":true},"packages":["npm:@acme/keep@1","npm:@acme/extra@1"]}`, theme))
			t.Setenv("DAEM_TEST_SECOND_RESULT", resultPath)
			fixture.Require(t, "apply", "--yes", "--json")
			nativefixture.RequirePiOrderValues(t, fixture.Settings, theme, "npm:@acme/keep@1", "npm:@acme/extra@1")
			testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nremove\ninstall\n")
			fixture.Require(t, "status", "--check", "--json")
			fixture.Require(t, "apply", "--yes", "--json")
			testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nremove\ninstall\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func nativePiRemovalFixture(t *testing.T, scope target.Scope, update bool) (nativefixture.PiInvocation, string) {
	t.Helper()
	fixture := nativefixture.NewPiInvocationAtScope(t, "none", scope)
	fixture.Require(t, "apply", "--yes")
	t.Setenv("DAEM_TEST_REMOVAL_FAULT", "")
	theme := "fixture-theme"
	if update {
		theme = "next-theme"
		testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{"theme":"next-theme"}`)
	}
	fixture.Require(t, "remove", "extension", "tools")
	fixture.Require(t, "lock")
	setNativePiCommandResult(t, fixture, fmt.Sprintf(`{"theme":%q,"foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`, theme))
	bin := filepath.Join(filepath.Dir(fixture.BeforeCommand), "bin")
	testkit.WriteFile(t, bin, "pi", `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '1.0.2\n'; exit 0; fi
case "$1" in
  remove) [ "$2" = 'git:github.com/acme/pi-tools@v1' ] || exit 97; result="$DAEM_TEST_NATIVE_RESULT" ;;
  install) [ "$2" = 'npm:@acme/extra@1' ] || exit 97; result="$DAEM_TEST_SECOND_RESULT" ;;
  *) exit 97 ;;
esac
if [ "$DAEM_TEST_SCOPE" = project ]; then
  [ "$#" -eq 3 ] && [ "$3" = -l ] || exit 97
else
  [ "$#" -eq 2 ] || exit 97
fi
/bin/cp "$DAEM_TEST_SETTINGS" "$DAEM_TEST_COMMAND_BEFORE"
/bin/cp .daem/state.json "$DAEM_TEST_LAUNCH_STATE"
/bin/cp "$DAEM_TEST_REGISTRY" "$DAEM_TEST_LAUNCH_REGISTRY"
if [ -f "$DAEM_TEST_INSTRUCTIONS_TARGET" ]; then
  { printf 'present\n'; /bin/cat "$DAEM_TEST_INSTRUCTIONS_TARGET"; } > "$DAEM_TEST_INSTRUCTIONS_BEFORE"
else
  printf 'absent\n' > "$DAEM_TEST_INSTRUCTIONS_BEFORE"
fi
printf '%s\n' "$1" >> "$DAEM_TEST_COMMAND_CALLS"
if [ "${DAEM_TEST_REMOVAL_FAULT:-}" = command ]; then exit 23; fi
/bin/cp "$result" "$DAEM_TEST_SETTINGS"
case "${DAEM_TEST_REMOVAL_FAULT:-}" in
  key) printf '%s\n' '{"theme":"changed","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}' > "$DAEM_TEST_SETTINGS" ;;
  outside) printf '%s\n' '{"theme":"fixture-theme","foreign":{"retained":false},"packages":["npm:@acme/keep@1"]}' > "$DAEM_TEST_SETTINGS" ;;
  malformed) printf '[' > "$DAEM_TEST_SETTINGS" ;;
  mode) /bin/chmod 666 "$DAEM_TEST_SETTINGS" ;;
  command-after) exit 23 ;;
  terminate) kill -KILL "$DAEM_TEST_REMOVAL_PARENT_PID"; exit 0 ;;
esac
`)
	if err := os.Chmod(filepath.Join(bin, "pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	return fixture, theme
}
