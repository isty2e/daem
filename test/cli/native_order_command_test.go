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
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestNativePiOrderAfterExtensionCommand(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, update := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/update=%t", scope, update), func(t *testing.T) {
				fixture := newNativePiOrderCommandFixture(t, scope)
				theme := "fixture-theme"
				if update {
					theme = "next-theme"
					testkit.WriteFile(t, fixture.workspace, "preferences.json", `{"theme":"next-theme"}`)
				}
				fixture.require(t, "lock")
				setNativePiCommandResult(t, fixture, fmt.Sprintf(`{"theme":%q,"foreign":{"retained":true},"packages":["npm:@acme/keep@1","npm:@acme/extra@1","git:github.com/acme/pi-tools@v1"]}`, theme))
				fixture.require(t, "apply", "--yes", "--json")
				requireNativePiOrderValues(t, fixture.settings, theme, "npm:@acme/keep@1", "git:github.com/acme/pi-tools@v1", "npm:@acme/extra@1")
				var before struct {
					Theme string `json:"theme"`
				}
				if err := json.Unmarshal(testkit.ReadFile(t, fixture.beforeCommand), &before); err != nil || before.Theme != theme {
					t.Fatal("command did not consume the settled selected key", err)
				}
				fixture.require(t, "status", "--check", "--json")
				fixture.require(t, "apply", "--yes", "--json")
				testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nextra\n")
				testkit.AssertFileContent(t, filepath.Join(fixture.workspace, "AGENTS.md"), "instructions after ordered extensions\n")
				testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
			})
		}
	}
}

func TestNativePiOrderCommandSuffixRecoveryUsesPostCommandBefore(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, boundary := range []string{"host", "state"} {
			t.Run(string(scope)+"/"+boundary, func(t *testing.T) {
				fixture := newNativePiOrderCommandFixture(t, scope)
				testkit.WriteFile(t, fixture.workspace, "preferences.json", `{}`)
				fixture.require(t, "lock")
				code, stdout, stderr := invokeNativeRecoveryCommand(t, nativeRecoveryFixture{manifest: fixture.manifest, destination: fixture.settings}, "native-ordinary-"+boundary, "apply", "--yes")
				if code != nativeRecoveryExit {
					t.Fatalf("combined order/key/file suffix missed its interruption: %d %s %s", code, stdout, stderr)
				}
				fixture.require(t, "recover", "--dry-run")
				fixture.require(t, "recover", "--yes")
				if boundary == "host" {
					requireNativePiOrderValues(t, fixture.settings, "fixture-theme", "npm:@acme/keep@1", "npm:@acme/extra@1", "git:github.com/acme/pi-tools@v1")
				} else {
					requireNativePiOrderValues(t, fixture.settings, "", "npm:@acme/keep@1", "git:github.com/acme/pi-tools@v1", "npm:@acme/extra@1")
				}
				fixture.require(t, "apply", "--yes", "--json")
				requireNativePiOrderValues(t, fixture.settings, "", "npm:@acme/keep@1", "git:github.com/acme/pi-tools@v1", "npm:@acme/extra@1")
				var document map[string]json.RawMessage
				if err := json.Unmarshal(testkit.ReadFile(t, fixture.settings), &document); err != nil {
					t.Fatal(err)
				}
				if _, retained := document["theme"]; retained {
					t.Fatal("key retirement did not share the actual suffix")
				}
				fixture.require(t, "status", "--check", "--json")
				testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nextra\n")
				testkit.AssertFileContent(t, filepath.Join(fixture.workspace, "AGENTS.md"), "instructions after ordered extensions\n")
				testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
			})
		}
	}
}

