//go:build darwin || linux

package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/target"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativePiRefreshUsesBoundProtectionAndPreservesManagedKeys(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newNativePiRefreshFixture(t, scope, "")
			prepared, err := refreshworkflow.PlanWrite(t.Context(), refreshworkflow.CommandInput{ManifestPath: fixture.manifest, ExtensionID: "tools"}, refreshworkflow.PlanOptions{})
			if err != nil {
				t.Fatal("current native Refresh planning", err)
			}
			if err := prepared.Close(); err != nil {
				t.Fatal(err)
			}
			manifestBefore := testkit.ReadFile(t, fixture.manifest)
			lockPath := filepath.Join(fixture.workspace, "daem.lock.toml")
			lockBefore := testkit.ReadFile(t, lockPath)
			fixture.require(t, "refresh", "extension", "tools", "--dry-run", "--json")
			testkit.AssertFileContent(t, fixture.calls, "install\ninstall\n")
			fixture.require(t, "refresh", "extension", "tools", "--yes", "--json")
			store, err := ownershipstore.New(fixture.launchRegistry)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := store.LoadProtectionRegistry(t.Context())
			if err != nil || len(registry.PendingProtections()) != 1 {
				t.Fatal("refresh command was released without its protection record", err)
			}
			row := registry.PendingProtections()[0]
			if _, bound := row.Execution(); !bound || row.Phase() != ownership.ProtectionBound {
				t.Fatal("refresh command was released before its execution was bound")
			}
			fixture.require(t, "status", "--check", "--json")
			fixture.require(t, "refresh", "extension", "tools", "--yes", "--json")
			testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\nrefresh\n")
			testkit.AssertFileContent(t, fixture.manifest, string(manifestBefore))
			testkit.AssertFileContent(t, lockPath, string(lockBefore))
			snapshot, err := statefile.LoadOptional(t.Context(), filepath.Join(fixture.workspace, ".daem", "state.json"))
			if err != nil || !snapshot.ContainsHostConfig() {
				t.Fatal("refresh history discarded native state", err)
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}

