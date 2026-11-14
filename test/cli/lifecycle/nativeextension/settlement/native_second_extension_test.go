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
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativePiAddsSecondExtensionWithManagedKeys(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, update := range []bool{false, true} {
			for _, reorder := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/update=%t/reorder=%t", scope, update, reorder), func(t *testing.T) {
					fixture := nativefixture.NewPiInvocationAtScope(t, "none", scope)
					fixture.Require(t, "apply", "--yes")
					theme := "fixture-theme"
					if update {
						theme = "next-theme"
						testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{"theme":"next-theme"}`)
					}
					fixture.Require(t, "add", "extension", "extra", "npm:@acme/extra@1", "--target", "pi", "--scope", string(scope))
					fixture.Require(t, "lock")
					setNativePiCommandResult(t, fixture, fmt.Sprintf(`{"theme":%q,"foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1","npm:@acme/extra@1"]}`, theme))
					if reorder {
						setNativePiCommandResult(t, fixture, fmt.Sprintf(`{"theme":%q,"foreign":{"retained":true},"packages":["npm:@acme/extra@1","npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`, theme))
					}
					bin := filepath.Join(filepath.Dir(fixture.BeforeCommand), "bin")
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
						if code, stdout, stderr := fixture.Run("apply", "--yes", "--json"); code == 0 {
							t.Fatalf("unreserved post-command permutation was executed: %s %s", stdout, stderr)
						}
						nativefixture.RequirePiOrderValues(t, fixture.Settings, theme, "npm:@acme/extra@1", "npm:@acme/keep@1", "git:github.com/acme/pi-tools@v1")
						packages = []string{"git:github.com/acme/pi-tools@v1", "npm:@acme/keep@1", "npm:@acme/extra@1"}
					}
					fixture.Require(t, "apply", "--yes", "--json")
					nativefixture.RequirePiOrderValues(t, fixture.Settings, theme, packages...)
					var before struct {
						Theme string `json:"theme"`
					}
					if err := json.Unmarshal(testkit.ReadFile(t, fixture.BeforeCommand), &before); err != nil {
						t.Fatal(err)
					}
					if before.Theme != theme {
						t.Fatal("second extension command did not consume the settled selected key")
					}
					testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nextra\n")
					fixture.Require(t, "status", "--check", "--json")
					fixture.Require(t, "apply", "--yes", "--json")
					testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nextra\n")
					testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
				})
			}
		}
	}
}
