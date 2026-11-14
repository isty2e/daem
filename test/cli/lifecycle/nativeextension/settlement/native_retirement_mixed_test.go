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
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func setNativePiCommandResult(t *testing.T, fixture nativefixture.PiInvocation, content string) {
	t.Helper()
	path := filepath.Join(fixture.Workspace, "pi-result.json")
	testkit.WriteFile(t, fixture.Workspace, "pi-result.json", content)
	t.Setenv("DAEM_TEST_NATIVE_RESULT", path)
}

func TestNativePiRetirementWithRemainingKeys(t *testing.T) {
	for _, variant := range []string{"replace and add", "current keys", "absent retired key"} {
		t.Run(variant, func(t *testing.T) {
			fixture := nativefixture.NewPiInvocation(t, "")
			testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{"theme":"fixture-theme","obsolete":true}`)
			setNativePiCommandResult(t, fixture, `{"theme":"fixture-theme","obsolete":true,"foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`)
			fixture.Require(t, "lock")
			fixture.Require(t, "apply", "--yes")
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
			testkit.WriteFile(t, fixture.Workspace, "preferences.json", selected)
			testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", before)
			setNativePiCommandResult(t, fixture, after)
			fixture.Require(t, "lock")
			fixture.Require(t, "apply", "--yes")
			fixture.LaunchProtection(t)
			nativefixture.RequireSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.BeforeCommand), selected, true)
			if variant != "absent retired key" {
				nativefixture.RequireSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.BeforeCommand), `{"obsolete":true}`, true)
			}
			content := testkit.ReadFile(t, fixture.Settings)
			nativefixture.RequireSharedValues(t, configdocument.JSON, content, selected, true)
			nativefixture.RequireSharedValues(t, configdocument.JSON, content, `{"obsolete":true}`, false)
			nativefixture.RequireSharedValues(t, configdocument.JSON, content, `{"foreign":{"retained":true}}`, true)
			fixture.Require(t, "status", "--check", "--json")
			fixture.Require(t, "apply", "--yes")
			if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\ninstall\n" || !bytes.Equal(content, testkit.ReadFile(t, fixture.Settings)) {
				t.Fatal("remaining-key convergence repeated effects")
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiChangedRetiringKeyStopsSuffixWithoutRepair(t *testing.T) {
	fixture := nativePiLastKeyRetirementFixture(t, target.ScopeProject, true, "")
	t.Setenv("DAEM_TEST_MUTATION", "key")
	if code, stdout, stderr := fixture.Run("apply", "--yes"); code == 0 {
		t.Fatalf("host-modified retiring key was accepted: %s %s", stdout, stderr)
	}
	fixture.LaunchProtection(t)
	nativefixture.RequireSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.Settings), `{"theme":"changed"}`, true)
	state, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, filepath.Join(fixture.Workspace, ".daem", "state.json")))
	if err != nil || !state.ContainsHostConfig() {
		t.Fatal("failed retirement discarded its managed baseline", err)
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiLastKeyRetirementProcessLossDoesNotReplayInstall(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := nativePiLastKeyRetirementFixture(t, target.ScopeProject, true, "")
			recoveryFixture := nativefixture.RecoveryFixture{Manifest: fixture.Manifest, Destination: fixture.Settings}
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, recoveryFixture, "native-ordinary-"+boundary, "apply", "--yes")
			if code != nativefixture.RecoveryExit {
				t.Fatalf("retiring suffix did not reach its actual process-loss boundary: %d %s %s", code, stdout, stderr)
			}
			fixture.LaunchProtection(t)
			fixture.Require(t, "recover", "--dry-run")
			fixture.Require(t, "recover", "--yes")
			fixture.Require(t, "apply", "--yes")
			nativefixture.RequireSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.Settings), `{"theme":"fixture-theme"}`, false)
			fixture.Require(t, "status", "--check", "--json")
			if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\ninstall\n" {
				t.Fatal("last-key recovery/retry replayed its completed installation")
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}