func TestNativePiRefreshRequiresCurrentKeysWithoutApplyingThem(t *testing.T) {
	for _, change := range []string{"desired", "host"} {
		t.Run(change, func(t *testing.T) {
			fixture := newNativePiRefreshFixture(t, target.ScopeProject, "")
			if change == "desired" {
				testkit.WriteFile(t, fixture.workspace, "preferences.json", `{"theme":"new-theme"}`)
				fixture.require(t, "lock")
			} else {
				testkit.WriteFile(t, filepath.Dir(fixture.settings), "settings.json", `{"theme":"host-change","foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`)
			}
			before := testkit.ReadFile(t, fixture.settings)
			code, stdout, stderr := fixture.run("refresh", "extension", "tools", "--yes", "--json")
			if code == 0 {
				t.Fatalf("refresh consumed non-current keys: %s %s", stdout, stderr)
			}
			testkit.AssertFileContent(t, fixture.settings, string(before))
			testkit.AssertFileContent(t, fixture.calls, "install\ninstall\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}

func TestNativePiRefreshRevalidatesDisclosedPermissionBaseline(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeProject, "")
	prepared, err := refreshworkflow.PlanWrite(t.Context(), refreshworkflow.CommandInput{ManifestPath: fixture.manifest, ExtensionID: "tools"}, refreshworkflow.PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := os.Chmod(fixture.settings, 0o666); err != nil {
		t.Fatal(err)
	}
	result, err := refreshworkflow.Execute(t.Context(), prepared, refreshworkflow.ExecuteOptions{})
	if err == nil || result.Attempted || result.ResultClass != refreshworkflow.ResultRefused {
		t.Fatalf("changed permission baseline was reused: %#v %v", result, err)
	}
	testkit.AssertFileContent(t, fixture.calls, "install\ninstall\n")
}

func TestNativePiRefreshQualifiesFailuresWithoutRepairOrInstallReplay(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, fault := range []string{"key", "outside", "mode", "malformed", "command", "command-mode", "tighten"} {
			t.Run(string(scope)+"/"+fault, func(t *testing.T) {
				fixture := newNativePiRefreshFixture(t, scope, fault)
				before, err := os.Stat(fixture.settings)
				if err != nil {
					t.Fatal(err)
				}
				code, stdout, stderr := fixture.run("refresh", "extension", "tools", "--yes", "--json")
				want := "partial"
				if fault == "command" {
					want = "failed"
				}
				if fault == "tighten" {
					want = "attempted_unverified"
				}
				if (code == 0) != (fault == "tighten") || decodeNativeRefreshClass(t, stdout) != want {
					t.Fatalf("wrong qualified outcome: %d %s %s", code, stdout, stderr)
				}
				testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\n")
				after, err := os.Stat(fixture.settings)
				if err != nil || after.Mode().Perm()&^before.Mode().Perm() != 0 {
					t.Fatal("refresh left broadened access", err)
				}
				if fault == "malformed" {
					testkit.AssertFileContent(t, fixture.settings, "[")
				}
				if fault == "key" || fault == "outside" {
					var document struct {
						Theme   string `json:"theme"`
						Foreign struct {
							Retained bool `json:"retained"`
						} `json:"foreign"`
					}
					if err := json.Unmarshal(testkit.ReadFile(t, fixture.settings), &document); err != nil {
						t.Fatal(err)
					}
					if fault == "key" && document.Theme != "changed" || fault == "outside" && document.Foreign.Retained {
						t.Fatal("refresh silently repaired unexpected content")
					}
				}
				t.Setenv("DAEM_TEST_REFRESH_FAULT", "")
				testkit.WriteFile(t, filepath.Dir(fixture.settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`)
				fixture.require(t, "refresh", "extension", "tools", "--yes", "--json")
				fixture.require(t, "status", "--check", "--json")
				testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\nrefresh\n")
				testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
			})
		}
	}
}

func TestNativePiRefreshDoesNotResolveUnrelatedDeclaredSources(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeProject, "")
	path := filepath.Join(fixture.workspace, "unrelated.md")
	testkit.WriteFile(t, fixture.workspace, "unrelated.md", "not consumed by refresh\n")
	fixture.require(t, "add", "instruction", "unrelated", path, "--target", "pi")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	fixture.require(t, "refresh", "extension", "tools", "--yes", "--json")
	testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\n")
}

func TestNativePiRefreshTimeoutStillContainsPermissions(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeProject, "timeout-mode")
	before, err := os.Stat(fixture.settings)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := fixture.run("refresh", "extension", "tools", "--timeout", "1s", "--yes", "--json")
	if code == 0 || decodeNativeRefreshClass(t, stdout) != "partial" {
		t.Fatalf("timed-out protected Refresh was not partial: %d %s %s", code, stdout, stderr)
	}
	var result struct {
		Result struct {
			Attempted bool `json:"attempted"`
			Process   struct {
				TimedOut bool `json:"timed_out"`
			} `json:"process_outcome"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Result.Attempted || !result.Result.Process.TimedOut {
		t.Fatalf("timeout lost its actual attempt: %s", stdout)
	}
	after, err := os.Stat(fixture.settings)
	if err != nil || after.Mode().Perm()&^before.Mode().Perm() != 0 {
		t.Fatal("timeout left broadened access", err)
	}
	testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\n")
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativePiRefreshUnqualifiedMetadataKeepsRecoveryFence(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeProject, "inaccessible")
	before, err := os.Stat(fixture.settings)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := fixture.run("refresh", "extension", "tools", "--yes", "--json")
	if code == 0 || decodeNativeRefreshClass(t, stdout) != "partial" {
		t.Fatalf("unqualified metadata was not partial: %d %s %s", code, stdout, stderr)
	}
	store, err := ownershipstore.New(os.Getenv("DAEM_TEST_REGISTRY"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.LoadProtectionRegistry(t.Context())
	if err != nil || len(registry.PendingProtections()) != 1 {
		t.Fatal("unqualified document lost its fence", err)
	}
	if code, _, _ := fixture.run("refresh", "extension", "tools", "--yes", "--json"); code == 0 {
		t.Fatal("unknown metadata became a new baseline")
	}
	testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\n")
	if err := os.Chmod(fixture.settings, before.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAEM_TEST_REFRESH_FAULT", "")
	fixture.require(t, "recover", "--dry-run")
	fixture.require(t, "recover", "--yes")
	fixture.require(t, "status", "--check", "--json")
	testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\n")
}

func TestNativePiRefreshProcessLossRecoversProtectionWithoutReplay(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeGlobal, "terminate")
	before, err := os.Stat(fixture.settings)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := invokeNativeRecoveryCommand(t, nativeRecoveryFixture{manifest: fixture.manifest, destination: fixture.settings}, "native-refresh-command", "refresh", "extension", "tools", "--yes", "--json")
	if code == 0 || code == nativeRecoveryExit {
		t.Fatalf("isolated Refresh was not terminated: %d %s %s", code, stdout, stderr)
	}
	store, err := ownershipstore.New(os.Getenv("DAEM_TEST_REGISTRY"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.LoadProtectionRegistry(t.Context())
	if err != nil || len(registry.PendingProtections()) != 1 || registry.PendingProtections()[0].Phase() != ownership.ProtectionBound {
		t.Fatal("lost Refresh did not retain its bound protection", err)
	}
	t.Setenv("DAEM_TEST_REFRESH_FAULT", "")
	if code, _, _ := fixture.run("refresh", "extension", "tools", "--yes", "--json"); code == 0 {
		t.Fatal("pending protection became a fresh Refresh baseline")
	}
	testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\n")
	other := nativePiInvocationFixture{workspace: filepath.Join(filepath.Dir(fixture.workspace), "other-workspace")}
	other.manifest = filepath.Join(other.workspace, "daem.toml")
	testkit.WriteFile(t, other.workspace, "daem.toml", "version=1\ntargets=['pi']\n[[extension]]\nid='tools'\ncarrier='pi-package'\nscope='global'\nsource={host_source='git:github.com/acme/pi-tools@v1'}\n")
	other.require(t, "lock")
	if code, _, _ := other.run("refresh", "extension", "tools", "--yes", "--json"); code == 0 {
		t.Fatal("a declaration without native keys bypassed another owner's pending fence")
	}
	testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\n")
	if err := os.Remove(filepath.Join(fixture.workspace, "preferences.json")); err != nil {
		t.Fatal(err)
	}
	fixture.require(t, "recover", "--dry-run")
	fixture.require(t, "recover", "--yes")
	testkit.WriteFile(t, fixture.workspace, "preferences.json", `{"theme":"fixture-theme"}`)
	after, err := os.Stat(fixture.settings)
	if err != nil || after.Mode().Perm()&^before.Mode().Perm() != 0 {
		t.Fatal("recovery left broadened access", err)
	}
	fixture.require(t, "status", "--check", "--json")
	other.require(t, "refresh", "extension", "tools", "--dry-run", "--json")
	testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\n")
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
	fixture.require(t, "refresh", "extension", "tools", "--yes", "--json")
	testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\nrefresh\n")
}

func TestNativePiRefreshProtectsBothSettingsLayersAndAllowsItsPackageRows(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeProject, "both")
	agent := os.Getenv("PI_CODING_AGENT_DIR")
	testkit.WriteFile(t, agent, "settings.json", `{"foreign":{"retained":true},"packages":["git:github.com/acme/pi-tools@v1"]}`)
	globalSource := filepath.Join(fixture.workspace, "global-preferences.json")
	testkit.WriteFile(t, fixture.workspace, "global-preferences.json", `{"theme":"global-theme"}`)
	body := string(testkit.ReadFile(t, fixture.manifest)) + fmt.Sprintf("\n[[host_config]]\nid='global-preferences'\nscope='global'\npath='settings.json'\nsource=%q\nownership='keys'\n", globalSource)
	testkit.WriteFile(t, fixture.workspace, "daem.toml", body)
	fixture.require(t, "lock")
	fixture.require(t, "apply", "--yes", "--json")
	fixture.require(t, "refresh", "extension", "tools", "--yes", "--json")
	store, err := ownershipstore.New(fixture.launchRegistry)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.LoadProtectionRegistry(t.Context())
	if err != nil || len(registry.PendingProtections()) != 1 || len(registry.PendingProtections()[0].Documents()) != 2 {
		t.Fatal("cross-scope refresh omitted a protected layer", err)
	}
	fixture.require(t, "status", "--check", "--json")
	testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nrefresh\n")
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func newNativePiRefreshFixture(t *testing.T, scope target.Scope, fault string) nativePiInvocationFixture {
	t.Helper()
	fixture := newNativePiInvocationFixtureAtScope(t, "", scope)
	fixture.require(t, "apply", "--yes", "--json")
	bin := filepath.Join(filepath.Dir(fixture.workspace), "bin")
	testkit.WriteFile(t, bin, "pi", `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '1.0.2\n'; exit 0; fi
[ "$#" -eq 3 ] && [ "$1" = update ] && [ "$2" = --extension ] && [ "$3" = 'git:github.com/acme/pi-tools@v1' ] || exit 97
/bin/cp "$DAEM_TEST_SETTINGS" "$DAEM_TEST_COMMAND_BEFORE"
/bin/cp "$DAEM_TEST_REGISTRY" "$DAEM_TEST_LAUNCH_REGISTRY"
/bin/cp .daem/state.json "$DAEM_TEST_LAUNCH_STATE"
printf 'refresh\n' >> "$DAEM_TEST_COMMAND_CALLS"
case "$DAEM_TEST_REFRESH_FAULT" in
  key) printf '%s\n' '{"theme":"changed","foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}' > "$DAEM_TEST_SETTINGS" ;;
  outside) printf '%s\n' '{"theme":"fixture-theme","foreign":{"retained":false},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}' > "$DAEM_TEST_SETTINGS" ;;
  mode) /bin/chmod 666 "$DAEM_TEST_SETTINGS" ;;
  tighten) /bin/chmod 600 "$DAEM_TEST_SETTINGS" ;;
  inaccessible) /bin/chmod 000 "$DAEM_TEST_SETTINGS" ;;
  malformed) printf '[' > "$DAEM_TEST_SETTINGS" ;;
  command) exit 23 ;;
  command-mode) /bin/chmod 666 "$DAEM_TEST_SETTINGS"; exit 23 ;;
  timeout-mode) /bin/chmod 666 "$DAEM_TEST_SETTINGS"; exec /bin/sleep 3 ;;
  terminate) /bin/chmod 666 "$DAEM_TEST_SETTINGS"; kill -KILL "$DAEM_TEST_REFRESH_PARENT_PID"; exit 0 ;;
  both)
    printf '%s\n' '{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1",{"source":"git:github.com/acme/pi-tools@v1"}]}' > "$DAEM_TEST_SETTINGS"
    printf '%s\n' '{"theme":"global-theme","foreign":{"retained":true},"packages":[{"source":"git:github.com/acme/pi-tools@v1"}]}' > "$PI_CODING_AGENT_DIR/settings.json"
    ;;
esac
`)
	if err := os.Chmod(filepath.Join(bin, "pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAEM_TEST_REFRESH_FAULT", fault)
	return fixture
}

func decodeNativeRefreshClass(t *testing.T, content string) string {
	t.Helper()
	var result struct {
		Result struct {
			Class string `json:"class"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatal(err)
	}
	return result.Result.Class
}
