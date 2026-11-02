//go:build darwin || linux

package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestNativeWholeDocumentPublishesAfterPiInstall(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, withKeys := range []bool{false, true} {
			for _, mode := range []string{"create", "record", "current", "update", "remove", "move", "move-current-relation", "tampered"} {
				t.Run(fmt.Sprintf("%s/keys=%t/%s", scope, withKeys, mode), func(t *testing.T) {
					invokesCommand := mode != "move-current-relation"
					f := newNativePiInvocationFixtureAtScope(t, "none", scope)
					original := string(testkit.ReadFile(t, f.manifest))
					base, keys, found := strings.Cut(original, "\n[[host_config]]")
					if !found {
						t.Fatal("native invocation fixture has no key declaration")
					}
					if withKeys {
						base += "\n[[host_config]]" + keys
					}
					declaration := base + fmt.Sprintf("\n[[host_config]]\nid='whole'\nscope=%q\npath='dedicated.json'\nsource=%q\nownership='document'\n", scope, filepath.Join(f.workspace, "whole.json"))
					testkit.WriteFile(t, f.workspace, "daem.toml", declaration)
					desired := `{"mode":"original","released":2}`
					testkit.WriteFile(t, f.workspace, "whole.json", desired)

					setPackages := func(installed bool) {
						var settings map[string]any
						if err := json.Unmarshal(testkit.ReadFile(t, f.settings), &settings); err != nil {
							t.Fatal(err)
						}
						packages := []string{"npm:@acme/keep@1"}
						if installed {
							packages = append(packages, "git:github.com/acme/pi-tools@v1")
						}
						settings["packages"] = packages
						content, err := json.Marshal(settings)
						if err != nil {
							t.Fatal(err)
						}
						testkit.WriteFile(t, filepath.Dir(f.settings), filepath.Base(f.settings), string(content))
					}
					if mode != "create" && mode != "record" {
						setPackages(true)
						f.require(t, "lock")
						f.require(t, "apply", "--yes")
						if invokesCommand {
							setPackages(false)
						}
					}
					if mode == "update" {
						desired = `{"mode":"updated","released":3}`
						testkit.WriteFile(t, f.workspace, "whole.json", desired)
					}
					if mode == "remove" {
						testkit.WriteFile(t, f.workspace, "daem.toml", base)
					} else if strings.HasPrefix(mode, "move") {
						testkit.WriteFile(t, f.workspace, "daem.toml", strings.Replace(declaration, "path='dedicated.json'", "path='moved.json'", 1))
					}

					document := filepath.Join(filepath.Dir(f.settings), "dedicated.json")
					if mode == "record" {
						testkit.WriteFile(t, filepath.Dir(document), filepath.Base(document), desired)
						if err := os.Chmod(document, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					launchDocument := filepath.Join(f.workspace, "whole-at-command.json")
					t.Setenv("DAEM_TEST_WHOLE_DOCUMENT", document)
					t.Setenv("DAEM_TEST_WHOLE_LAUNCH", launchDocument)
					scriptPath := filepath.Join(filepath.Dir(f.workspace), "bin", "pi")
					script := string(testkit.ReadFile(t, scriptPath))
					marker := `printf 'install\n' >> "$DAEM_TEST_COMMAND_CALLS"`
					observer := "if [ -f \"$DAEM_TEST_WHOLE_DOCUMENT\" ]; then /bin/cp \"$DAEM_TEST_WHOLE_DOCUMENT\" \"$DAEM_TEST_WHOLE_LAUNCH\"; else printf absent > \"$DAEM_TEST_WHOLE_LAUNCH\"; fi\n"
					if mode == "tampered" {
						observer += "printf tampered > \"$DAEM_TEST_WHOLE_DOCUMENT\"\n"
					}
					if strings.Count(script, marker) != 1 {
						t.Fatal("native invocation fixture has no unique install boundary")
					}
					script = strings.Replace(script, marker, observer+marker, 1)
					registryCopy := `/bin/cp "$DAEM_TEST_REGISTRY" "$DAEM_TEST_LAUNCH_REGISTRY"`
					if strings.Count(script, registryCopy) != 1 {
						t.Fatal("native invocation fixture has no unique Registry observation")
					}
					script = strings.Replace(script, registryCopy, `if [ -f "$DAEM_TEST_REGISTRY" ]; then `+registryCopy+`; else printf absent > "$DAEM_TEST_LAUNCH_REGISTRY"; fi`, 1)
					testkit.WriteFile(t, filepath.Dir(scriptPath), filepath.Base(scriptPath), script)
					if err := os.Chmod(scriptPath, 0o755); err != nil {
						t.Fatal(err)
					}

					f.require(t, "lock")
					if mode == "tampered" {
						code, _, _ := f.run("apply", "--yes")
						if code == 0 {
							t.Fatal("host mutation of a whole file was accepted")
						}
						testkit.AssertFileContent(t, document, "tampered")
						testkit.AssertFileContent(t, f.calls, "install\ninstall\n")
						return
					}
					apply := []string{"apply", "--yes"}
					if mode == "record" {
						apply = append(apply, "--manage-existing")
					}
					f.require(t, apply...)
					beforeCommand := `{"mode":"original","released":2}`
					expectedPaths := 1
					if mode == "create" {
						beforeCommand, expectedPaths = "absent", 0
					} else if mode == "record" {
						expectedPaths = 0
					}
					if invokesCommand {
						testkit.AssertFileContent(t, launchDocument, beforeCommand)
						snapshot, err := statefile.Decode(testkit.ReadFile(t, f.launchState))
						if err != nil || len(snapshot.ManagedPaths()) != expectedPaths {
							t.Fatal("whole-file ownership advanced before the host command", err)
						}
					}
					if mode == "remove" {
						if _, err := os.Stat(document); !os.IsNotExist(err) {
							t.Fatal("whole-file retirement did not run after the host command", err)
						}
					} else {
						if strings.HasPrefix(mode, "move") {
							testkit.AssertPathMissing(t, document)
							document = filepath.Join(filepath.Dir(document), "moved.json")
						}
						testkit.AssertFileContent(t, document, desired)
						info, err := os.Stat(document)
						if err != nil || info.Mode().Perm() != 0o600 {
							t.Fatal("whole-file publication lost its private mode", err)
						}
					}
					f.require(t, "apply", "--yes")
					expectedCalls := "install\n"
					if invokesCommand {
						expectedCalls += "install\n"
					}
					testkit.AssertFileContent(t, f.calls, expectedCalls)
					f.require(t, "status", "--check")
				})
			}
		}
	}
}

func TestNativeWholeDocumentRefusesPhysicalHostRewriteAlias(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			f := newNativePiInvocationFixtureAtScope(t, "none", scope)
			base, _, found := strings.Cut(string(testkit.ReadFile(t, f.manifest)), "\n[[host_config]]")
			if !found {
				t.Fatal("native invocation fixture has no key declaration")
			}
			document := filepath.Join(filepath.Dir(f.settings), "dedicated.json")
			if err := os.Symlink("settings.json", document); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(f.settings, 0o600); err != nil {
				t.Fatal(err)
			}
			testkit.WriteFile(t, f.workspace, "whole.json", string(testkit.ReadFile(t, f.settings)))
			declaration := base + fmt.Sprintf("\n[[host_config]]\nid='whole'\nscope=%q\npath='dedicated.json'\nsource=%q\nownership='document'\n", scope, filepath.Join(f.workspace, "whole.json"))
			testkit.WriteFile(t, f.workspace, "daem.toml", declaration)
			f.require(t, "lock")
			before := string(testkit.ReadFile(t, f.settings))
			state := testkit.ReadFile(t, filepath.Join(f.workspace, ".daem", "state.json"))
			code, _, _ := f.run("apply", "--yes", "--manage-existing")
			if code == 0 || string(testkit.ReadFile(t, filepath.Join(f.workspace, ".daem", "state.json"))) != string(state) {
				t.Fatal("whole ownership overlapping a command rewrite was accepted or changed state")
			}
			testkit.AssertFileContent(t, f.settings, before)
			testkit.AssertFileContent(t, f.calls, "install\n")
		})
	}
}

