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
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativePiOrderAfterExtensionCommand(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, update := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/update=%t", scope, update), func(t *testing.T) {
				fixture := newNativePiOrderCommandFixture(t, scope)
				theme := "fixture-theme"
				if update {
					theme = "next-theme"
					testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{"theme":"next-theme"}`)
				}
				fixture.Require(t, "lock")
				setNativePiCommandResult(t, fixture, fmt.Sprintf(`{"theme":%q,"foreign":{"retained":true},"packages":["npm:@acme/keep@1","npm:@acme/extra@1","git:github.com/acme/pi-tools@v1"]}`, theme))
				fixture.Require(t, "apply", "--yes", "--json")
				nativefixture.RequirePiOrderValues(t, fixture.Settings, theme, "npm:@acme/keep@1", "git:github.com/acme/pi-tools@v1", "npm:@acme/extra@1")
				var before struct {
					Theme string `json:"theme"`
				}
				if err := json.Unmarshal(testkit.ReadFile(t, fixture.BeforeCommand), &before); err != nil || before.Theme != theme {
					t.Fatal("command did not consume the settled selected key", err)
				}
				fixture.Require(t, "status", "--check", "--json")
				fixture.Require(t, "apply", "--yes", "--json")
				testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nextra\n")
				testkit.AssertFileContent(t, filepath.Join(fixture.Workspace, "AGENTS.md"), "instructions after ordered extensions\n")
				testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
			})
		}
	}
}

func TestNativePiOrderCommandSuffixRecoveryUsesPostCommandBefore(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, boundary := range []string{"host", "state"} {
			t.Run(string(scope)+"/"+boundary, func(t *testing.T) {
				fixture := newNativePiOrderCommandFixture(t, scope)
				testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{}`)
				fixture.Require(t, "lock")
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, nativefixture.RecoveryFixture{Manifest: fixture.Manifest, Destination: fixture.Settings}, "native-ordinary-"+boundary, "apply", "--yes")
				if code != nativefixture.RecoveryExit {
					t.Fatalf("combined order/key/file suffix missed its interruption: %d %s %s", code, stdout, stderr)
				}
				fixture.Require(t, "recover", "--dry-run")
				fixture.Require(t, "recover", "--yes")
				if boundary == "host" {
					nativefixture.RequirePiOrderValues(t, fixture.Settings, "fixture-theme", "npm:@acme/keep@1", "npm:@acme/extra@1", "git:github.com/acme/pi-tools@v1")
				} else {
					nativefixture.RequirePiOrderValues(t, fixture.Settings, "", "npm:@acme/keep@1", "git:github.com/acme/pi-tools@v1", "npm:@acme/extra@1")
				}
				fixture.Require(t, "apply", "--yes", "--json")
				nativefixture.RequirePiOrderValues(t, fixture.Settings, "", "npm:@acme/keep@1", "git:github.com/acme/pi-tools@v1", "npm:@acme/extra@1")
				var document map[string]json.RawMessage
				if err := json.Unmarshal(testkit.ReadFile(t, fixture.Settings), &document); err != nil {
					t.Fatal(err)
				}
				if _, retained := document["theme"]; retained {
					t.Fatal("key retirement did not share the actual suffix")
				}
				fixture.Require(t, "status", "--check", "--json")
				testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nextra\n")
				testkit.AssertFileContent(t, filepath.Join(fixture.Workspace, "AGENTS.md"), "instructions after ordered extensions\n")
				testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
			})
		}
	}
}

