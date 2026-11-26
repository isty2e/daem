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
	"github.com/isty2e/daem/internal/target"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativePiRefreshUsesBoundProtectionAndPreservesManagedKeys(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newNativePiRefreshFixture(t, scope, "")
			manifestBefore := testkit.ReadFile(t, fixture.Manifest)
			lockPath := filepath.Join(fixture.Workspace, "daem.lock.toml")
			lockBefore := testkit.ReadFile(t, lockPath)
			fixture.Require(t, "refresh", "extension", "tools", "--dry-run", "--json")
			testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\n")
			fixture.Require(t, "refresh", "extension", "tools", "--yes", "--json")
			store, err := ownershipstore.New(fixture.LaunchRegistry)
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
			fixture.Require(t, "status", "--check", "--json")
			fixture.Require(t, "refresh", "extension", "tools", "--yes", "--json")
			testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nrefresh\nrefresh\n")
			testkit.AssertFileContent(t, fixture.Manifest, string(manifestBefore))
			testkit.AssertFileContent(t, lockPath, string(lockBefore))
			snapshot, err := statefile.LoadOptional(t.Context(), filepath.Join(fixture.Workspace, ".daem", "state.json"))
			if err != nil || !snapshot.ContainsAgentConfig() {
				t.Fatal("refresh history discarded native state", err)
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiRefreshRequiresCurrentKeysWithoutApplyingThem(t *testing.T) {
	for _, change := range []string{"desired", "host"} {
		t.Run(change, func(t *testing.T) {
			fixture := newNativePiRefreshFixture(t, target.ScopeProject, "")
			if change == "desired" {
				testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{"theme":"new-theme"}`)
				fixture.Require(t, "lock")
			} else {
				testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", `{"theme":"host-change","foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`)
			}
			before := testkit.ReadFile(t, fixture.Settings)
			code, stdout, stderr := fixture.Run("refresh", "extension", "tools", "--yes", "--json")
			if code == 0 {
				t.Fatalf("refresh consumed non-current keys: %s %s", stdout, stderr)
			}
			testkit.AssertFileContent(t, fixture.Settings, string(before))
			testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiRefreshProtectsPermissionsAtInvocation(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeProject, "mode")
	prepared, err := refreshworkflow.PlanWrite(t.Context(), refreshworkflow.CommandInput{ManifestPath: fixture.Manifest, ExtensionID: "tools"}, refreshworkflow.PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := os.Chmod(fixture.Settings, 0o640); err != nil {
		t.Fatal(err)
	}
	result, err := refreshworkflow.Execute(t.Context(), prepared, refreshworkflow.ExecuteOptions{})
	if !result.Attempted || result.ResultClass != refreshworkflow.ResultPartial {
		t.Fatalf("host expansion was not reported: %#v %v", result, err)
	}
	info, err := os.Stat(fixture.Settings)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatal("refresh did not restore the permissions in effect at launch", err)
	}
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nrefresh\n")
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiRefreshQualifiesFailuresWithoutRepairOrInstallReplay(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, fault := range []string{"key", "outside", "mode", "malformed", "command", "command-mode", "tighten"} {
			t.Run(string(scope)+"/"+fault, func(t *testing.T) {
				fixture := nativefixture.NewInstalledPiWithPreferences(t, scope)
				setPiRefreshCommand(t, fixture, fault)
				if fault == "tighten" {
					if err := os.Chmod(fixture.Settings, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				before, err := os.Stat(fixture.Settings)
				if err != nil {
					t.Fatal(err)
				}

				code, stdout, stderr := fixture.Run("refresh", "extension", "tools", "--yes", "--json")
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
				testkit.AssertFileContent(t, fixture.Calls, "install\nrefresh\n")

				after, err := os.Stat(fixture.Settings)
				if err != nil || after.Mode().Perm()&^before.Mode().Perm() != 0 {
					t.Fatal("refresh left broadened access", err)
				}
				if fault == "tighten" && (before.Mode().Perm() != 0o644 || after.Mode().Perm() != 0o600) {
					t.Fatalf("refresh permissions: got %04o -> %04o, want 0644 -> 0600", before.Mode().Perm(), after.Mode().Perm())
				}
				if fault == "malformed" {
					testkit.AssertFileContent(t, fixture.Settings, "[")
				}
				if fault == "key" || fault == "outside" {
					var document struct {
						Theme   string `json:"theme"`
						Foreign struct {
							Retained bool `json:"retained"`
						} `json:"foreign"`
					}
					if err := json.Unmarshal(testkit.ReadFile(t, fixture.Settings), &document); err != nil {
						t.Fatal(err)
					}
					if fault == "key" && document.Theme != "changed" || fault == "outside" && document.Foreign.Retained {
						t.Fatal("refresh silently repaired unexpected content")
					}
				}

				t.Setenv("DAEM_TEST_REFRESH_FAULT", "")
				testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`)
				fixture.Require(t, "refresh", "extension", "tools", "--yes", "--json")
				fixture.Require(t, "status", "--check", "--json")
				testkit.AssertFileContent(t, fixture.Calls, "install\nrefresh\nrefresh\n")
				testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
			})
		}
	}
}

func TestNativeRefreshProtectsInstalledClaudeHooksInTheirProfile(t *testing.T) {
	f := newNativePiRefreshFixture(t, target.ScopeProject, "")
	profile := os.Getenv("CLAUDE_CONFIG_DIR")
	destination := filepath.Join(profile, "settings.json")
	const manifest = `version = 1
targets = ["pi", "claude-code"]
[[extension]]
id = "tools"
targets = ["pi"]
carrier = "pi-package"
scope = "project"
source = {host_source = "git:github.com/acme/pi-tools@v1"}
[[agent_config]]
id = "preferences"
targets = ["pi"]
scope = "project"
path = "settings.json"
source = %q
[[agent_config]]
id = "claude-model"
targets = ["claude-code"]
scope = "global"
path = "settings.json"
source = %q
[[hook]]
name = "notify"
targets = ["claude-code"]
scope = "global"
event = "Stop"
command = "echo done"
`
	source := filepath.Join(f.Workspace, "claude.json")
	testkit.WriteFile(t, f.Workspace, "claude.json", `{"model":"example"}`)
	testkit.WriteFile(t, f.Workspace, "daem.toml", fmt.Sprintf(manifest, filepath.Join(f.Workspace, "preferences.json"), source))
	f.Require(t, "lock")
	f.Require(t, "apply", "--target", "claude-code", "--yes")
	installed := string(testkit.ReadFile(t, destination))

	other := filepath.Join(f.Workspace, "other-profile")
	t.Setenv("CLAUDE_CONFIG_DIR", other)
	code, out, stderr := f.Run("refresh", "extension", "tools", "--dry-run", "--verbose")
	if code == 0 || !strings.Contains(out+stderr, destination) || !strings.Contains(out+stderr, filepath.Join(other, "settings.json")) {
		t.Fatalf("refresh did not identify the separate Claude profiles: %d %s %s", code, out, stderr)
	}
	testkit.AssertFileContent(t, f.Calls, "install\ninstall\n")
	t.Setenv("CLAUDE_CONFIG_DIR", profile)
	f.Require(t, "refresh", "extension", "tools", "--yes")
	testkit.AssertFileContent(t, f.Calls, "install\ninstall\nrefresh\n")
	testkit.AssertFileContent(t, destination, installed)

	t.Setenv("DAEM_TEST_REFRESH_FAULT", "terminate")
	code, out, stderr = nativefixture.InvokeRecoveryCommand(t, nativefixture.RecoveryFixture{Manifest: f.Manifest, Destination: f.Settings}, "native-refresh-command", "refresh", "extension", "tools", "--yes")
	if code == 0 || code == nativefixture.RecoveryExit {
		t.Fatalf("did not interrupt the refresh command: %d %s %s", code, out, stderr)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", other)
	const foreign = `{"private":true}`
	testkit.WriteFile(t, other, "settings.json", foreign)
	if code, out, stderr := f.Run("recover", "--yes"); code == 0 {
		t.Fatalf("recovered a protected Claude document under a different profile: %s %s", out, stderr)
	}
	testkit.AssertFileContent(t, filepath.Join(other, "settings.json"), foreign)
	t.Setenv("CLAUDE_CONFIG_DIR", profile)
	f.Require(t, "recover", "--yes")
	testkit.AssertFileContent(t, f.Calls, "install\ninstall\nrefresh\nrefresh\n")
	testkit.AssertFileContent(t, destination, installed)
}

func TestNativePiRefreshDoesNotResolveUnrelatedDeclaredSources(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeProject, "")
	path := filepath.Join(fixture.Workspace, "unrelated.md")
	testkit.WriteFile(t, fixture.Workspace, "unrelated.md", "not consumed by refresh\n")
	fixture.Require(t, "add", "instruction", "unrelated", path, "--target", "pi")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	fixture.Require(t, "refresh", "extension", "tools", "--yes", "--json")
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nrefresh\n")
}

func TestNativePiRefreshTimeoutStillContainsPermissions(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeProject, "timeout-mode")
	before, err := os.Stat(fixture.Settings)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := fixture.Run("refresh", "extension", "tools", "--timeout", "1s", "--yes", "--json")
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
	after, err := os.Stat(fixture.Settings)
	if err != nil || after.Mode().Perm()&^before.Mode().Perm() != 0 {
		t.Fatal("timeout left broadened access", err)
	}
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nrefresh\n")
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiRefreshUnqualifiedMetadataKeepsRecoveryFence(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeProject, "inaccessible")
	before, err := os.Stat(fixture.Settings)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := fixture.Run("refresh", "extension", "tools", "--yes", "--json")
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
	if code, _, _ := fixture.Run("refresh", "extension", "tools", "--yes", "--json"); code == 0 {
		t.Fatal("unknown metadata became a new baseline")
	}
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nrefresh\n")
	if err := os.Chmod(fixture.Settings, before.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAEM_TEST_REFRESH_FAULT", "")
	fixture.Require(t, "recover", "--dry-run")
	fixture.Require(t, "recover", "--yes")
	fixture.Require(t, "status", "--check", "--json")
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nrefresh\n")
}

func TestNativePiRefreshProcessLossRecoversProtectionWithoutReplay(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeGlobal, "terminate")
	before, err := os.Stat(fixture.Settings)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, nativefixture.RecoveryFixture{Manifest: fixture.Manifest, Destination: fixture.Settings}, "native-refresh-command", "refresh", "extension", "tools", "--yes", "--json")
	if code == 0 || code == nativefixture.RecoveryExit {
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
	if code, _, _ := fixture.Run("refresh", "extension", "tools", "--yes", "--json"); code == 0 {
		t.Fatal("pending protection became a fresh Refresh baseline")
	}
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nrefresh\n")
	other := nativefixture.PiInvocation{Workspace: filepath.Join(filepath.Dir(fixture.Workspace), "other-workspace")}
	other.Manifest = filepath.Join(other.Workspace, "daem.toml")
	testkit.WriteFile(t, other.Workspace, "daem.toml", "version=1\ntargets=['pi']\n[[extension]]\nid='tools'\ncarrier='pi-package'\nscope='global'\nsource={host_source='git:github.com/acme/pi-tools@v1'}\n")
	other.Require(t, "lock")
	if code, _, _ := other.Run("refresh", "extension", "tools", "--yes", "--json"); code == 0 {
		t.Fatal("a declaration without native keys bypassed another owner's pending fence")
	}
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nrefresh\n")
	if err := os.Remove(filepath.Join(fixture.Workspace, "preferences.json")); err != nil {
		t.Fatal(err)
	}
	fixture.Require(t, "recover", "--dry-run")
	fixture.Require(t, "recover", "--yes")
	testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{"theme":"fixture-theme"}`)
	after, err := os.Stat(fixture.Settings)
	if err != nil || after.Mode().Perm()&^before.Mode().Perm() != 0 {
		t.Fatal("recovery left broadened access", err)
	}
	fixture.Require(t, "status", "--check", "--json")
	other.Require(t, "refresh", "extension", "tools", "--dry-run", "--json")
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nrefresh\n")
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
	fixture.Require(t, "refresh", "extension", "tools", "--yes", "--json")
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nrefresh\nrefresh\n")
}

func TestNativePiRefreshProtectsBothSettingsLayersAndAllowsItsPackageRows(t *testing.T) {
	fixture := newNativePiRefreshFixture(t, target.ScopeProject, "both")
	agent := os.Getenv("PI_CODING_AGENT_DIR")
	testkit.WriteFile(t, agent, "settings.json", `{"foreign":{"retained":true},"packages":["git:github.com/acme/pi-tools@v1"]}`)
	globalSource := filepath.Join(fixture.Workspace, "global-preferences.json")
	testkit.WriteFile(t, fixture.Workspace, "global-preferences.json", `{"theme":"global-theme"}`)
	body := string(testkit.ReadFile(t, fixture.Manifest)) + fmt.Sprintf("\n[[agent_config]]\nid='global-preferences'\nscope='global'\npath='settings.json'\nsource=%q\nownership='keys'\n", globalSource)
	testkit.WriteFile(t, fixture.Workspace, "daem.toml", body)
	fixture.Require(t, "lock")
	fixture.Require(t, "apply", "--yes", "--json")
	fixture.Require(t, "refresh", "extension", "tools", "--yes", "--json")
	store, err := ownershipstore.New(fixture.LaunchRegistry)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.LoadProtectionRegistry(t.Context())
	if err != nil || len(registry.PendingProtections()) != 1 || len(registry.PendingProtections()[0].Documents()) != 2 {
		t.Fatal("cross-scope refresh omitted a protected layer", err)
	}
	fixture.Require(t, "status", "--check", "--json")
	testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nrefresh\n")
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func newNativePiRefreshFixture(t *testing.T, scope target.Scope, fault string) nativefixture.PiInvocation {
	t.Helper()
	fixture := nativefixture.NewPiInvocationAtScope(t, "", scope)
	fixture.Require(t, "apply", "--yes", "--json")
	setPiRefreshCommand(t, fixture, fault)
	return fixture
}

func setPiRefreshCommand(t *testing.T, fixture nativefixture.PiInvocation, fault string) {
	t.Helper()
	bin := filepath.Join(filepath.Dir(fixture.Workspace), "bin")
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
