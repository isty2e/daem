//go:build darwin || linux

package cli_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func nativeTransitionCommandFixture(t *testing.T, selected target.Target, scope target.Scope, mode string) (nativePiInvocationFixture, nativeDocumentTransitionFixture) {
	t.Helper()
	f := newNativePiInvocationFixtureAtScope(t, "none", scope)
	g := nativeDocumentTransitionFixture{
		workspace: f.workspace, syntax: configdocument.TOML,
		original: "# whole preferences\nfoo = 1\n# released value\nbar = 2\n", retained: "foo=1\n", changed: "foo=3\nnew=4\n", released: "bar=2\n",
	}
	name, root := "prefs.toml", filepath.Join(f.workspace, ".codex")
	if scope == target.ScopeGlobal {
		root = os.Getenv("CODEX_HOME")
	}
	if selected == target.TargetPi {
		name, root, g.syntax = "prefs.jsonc", filepath.Dir(f.settings), configdocument.JSONC
		g.original = "{\n // whole preferences\n \"foo\":1,\n // released value\n \"bar\":2,\n}\n"
		g.retained, g.changed, g.released = `{"foo":1}`, `{"foo":3,"new":4}`, `{"bar":2}`
	}
	g.source = filepath.Join(f.workspace, "transition"+filepath.Ext(name))
	paths, err := daempaths.Resolve(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	g.nativeRecoveryFixture = nativeRecoveryFixture{manifest: f.manifest, destination: filepath.Join(root, name), paths: paths, kind: "transition-command"}
	g.declaration = fmt.Sprintf("version=1\ntargets=['pi','codex']\n[[extension]]\nid='tools'\ntargets=['pi']\ncarrier='pi-package'\nscope=%q\nsource={host_source='git:github.com/acme/pi-tools@v1'}\n[[host_config]]\nid='prefs'\ntargets=[%q]\nscope=%q\npath=%q\nsource=%q\nownership=", scope, selected, scope, name, g.source)
	testkit.WriteFile(t, filepath.Dir(f.settings), "settings.json", `{"foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`)
	adoption := strings.HasPrefix(mode, "adopt")
	if adoption {
		g.writeMode(t, "keys")
		testkit.WriteFile(t, f.workspace, filepath.Base(g.source), g.retained)
		testkit.WriteFile(t, filepath.Dir(g.destination), name, g.original)
		if err := os.Chmod(g.destination, 0o600); err != nil {
			t.Fatal(err)
		}
	} else {
		g.writeMode(t, "document")
		testkit.WriteFile(t, f.workspace, filepath.Base(g.source), g.original)
	}
	f.require(t, "lock")
	f.require(t, "apply", "--yes", "--manage-existing")
	testkit.AssertFileContent(t, f.calls, "install\n")
	testkit.AssertFileContent(t, g.destination, g.original)
	if adoption {
		g.writeMode(t, "document")
		content := string(testkit.ReadFile(t, g.destination))
		if mode == "adopt-mismatch" {
			comment := "# unmatched whole source\n"
			if selected == target.TargetPi {
				comment = "// unmatched whole source\n"
			}
			content += comment
		}
		testkit.WriteFile(t, f.workspace, filepath.Base(g.source), content)
	} else {
		fragment := g.changed
		if mode == "narrow-current" {
			fragment = g.retained
		} else if mode == "narrow-empty" {
			fragment = ""
			if selected == target.TargetPi {
				fragment = "{}"
			}
		}
		g.narrow(t, fragment)
	}
	testkit.WriteFile(t, filepath.Dir(f.settings), "settings.json", `{"foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
	launch := filepath.Join(f.workspace, "transition-at-command")
	t.Setenv("DAEM_TEST_TRANSITION_DOCUMENT", g.destination)
	t.Setenv("DAEM_TEST_TRANSITION_LAUNCH", launch)
	scriptPath, err := exec.LookPath("pi")
	if err != nil {
		t.Fatal(err)
	}
	script := string(testkit.ReadFile(t, scriptPath))
	marker := `printf 'install\n' >> "$DAEM_TEST_COMMAND_CALLS"`
	if strings.Count(script, marker) != 1 {
		t.Fatal("Pi fixture has no unique invocation observation")
	}
	script = strings.Replace(script, marker, "/bin/cp \"$DAEM_TEST_TRANSITION_DOCUMENT\" \"$DAEM_TEST_TRANSITION_LAUNCH\"\n"+marker, 1)
	testkit.WriteFile(t, filepath.Dir(scriptPath), filepath.Base(scriptPath), script)
	if err := os.Chmod(scriptPath, 0o700); err != nil {
		t.Fatal(err)
	}
	f.require(t, "lock")
	return f, g
}

func TestNativeOwnershipTransitionWithPiCommand(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, mode := range []string{"narrow", "narrow-current", "narrow-empty", "adopt", "adopt-no-consent", "adopt-mismatch"} {
				t.Run(string(selected)+"/"+string(scope)+"/"+mode, func(t *testing.T) {
					f, g := nativeTransitionCommandFixture(t, selected, scope, mode)
					before := snapshotNativeRecoveryEvidence(t, g.nativeRecoveryFixture)
					args := []string{"apply", "--yes"}
					if mode != "adopt-no-consent" {
						args = append(args, "--manage-existing")
					}
					code, stdout, stderr := f.run(args...)
					if mode == "adopt-no-consent" || mode == "adopt-mismatch" {
						if code == 0 || !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, g.nativeRecoveryFixture)) {
							t.Fatal("unqualified whole adoption had effects")
						}
						testkit.AssertFileContent(t, f.calls, "install\n")
						return
					}
					if code != 0 {
						t.Fatalf("ownership transition with real command failed: %d %s %s", code, stdout, stderr)
					}
					launched, err := statefile.Decode(testkit.ReadFile(t, f.launchState))
					if err != nil {
						t.Fatal(err)
					}
					atCommand := testkit.ReadFile(t, filepath.Join(f.workspace, "transition-at-command"))
					if mode == "adopt" || mode == "narrow-empty" {
						if string(atCommand) != g.original {
							t.Fatal("final ownership work changed content before the command")
						}
						paths, keys := 0, 1
						if mode == "narrow-empty" {
							paths, keys = 1, 0
							if !strings.Contains(string(testkit.ReadFile(t, g.destination)), "released value") {
								t.Fatal("empty narrowing lost released comments")
							}
						}
						if len(launched.ManagedPaths()) != paths || len(launched.ManagedAggregates()) != keys {
							t.Fatal("final ownership settled before its actual suffix")
						}
					} else {
						fragment, keys := g.changed, 2
						if mode == "narrow-current" {
							fragment, keys = g.retained, 1
							if string(atCommand) != g.original {
								t.Fatal("state-only narrowing rewrote current bytes")
							}
						}
						requireNativeSharedValues(t, g.syntax, atCommand, fragment, true)
						if len(launched.ManagedPaths()) != 0 || len(launched.ManagedAggregates()) != keys {
							t.Fatal("positive narrowing was not fully committed before the command")
						}
					}
					requireNativeSharedValues(t, g.syntax, testkit.ReadFile(t, g.destination), g.released, true)
					if info, err := os.Stat(g.destination); err != nil || info.Mode().Perm() != 0o600 {
						t.Fatal("transition changed private permissions", err)
					}
					final, err := statefile.Decode(testkit.ReadFile(t, g.paths.StatefilePath))
					paths, keys := 0, 2
					if mode == "adopt" {
						paths, keys = 1, 0
					} else if mode == "narrow-current" {
						keys = 1
					} else if mode == "narrow-empty" {
						keys = 0
					}
					if err != nil || len(final.ManagedPaths()) != paths || len(final.ManagedAggregates()) != keys {
						t.Fatal("transition did not publish its actual final ownership", err)
					}
					f.require(t, "status", "--check")
					f.require(t, "apply", "--yes")
					testkit.AssertFileContent(t, f.calls, "install\ninstall\n")
					testkit.AssertNoRecoveryArtifacts(t, f.workspace)
				})
			}
		}
	}
}

func TestNativePositiveNarrowingRecoveryBeforePiCommand(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, boundary := range []string{"host", "state"} {
				t.Run(string(selected)+"/"+string(scope)+"/"+boundary, func(t *testing.T) {
					f, g := nativeTransitionCommandFixture(t, selected, scope, "narrow")
					code, stdout, stderr := invokeNativeRecoveryCommand(t, g.nativeRecoveryFixture, "native-ordinary-"+boundary, "apply", "--yes")
					if code != nativeRecoveryExit {
						t.Fatalf("narrowing prefix did not reach %s: %d %s %s", boundary, code, stdout, stderr)
					}
					testkit.AssertFileContent(t, f.calls, "install\n")
					requireNativeSharedValues(t, g.syntax, testkit.ReadFile(t, g.destination), g.changed, true)
					classification, settled := "needs_rollback", "clean_before"
					if boundary == "state" {
						classification, settled = "needs_finalize", "clean_after"
					}
					preview := requireNativeRecoveryDryRun(t, g.nativeRecoveryFixture, classification, settled)
					if preview.AuthorityKind != "active_journal" {
						t.Fatal("narrowing interruption did not preserve its actual journal")
					}
					f.require(t, "recover", "--yes")
					if boundary == "host" {
						testkit.AssertFileContent(t, g.destination, g.original)
					} else {
						requireNativeSharedValues(t, g.syntax, testkit.ReadFile(t, g.destination), g.changed, true)
					}
					f.require(t, "apply", "--yes")
					f.require(t, "status", "--check")
					testkit.AssertFileContent(t, f.calls, "install\ninstall\n")
					testkit.AssertNoRecoveryArtifacts(t, f.workspace)
				})
			}
		}
	}
}

func TestNativeWholeAdoptionRecoveryAfterPiCommand(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, boundary := range []string{"before-state", "state"} {
				t.Run(string(selected)+"/"+string(scope)+"/"+boundary, func(t *testing.T) {
					f, g := nativeTransitionCommandFixture(t, selected, scope, "adopt")
					prior, err := statefile.Decode(testkit.ReadFile(t, g.paths.StatefilePath))
					if err != nil || len(prior.ManagedPaths()) != 0 || len(prior.ManagedAggregates()) != 1 {
						t.Fatal("adoption interruption fixture has no unique real key-to-whole transition", err)
					}
					code, stdout, stderr := invokeNativeRecoveryCommand(t, g.nativeRecoveryFixture, "native-adoption-"+boundary, "apply", "--yes", "--manage-existing")
					if code != nativeRecoveryExit {
						t.Fatalf("state-only adoption did not reach %s: %d %s %s", boundary, code, stdout, stderr)
					}
					testkit.AssertFileContent(t, f.calls, "install\ninstall\n")
					testkit.AssertFileContent(t, g.destination, g.original)
					classification, settled := "needs_rollback", "clean_before"
					if boundary == "state" {
						classification, settled = "needs_finalize", "clean_after"
					}
					preview := requireNativeRecoveryDryRun(t, g.nativeRecoveryFixture, classification, settled)
					if preview.AuthorityKind != "active_journal" {
						t.Fatal("state-only adoption did not preserve its actual journal")
					}
					f.require(t, "recover", "--yes")
					state, err := statefile.Decode(testkit.ReadFile(t, g.paths.StatefilePath))
					paths, keys := 0, 1
					if boundary == "state" {
						paths, keys = 1, 0
					}
					if err != nil || len(state.ManagedPaths()) != paths || len(state.ManagedAggregates()) != keys {
						t.Fatal("adoption recovery lost its actual before/after ownership", err)
					}
					f.require(t, "apply", "--yes", "--manage-existing")
					f.require(t, "status", "--check")
					testkit.AssertFileContent(t, g.destination, g.original)
					testkit.AssertFileContent(t, f.calls, "install\ninstall\n")
					testkit.AssertNoRecoveryArtifacts(t, f.workspace)
				})
			}
		}
	}
}

func TestNativeNarrowingCommitSurvivesLaterCommandFailure(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		t.Run(string(selected), func(t *testing.T) {
			f, g := nativeTransitionCommandFixture(t, selected, target.ScopeProject, "narrow")
			t.Setenv("DAEM_TEST_MUTATION", "command")
			if code, _, _ := f.run("apply", "--yes"); code == 0 {
				t.Fatal("failed command was reported as successful")
			}
			state, err := statefile.Decode(testkit.ReadFile(t, g.paths.StatefilePath))
			if err != nil || len(state.ManagedPaths()) != 0 || len(state.ManagedAggregates()) != 2 {
				t.Fatal("later command failure undid committed narrowing ownership", err)
			}
			requireNativeSharedValues(t, g.syntax, testkit.ReadFile(t, g.destination), g.changed, true)
			requireNativeSharedValues(t, g.syntax, testkit.ReadFile(t, g.destination), g.released, true)
			t.Setenv("DAEM_TEST_MUTATION", "none")
			f.require(t, "apply", "--yes")
			f.require(t, "status", "--check")
			testkit.AssertFileContent(t, f.calls, "install\ninstall\n")
			testkit.AssertNoRecoveryArtifacts(t, f.workspace)
		})
	}
}

func TestNativeNarrowingDoesNotAdoptCommandChanges(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, mutation := range []string{"key", "released"} {
			t.Run(string(selected)+"/"+mutation, func(t *testing.T) {
				f, g := nativeTransitionCommandFixture(t, selected, target.ScopeProject, "narrow")
				changed := "foo=90\nnew=4\nbar=2\n"
				if mutation == "released" {
					changed = "foo=3\nnew=4\nbar=90\n"
				}
				if selected == target.TargetPi {
					changed = `{"foo":90,"new":4,"bar":2}`
					if mutation == "released" {
						changed = `{"foo":3,"new":4,"bar":90}`
					}
				}
				result := filepath.Join(f.workspace, "changed-by-command")
				testkit.WriteFile(t, f.workspace, "changed-by-command", changed)
				t.Setenv("DAEM_TEST_TRANSITION_RESULT", result)
				scriptPath, err := exec.LookPath("pi")
				if err != nil {
					t.Fatal(err)
				}
				marker := `printf 'install\n' >> "$DAEM_TEST_COMMAND_CALLS"`
				script := string(testkit.ReadFile(t, scriptPath))
				if strings.Count(script, marker) != 1 {
					t.Fatal("transition fixture has no unique invocation observation")
				}
				script = strings.Replace(script, marker, marker+"\n/bin/cp \"$DAEM_TEST_TRANSITION_RESULT\" \"$DAEM_TEST_TRANSITION_DOCUMENT\"", 1)
				testkit.WriteFile(t, filepath.Dir(scriptPath), filepath.Base(scriptPath), script)
				if err := os.Chmod(scriptPath, 0o700); err != nil {
					t.Fatal(err)
				}
				if code, _, _ := f.run("apply", "--yes"); code == 0 {
					t.Fatal("command changes were adopted during ownership transition")
				}
				testkit.AssertFileContent(t, g.destination, changed)
				testkit.AssertFileContent(t, f.calls, "install\ninstall\n")
				state, err := statefile.Decode(testkit.ReadFile(t, g.paths.StatefilePath))
				if err != nil || len(state.ManagedPaths()) != 0 || len(state.ManagedAggregates()) != 2 {
					t.Fatal("invocation conflict lost committed key ownership", err)
				}
			})
		}
	}
}

func TestNativePlannedNarrowingIsNotRefreshSettlement(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, mode := range []string{"narrow-current", "narrow-empty"} {
			t.Run(string(selected)+"/"+mode, func(t *testing.T) {
				f, g := nativeTransitionCommandFixture(t, selected, target.ScopeProject, mode)
				testkit.WriteFile(t, filepath.Dir(f.settings), "settings.json", `{"foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}`)
				before := snapshotNativeRecoveryEvidence(t, g.nativeRecoveryFixture)
				code, _, _ := f.run("refresh", "extension", "tools", "--yes")
				if code == 0 || !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, g.nativeRecoveryFixture)) {
					t.Fatal("Refresh treated planned or empty narrowing as settled ownership")
				}
				testkit.AssertFileContent(t, g.destination, g.original)
				testkit.AssertFileContent(t, f.calls, "install\n")
			})
		}
	}
}