func TestNativeWholeDocumentSuffixRecoveryDoesNotRepeatPiInstall(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, boundary := range []string{"host", "state"} {
			for _, mode := range []string{"create", "move"} {
				t.Run(string(scope)+"/"+boundary+"/"+mode, func(t *testing.T) {
					f := newNativePiInvocationFixtureAtScope(t, "none", scope)
					desired := `{"dedicated":true}`
					testkit.WriteFile(t, f.workspace, "whole.json", desired)
					declaration := string(testkit.ReadFile(t, f.manifest)) + fmt.Sprintf("\n[[host_config]]\nid='whole'\nscope=%q\npath='dedicated.json'\nsource=%q\nownership='document'\n", scope, filepath.Join(f.workspace, "whole.json"))
					testkit.WriteFile(t, f.workspace, "daem.toml", declaration)
					f.require(t, "lock")
					originalDocument := filepath.Join(filepath.Dir(f.settings), "dedicated.json")
					document := originalDocument
					if mode == "move" {
						testkit.WriteFile(t, filepath.Dir(f.settings), "settings.json", `{"foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`)
						f.require(t, "apply", "--yes")
						testkit.WriteFile(t, filepath.Dir(f.settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
						testkit.WriteFile(t, f.workspace, "daem.toml", strings.Replace(declaration, "path='dedicated.json'", "path='moved.json'", 1))
						f.require(t, "lock")
						document = filepath.Join(filepath.Dir(f.settings), "moved.json")
					}
					paths, err := daempaths.Resolve(f.manifest)
					if err != nil {
						t.Fatal(err)
					}
					fixture := nativeRecoveryFixture{manifest: f.manifest, destination: document, paths: paths, kind: "whole"}
					code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, "native-ordinary-"+boundary, "apply", "--yes")
					if code != nativeRecoveryExit {
						t.Fatalf("whole suffix did not reach %s: %d %s %s", boundary, code, stdout, stderr)
					}
					testkit.AssertFileContent(t, f.calls, "install\ninstall\n")
					testkit.AssertFileContent(t, document, desired)
					before := snapshotNativeRecoveryEvidence(t, fixture)
					f.require(t, "recover", "--dry-run")
					if !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, fixture)) {
						t.Fatal("whole suffix recovery preview changed evidence")
					}
					f.require(t, "recover", "--yes")
					if boundary == "host" {
						testkit.AssertPathMissing(t, document)
						if mode == "move" {
							testkit.AssertFileContent(t, originalDocument, desired)
						}
					} else {
						testkit.AssertFileContent(t, document, desired)
						if mode == "move" {
							testkit.AssertPathMissing(t, originalDocument)
						}
					}
					var settings map[string]json.RawMessage
					if err := json.Unmarshal(testkit.ReadFile(t, f.settings), &settings); err != nil || string(settings["theme"]) != `"fixture-theme"` {
						t.Fatal("suffix recovery lost committed native preconfiguration", err)
					}
					f.require(t, "apply", "--yes")
					testkit.AssertFileContent(t, document, desired)
					if mode == "move" {
						testkit.AssertPathMissing(t, originalDocument)
					}
					testkit.AssertFileContent(t, f.calls, "install\ninstall\n")
					f.require(t, "status", "--check")
					testkit.AssertNoRecoveryArtifacts(t, f.workspace)
				})
			}
		}
	}
}