func TestNativePiOrderCommandSuffixReturnedErrorsReportActualSettlement(t *testing.T) {
	for _, boundary := range []string{"return-error", "return-state-error"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newNativePiOrderCommandFixture(t, target.ScopeProject)
			testkit.WriteFile(t, fixture.workspace, "preferences.json", `{}`)
			fixture.require(t, "lock")
			code, stdout, stderr := invokeNativeRecoveryCommand(t, nativeRecoveryFixture{manifest: fixture.manifest, destination: fixture.settings}, "native-ordinary-"+boundary, "apply", "--yes", "--json")
			if code == 0 || code == nativeRecoveryExit {
				t.Fatalf("expected returned suffix error: %d %s %s", code, stdout, stderr)
			}
			var result clijson.ApplyResult
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatal(err)
			}
			settled := boundary == "return-state-error"
			if len(result.RelationOrderResults) != 1 || result.RelationOrderResults[0].Outcome != "failed" || result.RelationOrderResults[0].Changed != settled {
				t.Fatalf("returned error hid actual order settlement: %s", stdout)
			}
			if settled {
				requireNativePiOrderValues(t, fixture.settings, "", "npm:@acme/keep@1", "git:github.com/acme/pi-tools@v1", "npm:@acme/extra@1")
			} else {
				requireNativePiOrderValues(t, fixture.settings, "fixture-theme", "npm:@acme/keep@1", "npm:@acme/extra@1", "git:github.com/acme/pi-tools@v1")
			}
			fixture.require(t, "apply", "--yes", "--json")
			fixture.require(t, "status", "--check", "--json")
			testkit.AssertFileContent(t, fixture.calls, "install\ninstall\nextra\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}

func TestNativePiOrderCommandSelectsBothScopeMutationVariants(t *testing.T) {
	for _, projectChanged := range []bool{false, true} {
		for _, globalChanged := range []bool{false, true} {
			for _, remove := range []bool{false, true} {
				t.Run(fmt.Sprintf("project=%t/global=%t/remove=%t", projectChanged, globalChanged, remove), func(t *testing.T) {
					root := t.TempDir()
					setNativeRecoveryEnvironment(t, root)
					workspace := filepath.Join(root, "workspace")
					fixture := nativePiInvocationFixture{workspace: workspace, manifest: filepath.Join(workspace, "daem.toml"), settings: filepath.Join(workspace, ".pi", "settings.json"), beforeCommand: filepath.Join(root, "command-before.json"), calls: filepath.Join(root, "calls")}
					testkit.WriteFile(t, workspace, "daem.toml", "version=1\ntargets=['pi']\n")
					t.Setenv("DAEM_TEST_SETTINGS", fixture.settings)
					t.Setenv("DAEM_TEST_COMMAND_BEFORE", fixture.beforeCommand)
					t.Setenv("DAEM_TEST_COMMAND_CALLS", fixture.calls)
					t.Setenv("DAEM_TEST_SCOPE", string(target.ScopeProject))
					writeNativePiOrderCommand(t, fixture)
					globalSettings := filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "settings.json")
					for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
						path := fixture.settings
						if scope == target.ScopeGlobal {
							path = globalSettings
						}
						names := []string{"alpha", "beta"}
						packages := `"npm:@acme/keep@1","npm:@acme/alpha@1","npm:@acme/beta@1"`
						if remove && scope == target.ScopeProject {
							names = append(names, "obsolete")
							packages += `,"npm:@acme/obsolete@1"`
						}
						testkit.WriteFile(t, filepath.Dir(path), "settings.json", fmt.Sprintf(`{"packages":[%s],"foreign":{"retained":true}}`, packages))
						for _, name := range names {
							fixture.require(t, "add", "extension", string(scope)+"-"+name, "npm:@acme/"+name+"@1", "--target", "pi", "--scope", string(scope))
						}
					}
					fixture.require(t, "lock")
					fixture.require(t, "apply", "--manage-existing", "--yes")
					manifest := string(testkit.ReadFile(t, fixture.manifest))
					for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
						manifest += fmt.Sprintf("\n[[host_config]]\nid=%q\nscope=%q\npath='settings.json'\nsource=%q\nownership='keys'\n", "preferences-"+string(scope), scope, filepath.Join(workspace, "preferences.json"))
					}
					testkit.WriteFile(t, workspace, "daem.toml", manifest)
					testkit.WriteFile(t, workspace, "preferences.json", `{"theme":"fixture-theme"}`)
					fixture.require(t, "lock")
					fixture.require(t, "apply", "--yes")
					if globalChanged {
						testkit.WriteFile(t, filepath.Dir(globalSettings), "settings.json", `{"theme":"fixture-theme","packages":["npm:@acme/keep@1","npm:@acme/beta@1","npm:@acme/alpha@1"],"foreign":{"retained":true}}`)
					}
					if remove {
						if projectChanged {
							testkit.WriteFile(t, filepath.Dir(fixture.settings), "settings.json", `{"theme":"fixture-theme","packages":["npm:@acme/keep@1","npm:@acme/beta@1","npm:@acme/alpha@1","npm:@acme/obsolete@1"],"foreign":{"retained":true}}`)
						}
						fixture.require(t, "remove", "extension", "project-obsolete", "--target", "pi", "--scope", "project")
					} else {
						fixture.require(t, "add", "extension", "extra", "npm:@acme/extra@1", "--target", "pi", "--scope", "project")
					}
					fixture.addInstructions(t, "instructions after ordered extensions\n")
					fixture.require(t, "lock")
					postPackages := `"npm:@acme/alpha@1","npm:@acme/beta@1","npm:@acme/extra@1"`
					if projectChanged {
						postPackages = `"npm:@acme/alpha@1","npm:@acme/extra@1","npm:@acme/beta@1"`
					}
					if remove {
						postPackages = `"npm:@acme/alpha@1","npm:@acme/beta@1"`
						if projectChanged {
							postPackages = `"npm:@acme/beta@1","npm:@acme/alpha@1"`
						}
					}
					setNativePiCommandResult(t, fixture, fmt.Sprintf(`{"theme":"fixture-theme","packages":["npm:@acme/keep@1",%s],"foreign":{"retained":true}}`, postPackages))
					code, stdout, stderr := fixture.run("apply", "--yes", "--json")
					if code != 0 {
						t.Fatalf("combined scope variants failed: %d %s %s", code, stdout, stderr)
					}
					var result clijson.ApplyResult
					if err := json.Unmarshal([]byte(stdout), &result); err != nil {
						t.Fatal(err)
					}
					if len(result.RelationOrderResults) != 2 {
						t.Fatalf("lost scope-specific order results: %s", stdout)
					}
					for _, row := range result.RelationOrderResults {
						changed := projectChanged
						if row.Scope == string(target.ScopeGlobal) {
							changed = globalChanged
						}
						outcome := "exact"
						if changed {
							outcome = "converged"
						}
						if row.Changed != changed || row.Outcome != outcome {
							t.Fatalf("order result fabricated or lost actual scope changes: %s", stdout)
						}
					}
					expectedPackages := []string{"npm:@acme/keep@1", "npm:@acme/alpha@1", "npm:@acme/beta@1"}
					if !remove {
						expectedPackages = append(expectedPackages, "npm:@acme/extra@1")
					}
					requireNativePiOrderValues(t, fixture.settings, "fixture-theme", expectedPackages...)
					requireNativePiOrderValues(t, globalSettings, "fixture-theme", "npm:@acme/keep@1", "npm:@acme/alpha@1", "npm:@acme/beta@1")
					fixture.require(t, "status", "--check", "--json")
					fixture.require(t, "apply", "--yes", "--json")
					expectedCall := "extra\n"
					if remove {
						expectedCall = "remove\n"
					}
					testkit.AssertFileContent(t, fixture.calls, expectedCall)
					testkit.AssertNoRecoveryArtifacts(t, workspace)
				})
			}
		}
	}
}