func TestNativePiOrderCommandSuffixReturnedErrorsReportActualSettlement(t *testing.T) {
	for _, boundary := range []string{"return-error", "return-state-error"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newNativePiOrderCommandFixture(t, target.ScopeProject)
			testkit.WriteFile(t, fixture.Workspace, "preferences.json", `{}`)
			fixture.Require(t, "lock")
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, nativefixture.RecoveryFixture{Manifest: fixture.Manifest, Destination: fixture.Settings}, "native-ordinary-"+boundary, "apply", "--yes", "--json")
			if code == 0 || code == nativefixture.RecoveryExit {
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
				nativefixture.RequirePiOrderValues(t, fixture.Settings, "", "npm:@acme/keep@1", "git:github.com/acme/pi-tools@v1", "npm:@acme/extra@1")
			} else {
				nativefixture.RequirePiOrderValues(t, fixture.Settings, "fixture-theme", "npm:@acme/keep@1", "npm:@acme/extra@1", "git:github.com/acme/pi-tools@v1")
			}
			fixture.Require(t, "apply", "--yes", "--json")
			fixture.Require(t, "status", "--check", "--json")
			testkit.AssertFileContent(t, fixture.Calls, "install\ninstall\nextra\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiOrderCommandSelectsBothScopeMutationVariants(t *testing.T) {
	for _, projectChanged := range []bool{false, true} {
		for _, globalChanged := range []bool{false, true} {
			for _, remove := range []bool{false, true} {
				t.Run(fmt.Sprintf("project=%t/global=%t/remove=%t", projectChanged, globalChanged, remove), func(t *testing.T) {
					root := t.TempDir()
					nativefixture.SetRecoveryEnvironment(t, root)
					workspace := filepath.Join(root, "workspace")
					fixture := nativefixture.PiInvocation{Workspace: workspace, Manifest: filepath.Join(workspace, "daem.toml"), Settings: filepath.Join(workspace, ".pi", "settings.json"), BeforeCommand: filepath.Join(root, "command-before.json"), Calls: filepath.Join(root, "calls")}
					testkit.WriteFile(t, workspace, "daem.toml", "version=1\ntargets=['pi']\n")
					t.Setenv("DAEM_TEST_SETTINGS", fixture.Settings)
					t.Setenv("DAEM_TEST_COMMAND_BEFORE", fixture.BeforeCommand)
					t.Setenv("DAEM_TEST_COMMAND_CALLS", fixture.Calls)
					t.Setenv("DAEM_TEST_SCOPE", string(target.ScopeProject))
					writeNativePiOrderCommand(t, fixture)
					globalSettings := filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "settings.json")
					for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
						path := fixture.Settings
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
							fixture.Require(t, "add", "extension", string(scope)+"-"+name, "npm:@acme/"+name+"@1", "--target", "pi", "--scope", string(scope))
						}
					}
					fixture.Require(t, "lock")
					fixture.Require(t, "apply", "--manage-existing", "--yes")
					manifest := string(testkit.ReadFile(t, fixture.Manifest))
					for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
						manifest += fmt.Sprintf("\n[[host_config]]\nid=%q\nscope=%q\npath='settings.json'\nsource=%q\nownership='keys'\n", "preferences-"+string(scope), scope, filepath.Join(workspace, "preferences.json"))
					}
					testkit.WriteFile(t, workspace, "daem.toml", manifest)
					testkit.WriteFile(t, workspace, "preferences.json", `{"theme":"fixture-theme"}`)
					fixture.Require(t, "lock")
					fixture.Require(t, "apply", "--yes")
					if globalChanged {
						testkit.WriteFile(t, filepath.Dir(globalSettings), "settings.json", `{"theme":"fixture-theme","packages":["npm:@acme/keep@1","npm:@acme/beta@1","npm:@acme/alpha@1"],"foreign":{"retained":true}}`)
					}
					if remove {
						if projectChanged {
							testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", `{"theme":"fixture-theme","packages":["npm:@acme/keep@1","npm:@acme/beta@1","npm:@acme/alpha@1","npm:@acme/obsolete@1"],"foreign":{"retained":true}}`)
						}
						fixture.Require(t, "remove", "extension", "project-obsolete", "--target", "pi", "--scope", "project")
					} else {
						fixture.Require(t, "add", "extension", "extra", "npm:@acme/extra@1", "--target", "pi", "--scope", "project")
					}
					fixture.AddInstructions(t, "instructions after ordered extensions\n")
					fixture.Require(t, "lock")
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
					code, stdout, stderr := fixture.Run("apply", "--yes", "--json")
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
					nativefixture.RequirePiOrderValues(t, fixture.Settings, "fixture-theme", expectedPackages...)
					nativefixture.RequirePiOrderValues(t, globalSettings, "fixture-theme", "npm:@acme/keep@1", "npm:@acme/alpha@1", "npm:@acme/beta@1")
					fixture.Require(t, "status", "--check", "--json")
					fixture.Require(t, "apply", "--yes", "--json")
					expectedCall := "extra\n"
					if remove {
						expectedCall = "remove\n"
					}
					testkit.AssertFileContent(t, fixture.Calls, expectedCall)
					testkit.AssertNoRecoveryArtifacts(t, workspace)
				})
			}
		}
	}
}

func newNativePiOrderCommandFixture(t *testing.T, scope target.Scope) nativefixture.PiInvocation {
	t.Helper()
	fixture := nativefixture.NewPiInvocationAtScope(t, "none", scope)
	fixture.Require(t, "apply", "--yes")
	fixture.Require(t, "add", "extension", "extra", "npm:@acme/extra@1", "--target", "pi", "--scope", string(scope))
	fixture.AddInstructions(t, "instructions after ordered extensions\n")
	fixture.Require(t, "lock")
	setNativePiCommandResult(t, fixture, `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1","npm:@acme/extra@1","git:github.com/acme/pi-tools@v1"]}`)
	writeNativePiOrderCommand(t, fixture)
	return fixture
}

func writeNativePiOrderCommand(t *testing.T, fixture nativefixture.PiInvocation) {
	t.Helper()
	bin := filepath.Join(filepath.Dir(fixture.BeforeCommand), "bin")
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
