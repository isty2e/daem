//go:build darwin || linux

package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func nativePiLastKeyRetirementFixture(t *testing.T, scope target.Scope, omitted bool, mutation string) nativefixture.PiInvocation {
	t.Helper()
	fixture := nativefixture.NewPiInvocationAtScope(t, mutation, scope)
	fixture.Require(t, "apply", "--yes")
	if omitted {
		manifest := string(testkit.ReadFile(t, fixture.Manifest))
		index := strings.LastIndex(manifest, "[[host_config]]")
		if index < 0 {
			t.Fatal("fixture native declaration is missing")
		}
		testkit.WriteFile(t, fixture.Workspace, "daem.toml", manifest[:index])
	} else {
		testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{}`)
	}
	testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
	fixture.Require(t, "lock")
	return fixture
}

func TestNativePiLastKeyRetirementFollowsInvocation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, omitted := range []bool{false, true} {
			name := string(scope) + "/empty selection"
			if omitted {
				name = string(scope) + "/omitted declaration"
			}
			t.Run(name, func(t *testing.T) {
				fixture := nativePiLastKeyRetirementFixture(t, scope, omitted, "")
				fixture.Require(t, "apply", "--dry-run")
				fixture.Require(t, "apply", "--yes")
				fixture.LaunchProtection(t)
				nativefixture.RequireSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.BeforeCommand), `{"theme":"fixture-theme"}`, true)
				content := testkit.ReadFile(t, fixture.Settings)
				nativefixture.RequireSharedValues(t, configdocument.JSON, content, `{"theme":"fixture-theme"}`, false)
				nativefixture.RequireSharedValues(t, configdocument.JSON, content, `{"foreign":{"retained":true}}`, true)
				paths, err := daempaths.Resolve(fixture.Manifest)
				if err != nil {
					t.Fatal(err)
				}
				state, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, paths.StatefilePath))
				if err != nil || state.ContainsHostConfig() {
					t.Fatal("last native key remained managed after its suffix", err)
				}
				store, err := ownershipstore.New(paths.OwnershipRegistryPath)
				if err != nil {
					t.Fatal(err)
				}
				registry, err := store.LoadProtectionRegistry(t.Context())
				if err != nil || len(registry.DocumentBindings()) != 0 || len(registry.PendingProtections()) != 0 {
					t.Fatal("last key retained a stale document binding or safety record", err)
				}
				fixture.Require(t, "status", "--check", "--json")
				fixture.Require(t, "apply", "--yes")
				if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\ninstall\n" || !bytes.Equal(content, testkit.ReadFile(t, fixture.Settings)) {
					t.Fatal("converged retirement repeated its installation or direct effects")
				}
				testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
			})
		}
	}
}

func TestNativePiRetirementFailurePreservesKey(t *testing.T) {
	fixture := nativePiLastKeyRetirementFixture(t, target.ScopeProject, false, "")
	t.Setenv("DAEM_TEST_MUTATION", "command")
	if code, stdout, stderr := fixture.Run("apply", "--yes"); code == 0 {
		t.Fatalf("failed installation was accepted: %s %s", stdout, stderr)
	}
	fixture.LaunchProtection(t)
	nativefixture.RequireSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.Settings), `{"theme":"fixture-theme"}`, true)
	if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\ninstall\n" {
		t.Fatal("failed installation was skipped or replayed")
	}
	if _, err := os.Stat(filepath.Join(fixture.Workspace, ".daem", "state.json")); err != nil {
		t.Fatal("failed retirement lost its actual native state", err)
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiRetiringMissingDocumentNeedsNoDummyProtection(t *testing.T) {
	fixture := nativePiLastKeyRetirementFixture(t, target.ScopeProject, true, "")
	if err := os.Remove(fixture.Settings); err != nil {
		t.Fatal(err)
	}
	setNativePiCommandResult(t, fixture, `{"packages":["git:github.com/acme/pi-tools@v1"]}`)
	fixture.Require(t, "apply", "--yes")
	store, err := ownershipstore.New(fixture.LaunchRegistry)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.LoadProtectionRegistry(t.Context())
	if err != nil || len(registry.PendingProtections()) != 0 {
		t.Fatal("an absent retiring document manufactured safety work", err)
	}
	state, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, fixture.LaunchState))
	if err != nil || len(state.PendingCarrierInstalls()) != 1 || !state.ContainsHostConfig() {
		t.Fatal("unprotected invocation lost correlation or its retiring baseline", err)
	}
	nativefixture.RequireSharedValues(t, configdocument.JSON, testkit.ReadFile(t, fixture.Settings), `{"theme":"fixture-theme"}`, false)
	fixture.Require(t, "status", "--check", "--json")
	fixture.Require(t, "apply", "--yes")
	if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\ninstall\n" {
		t.Fatal("retiring an absent document replayed installation")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}