func newNativePiOrderCommandFixture(t *testing.T, scope target.Scope) nativePiInvocationFixture {
	t.Helper()
	fixture := newNativePiInvocationFixtureAtScope(t, "none", scope)
	fixture.require(t, "apply", "--yes")
	fixture.require(t, "add", "extension", "extra", "npm:@acme/extra@1", "--target", "pi", "--scope", string(scope))
	fixture.addInstructions(t, "instructions after ordered extensions\n")
	fixture.require(t, "lock")
	setNativePiCommandResult(t, fixture, `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1","npm:@acme/extra@1","git:github.com/acme/pi-tools@v1"]}`)
	writeNativePiOrderCommand(t, fixture)
	return fixture
}

func writeNativePiOrderCommand(t *testing.T, fixture nativePiInvocationFixture) {
	t.Helper()
	bin := filepath.Join(filepath.Dir(fixture.beforeCommand), "bin")
	testkit.WriteFile(t, bin, "pi", `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '1.0.2\n'; exit 0; fi
case "$1:$2" in
  install:npm:@acme/extra@1) label=extra ;;
  remove:npm:@acme/obsolete@1) label=remove ;;
  *) exit 97 ;;
esac
if [ "$DAEM_TEST_SCOPE" = project ]; then
  [ "$#" -eq 3 ] && [ "$3" = -l ] || exit 97
else
  [ "$#" -eq 2 ] || exit 97
fi
/bin/cp "$DAEM_TEST_SETTINGS" "$DAEM_TEST_COMMAND_BEFORE"
printf '%s\n' "$label" >> "$DAEM_TEST_COMMAND_CALLS"
/bin/cp "$DAEM_TEST_NATIVE_RESULT" "$DAEM_TEST_SETTINGS"
`)
	if err := os.Chmod(filepath.Join(bin, "pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
