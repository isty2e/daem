//go:build darwin || linux

package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func setNativePiCommandResult(t *testing.T, fixture nativePiInvocationFixture, content string) {
	t.Helper()
	path := filepath.Join(fixture.workspace, "pi-result.json")
	testkit.WriteFile(t, fixture.workspace, "pi-result.json", content)
	t.Setenv("DAEM_TEST_NATIVE_RESULT", path)
}

func TestNativePiRetirementWithRemainingKeys(t *testing.T) {
	for _, variant := range []string{"replace and add", "current keys", "absent retired key"} {
		t.Run(variant, func(t *testing.T) {
			fixture := newNativePiInvocationFixture(t, "")
			testkit.WriteFile(t, fixture.workspace, "preferences.json", `{"theme":"fixture-theme","obsolete":true}`)
			setNativePiCommandResult(t, fixture, `{"theme":"fixture-theme","obsolete":true,"foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`)
			fixture.require(t, "lock")
			fixture.require(t, "apply", "--yes")
			selected := `{"theme":"fixture-theme"}`
			before := `{"theme":"fixture-theme","obsolete":true,"foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`
			after := `{"theme":"fixture-theme","obsolete":true,"foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`
			if variant == "replace and add" {
				selected = `{"theme":"next-theme","added":7}`
				after = `{"theme":"next-theme","added":7,"obsolete":true,"foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`
			} else if variant == "absent retired key" {
				before = `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`
				after = `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`
			}
			testkit.WriteFile(t, fixture.workspace, "preferences.json", selected)
			testkit.WriteFile(t, filepath.Dir(fixture.settings), "settings.json", before)
			setNativePiCommandResult(t, fixture, after)
			fixture.require(t, "lock")
			fixture.require(t, "apply", "--yes")
			fixture.launchProtection(t)
			requireNativeSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.beforeCommand), selected, true)
			if variant != "absent retired key" {
				requireNativeSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.beforeCommand), `{"obsolete":true}`, true)
			}
			content := testkit.ReadFile(t, fixture.settings)
			requireNativeSharedValues(t, configdocument.JSON, content, selected, true)
			requireNativeSharedValues(t, configdocument.JSON, content, `{"obsolete":true}`, false)
			requireNativeSharedValues(t, configdocument.JSON, content, `{"foreign":{"retained":true}}`, true)
			fixture.require(t, "status", "--check", "--json")
			fixture.require(t, "apply", "--yes")
			if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\ninstall\n" || !bytes.Equal(content, testkit.ReadFile(t, fixture.settings)) {
				t.Fatal("remaining-key convergence repeated effects")
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}

func TestNativePiChangedRetiringKeyStopsSuffixWithoutRepair(t *testing.T) {
	fixture := nativePiLastKeyRetirementFixture(t, target.ScopeProject, true, "")
	t.Setenv("DAEM_TEST_MUTATION", "key")
	if code, stdout, stderr := fixture.run("apply", "--yes"); code == 0 {
		t.Fatalf("host-modified retiring key was accepted: %s %s", stdout, stderr)
	}
	fixture.launchProtection(t)
	requireNativeSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.settings), `{"theme":"changed"}`, true)
	state, err := statefile.Decode(testkit.ReadFile(t, filepath.Join(fixture.workspace, ".daem", "state.json")))
	if err != nil || !state.ContainsHostConfig() {
		t.Fatal("failed retirement discarded its managed baseline", err)
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativePiLastKeyRetirementProcessLossDoesNotReplayInstall(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := nativePiLastKeyRetirementFixture(t, target.ScopeProject, true, "")
			recoveryFixture := nativeRecoveryFixture{manifest: fixture.manifest, destination: fixture.settings}
			code, stdout, stderr := invokeNativeRecoveryCommand(t, recoveryFixture, "native-ordinary-"+boundary, "apply", "--yes")
			if code != nativeRecoveryExit {
				t.Fatalf("retiring suffix did not reach its actual process-loss boundary: %d %s %s", code, stdout, stderr)
			}
			fixture.launchProtection(t)
			fixture.require(t, "recover", "--dry-run")
			fixture.require(t, "recover", "--yes")
			fixture.require(t, "apply", "--yes")
			requireNativeSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.settings), `{"theme":"fixture-theme"}`, false)
			fixture.require(t, "status", "--check", "--json")
			if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\ninstall\n" {
				t.Fatal("last-key recovery/retry replayed its completed installation")
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}
