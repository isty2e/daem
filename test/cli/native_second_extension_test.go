//go:build darwin || linux

package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativePiAddsSecondExtensionWithManagedKeys(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, update := range []bool{false, true} {
			for _, reorder := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/update=%t/reorder=%t", scope, update, reorder), func(t *testing.T) {
					fixture := newNativePiInvocationFixtureAtScope(t, "none", scope)
					fixture.require(t, "apply", "--yes")
					theme := "fixture-theme"
					if update {
						theme = "next-theme"
						testkit.WriteFile(t, fixture.workspace, "preferences.json", `{"theme":"next-theme"}`)
					}
					fixture.require(t, "add", "extension", "extra", "npm:@acme/extra@1", "--target", "pi", "--scope", string(scope))
					fixture.require(t, "lock")
					setNativePiCommandResult(t, fixture, fmt.Sprintf(`{"theme":%q,"foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1","npm:@acme/extra@1"]}`, theme))
					if reorder {
						setNativePiCommandResult(t, fixture, fmt.Sprintf(`{"theme":%q,"foreign":{"retained":true},"packages":["npm:@acme/extra@1","npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`, theme))
					}
					bin := filepath.Join(filepath.Dir(fixture.beforeCommand), "bin")
					testkit.WriteFile(t, bin, "pi", `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '1.0.2\n'; exit 0; fi
[ "$1" = install ] && [ "$2" = 'npm:@acme/extra@1' ] || exit 97
if [ "$DAEM_TEST_SCOPE" = project ]; then
  [ "$#" -eq 3 ] && [ "$3" = -l ] || exit 97
else
  [ "$#" -eq 2 ] || exit 97
fi
/bin/cp "$DAEM_TEST_SETTINGS" "$DAEM_TEST_COMMAND_BEFORE"
printf 'extra\n' >> "$DAEM_TEST_COMMAND_CALLS"
/bin/cp "$DAEM_TEST_NATIVE_RESULT" "$DAEM_TEST_SETTINGS"
`)
					if err := os.Chmod(filepath.Join(bin, "pi"), 0o700); err != nil {
						t.Fatal(err)
					}
					packages := []string{"npm:@acme/keep@1", "git:github.com/acme/pi-tools@v1", "npm:@acme/extra@1"}
					if reorder {
						if code, stdout, stderr := fixture.run("apply", "--yes", "--json"); code == 0 {
							t.Fatalf("unreserved post-command permutation was executed: %s %s", stdout, stderr)
						}
						requireNativePiOrderValues(t, fixture.settings, theme, "npm:@acme/extra@1", "npm:@acme/keep@1", "git:github.com/acme/pi-tools@v1")
						packages = []string{"git:github.com/acme/pi-tools@v1", "npm:@acme/keep@1", "npm:@acme/extra@1"}
					}
					fixture.require(t, "apply", "--yes", "--json")
					requireNativePiOrderValues(t, fixture.settings, theme, packages...)
					var before struct {
						Theme string `json:"theme"`
					}
					if err := json.Unmarshal(testkit.ReadFile(t, fixture.beforeCommand), &before); err != nil {
						t.Fatal(err)
					}
					if before.Theme != theme {
						t.Fatal("second extension command did not consume the settled selected key")
					}
					testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nextra\n")
					fixture.require(t, "status", "--check", "--json")
					fixture.require(t, "apply", "--yes", "--json")
					testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nextra\n")
					testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
				})
			}
		}
	}
}
