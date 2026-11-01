//go:build darwin || linux

package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

type nativeDocumentTransitionFixture struct {
	nativeRecoveryFixture
	workspace, source, declaration, original, retained, changed, released string
	syntax                                                                configdocument.Syntax
}

func newNativeDocumentTransitionFixture(t *testing.T, selected target.Target, scope target.Scope) nativeDocumentTransitionFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	setNativeRecoveryEnvironment(t, root)
	f := nativeDocumentTransitionFixture{workspace: filepath.Join(root, "workspace"), syntax: configdocument.TOML, original: "# original whole file\nfoo = 1\n# released value\nbar = 2", retained: "foo=1\n", changed: "foo=3\nnew=4\n", released: "bar=2\n"}
	name := "prefs.toml"
	hostRoot := filepath.Join(f.workspace, ".codex")
	if scope == target.ScopeGlobal {
		hostRoot = os.Getenv("CODEX_HOME")
	}
	if selected == target.TargetPi {
		name = "prefs.jsonc"
		hostRoot = filepath.Join(f.workspace, ".pi")
		if scope == target.ScopeGlobal {
			hostRoot = os.Getenv("PI_CODING_AGENT_DIR")
		}
		f.syntax = configdocument.JSONC
		f.original = "{\n  // original whole file\n  \"foo\": 1,\n  // released value\n  \"bar\": 2,\n}\n"
		f.retained, f.changed, f.released = `{"foo":1}`, `{"foo":3,"new":4}`, `{"bar":2}`
	}
	f.manifest = filepath.Join(f.workspace, "daem.toml")
	f.destination = filepath.Join(hostRoot, name)
	f.source = filepath.Join(f.workspace, "source"+filepath.Ext(name))
	f.paths, err = daempaths.Resolve(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	f.declaration = fmt.Sprintf("version=1\ntargets=[%q]\n[[host_config]]\nid='prefs'\ntargets=[%q]\nscope=%q\npath=%q\nsource=%q\nownership=", selected, selected, scope, name, f.source)
	testkit.WriteFile(t, f.workspace, filepath.Base(f.source), f.original)
	f.writeMode(t, "document")
	runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
	return f
}

func (f nativeDocumentTransitionFixture) writeMode(t *testing.T, mode string) {
	t.Helper()
	testkit.WriteFile(t, f.workspace, "daem.toml", f.declaration+fmt.Sprintf("%q\n", mode))
}

func (f nativeDocumentTransitionFixture) applyWhole(t *testing.T) {
	t.Helper()
	runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes", "--json")
	testkit.AssertFileContent(t, f.destination, f.original)
	snapshot, err := statefile.Decode(testkit.ReadFile(t, f.paths.StatefilePath))
	if err != nil || len(snapshot.ManagedPaths()) != 1 {
		t.Fatal("real whole producer did not persist its native baseline", err)
	}
	declaration, err := declarationmanifest.Decode(testkit.ReadFile(t, f.manifest))
	if err != nil {
		t.Fatal(err)
	}
	config := declaration.HostConfigs()[0]
	expected, err := aggregate.HostConfigDocumentSubject(config.ID(), config.Target(), config.Scope())
	if err != nil || snapshot.ManagedPaths()[0].Subject() != expected || snapshot.ManagedPaths()[0].ContentHash() != artifact.HashFileContent([]byte(f.original)) {
		t.Fatal("real producer lost canonical placement identity or actual hash", err)
	}
	runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check", "--json")
}

func (f nativeDocumentTransitionFixture) narrow(t *testing.T, fragment string) {
	t.Helper()
	f.writeMode(t, "keys")
	testkit.WriteFile(t, f.workspace, filepath.Base(f.source), fragment)
	runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
}

func TestNativeDocumentRealProducerThenKeyNarrowing(t *testing.T) {
	for _, selected := range []target.Target{target.TargetCodex, target.TargetPi} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				f := newNativeDocumentTransitionFixture(t, selected, scope)
				f.applyWhole(t)
				f.narrow(t, f.changed)
				preview := clijson.DecodePlan(t, []byte(runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--dry-run", "--json")))
				if preview.ActionCount != 0 || len(preview.NativeNarrowings) != 1 || preview.NativeNarrowings[0].DesiredKeyCount != 2 || !preview.NativeNarrowings[0].ValuesOmitted {
					t.Fatalf("narrowing preview lost its public action: %+v", preview)
				}
				applied := clijson.DecodeApplyResult(t, []byte(runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes", "--json")))
				if applied.ActionCount != 1 || len(applied.NativeNarrowings) != 1 || applied.NativeNarrowings[0] != preview.NativeNarrowings[0] {
					t.Fatalf("ownership narrowing was not reported as its actual action: %+v", applied)
				}
				content := testkit.ReadFile(t, f.destination)
				requireNativeSharedValues(t, f.syntax, content, f.changed, true)
				requireNativeSharedValues(t, f.syntax, content, f.released, true)
				snapshot, err := statefile.Decode(testkit.ReadFile(t, f.paths.StatefilePath))
				if err != nil || len(snapshot.ManagedPaths()) != 0 || len(snapshot.ManagedAggregates()) != 2 {
					t.Fatal("narrowing lost key state or retained whole ownership", err)
				}
				runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check", "--json")
				runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes")
				if !bytes.Equal(content, testkit.ReadFile(t, f.destination)) {
					t.Fatal("repeat apply rewrote narrowed content")
				}
				testkit.AssertNoRecoveryArtifacts(t, f.workspace)
			})
		}
	}
}

func TestNativeDocumentPreparedWholePipeline(t *testing.T) {
	t.Run("removal", func(t *testing.T) {
		f := newNativeDocumentTransitionFixture(t, target.TargetCodex, target.ScopeProject)
		f.applyWhole(t)
		testkit.WriteFile(t, f.workspace, "daem.toml", "version=1\ntargets=['codex']\n")
		runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
		prepared, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: f.manifest})
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Close()
		result, err := applyworkflow.ExecuteWithOptions(t.Context(), prepared, applyworkflow.ExecuteOptions{})
		if err != nil {
			t.Fatalf("execute whole removal: %v; result=%+v", err, result)
		}
		testkit.AssertPathMissing(t, f.destination)
	})
	f := newNativeDocumentTransitionFixture(t, target.TargetPi, target.ScopeProject)
	prepared, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: f.manifest})
	if err != nil {
		t.Fatal("prepare whole", err)
	}
	defer prepared.Close()
	result, err := applyworkflow.ExecuteWithOptions(t.Context(), prepared, applyworkflow.ExecuteOptions{})
	if err != nil {
		t.Fatalf("execute whole: %v; started=%t", err, result.ExecutionAttempted)
	}
	testkit.AssertFileContent(t, f.destination, f.original)
	f.narrow(t, f.changed)
	narrowing, err := applyworkflow.PlanWrite(t.Context(), applyworkflow.CommandInput{ManifestPath: f.manifest})
	if err != nil {
		t.Fatal("prepare narrowing", err)
	}
	defer narrowing.Close()
	result, err = applyworkflow.ExecuteWithOptions(t.Context(), narrowing, applyworkflow.ExecuteOptions{})
	if err != nil {
		t.Fatalf("execute narrowing: %v; started=%t", err, result.ExecutionAttempted)
	}
}

func TestNativeDocumentNarrowingRecoveryUsesRealWholeBaseline(t *testing.T) {
	for _, selected := range []target.Target{target.TargetCodex, target.TargetPi} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, boundary := range []string{"host", "state"} {
				t.Run(fmt.Sprintf("%s/%s/%s", selected, scope, boundary), func(t *testing.T) {
					f := newNativeDocumentTransitionFixture(t, selected, scope)
					f.original, f.changed = "[prefs]", "[prefs]\nfoo=3\n[new]\nvalue=4\n"
					if selected == target.TargetPi {
						f.original, f.changed = `{"prefs":{}}`, `{"prefs":{"foo":3},"new":{"value":4}}`
					}
					testkit.WriteFile(t, f.workspace, filepath.Base(f.source), f.original)
					runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
					f.applyWhole(t)
					before := testkit.ReadFile(t, f.paths.StatefilePath)
					f.narrow(t, f.changed)
					code, stdout, stderr := invokeNativeRecoveryCommand(t, f.nativeRecoveryFixture, boundary, "apply", "--yes")
					if code != nativeRecoveryExit {
						t.Fatalf("real narrowing did not reach %s publication: %d %s %s", boundary, code, stdout, stderr)
					}
					current := testkit.ReadFile(t, f.destination)
					info, err := os.Stat(f.destination)
					if err != nil {
						t.Fatal(err)
					}
					runNativeLifecycleCommand(t, "recover", "--manifest", f.manifest, "--dry-run")
					runNativeLifecycleCommand(t, "recover", "--manifest", f.manifest, "--yes")
					if boundary == "host" {
						testkit.AssertFileContent(t, f.destination, f.original)
						if !bytes.Equal(before, testkit.ReadFile(t, f.paths.StatefilePath)) {
							t.Fatal("rollback changed the actual persisted whole baseline")
						}
						f.writeMode(t, "document")
						testkit.WriteFile(t, f.workspace, filepath.Base(f.source), f.original)
						runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
						runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check", "--json")
						f.narrow(t, f.changed)
					} else {
						after, err := os.Stat(f.destination)
						if err != nil || !os.SameFile(info, after) || !bytes.Equal(current, testkit.ReadFile(t, f.destination)) {
							t.Fatal("finalization replayed host publication", err)
						}
					}
					settled := snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)
					invokeNativeRecoveryCommand(t, f.nativeRecoveryFixture, "none", "recover", "--yes")
					if !reflect.DeepEqual(settled, snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)) {
						t.Fatal("repeat recovery changed a settled operation")
					}
					runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes")
					runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check", "--json")
					testkit.AssertNoRecoveryArtifacts(t, f.workspace)
				})
			}
		}
	}
}

func TestNativeDocumentNarrowingReleasedEditsAndThirdValues(t *testing.T) {
	for _, selected := range []target.Target{target.TargetCodex, target.TargetPi} {
		for _, third := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/third=%t", selected, third), func(t *testing.T) {
				f := newNativeDocumentTransitionFixture(t, selected, target.ScopeGlobal)
				f.applyWhole(t)
				f.narrow(t, f.changed)
				code, stdout, stderr := invokeNativeRecoveryCommand(t, f.nativeRecoveryFixture, "host", "apply", "--yes")
				if code != nativeRecoveryExit {
					t.Fatalf("no real interruption: %d %s %s", code, stdout, stderr)
				}
				values := map[string]int{"foo": 3, "bar": 9, "new": 4}
				if third {
					values["foo"] = 99
				}
				content := []byte(fmt.Sprintf("# external released edit\nfoo=%d\nbar=9\nnew=4\n", values["foo"]))
				if selected == target.TargetPi {
					content, _ = json.Marshal(values)
					content = append([]byte("// external released edit\n"), content...)
				}
				if err := os.WriteFile(f.destination, content, 0o600); err != nil {
					t.Fatal(err)
				}
				if third {
					before := snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)
					for _, args := range [][]string{{"recover", "--dry-run"}, {"recover", "--yes"}} {
						code, _, _ := invokeNativeRecoveryCommand(t, f.nativeRecoveryFixture, "none", args...)
						if code == 0 {
							t.Fatal("third retained value was accepted")
						}
					}
					if !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)) {
						t.Fatal("refused recovery changed durable evidence")
					}
					return
				}
				runNativeLifecycleCommand(t, "recover", "--manifest", f.manifest, "--yes")
				restored := testkit.ReadFile(t, f.destination)
				requireNativeSharedValues(t, f.syntax, restored, f.retained, true)
				requireNativeSharedValues(t, f.syntax, restored, map[bool]string{true: `{"bar":9}`, false: "bar=9\n"}[selected == target.TargetPi], true)
				if !bytes.Contains(restored, []byte("external released edit")) {
					t.Fatal("released comment was overwritten")
				}
				settled := snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)
				invokeNativeRecoveryCommand(t, f.nativeRecoveryFixture, "none", "recover", "--yes")
				if !reflect.DeepEqual(settled, snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)) {
					t.Fatal("repeat recovery changed settled released edits")
				}
			})
		}
	}
}

func TestNativeDocumentNarrowingNoNewCustody(t *testing.T) {
	for _, selected := range []target.Target{target.TargetCodex, target.TargetPi} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, variant := range []string{"matching-retained", "capture-required", "empty-release"} {
				t.Run(fmt.Sprintf("%s/%s/%s", selected, scope, variant), func(t *testing.T) {
					f := newNativeDocumentTransitionFixture(t, selected, scope)
					f.applyWhole(t)
					external := strings.Replace(f.original, "bar = 2", "bar = 9", 1)
					if selected == target.TargetPi {
						external = strings.Replace(f.original, `"bar": 2`, `"bar": 9`, 1)
					}
					if err := os.WriteFile(f.destination, []byte(external), 0o600); err != nil {
						t.Fatal(err)
					}
					desired := f.retained
					if variant == "capture-required" {
						desired = f.changed
					}
					if variant == "empty-release" {
						desired = ""
						if selected == target.TargetPi {
							desired = `{}`
						}
					}
					f.narrow(t, desired)
					if variant == "capture-required" {
						before := snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)
						code, _, _ := invokeNativeRecoveryCommand(t, f.nativeRecoveryFixture, "none", "apply", "--yes")
						if code == 0 || !reflect.DeepEqual(before, snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)) {
							t.Fatal("capture-required narrowing modified host or ownership")
						}
						f.narrow(t, f.retained)
					}
					runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes")
					testkit.AssertFileContent(t, f.destination, external)
					snapshot, err := statefile.Decode(testkit.ReadFile(t, f.paths.StatefilePath))
					count := 1
					if variant == "empty-release" {
						count = 0
					}
					if err != nil || len(snapshot.ManagedPaths()) != 0 || len(snapshot.ManagedAggregates()) != count {
						t.Fatal("scope release lost its exact state partition", err)
					}
					runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check", "--json")
					runNativeLifecycleCommand(t, "list", "outputs", "--manifest", f.manifest, "--json")
					testkit.AssertNoRecoveryArtifacts(t, f.workspace)
				})
			}
		}
	}
}

func TestNativeDocumentWholeUpdateAndRemoval(t *testing.T) {
	for _, selected := range []target.Target{target.TargetCodex, target.TargetPi} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				f := newNativeDocumentTransitionFixture(t, selected, scope)
				f.applyWhole(t)
				updated := f.original + "\n# changed whole source\n"
				if selected == target.TargetPi {
					updated = f.original + "\n// changed whole source\n"
				}
				testkit.WriteFile(t, f.workspace, filepath.Base(f.source), updated)
				runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
				runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes")
				testkit.AssertFileContent(t, f.destination, updated)
				runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check", "--json")
				runNativeLifecycleCommand(t, "list", "outputs", "--manifest", f.manifest, "--json")
				testkit.WriteFile(t, f.workspace, "daem.toml", fmt.Sprintf("version=1\ntargets=[%q]\n", selected))
				runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
				runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes")
				testkit.AssertPathMissing(t, f.destination)
				runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check", "--json")
			})
		}
	}
}

func TestNativeDocumentNarrowingReturnedErrors(t *testing.T) {
	for _, selected := range []target.Target{target.TargetCodex, target.TargetPi} {
		for _, boundary := range []string{"return-error", "cancel", "return-state-error"} {
			t.Run(string(selected)+"/"+boundary, func(t *testing.T) {
				f := newNativeDocumentTransitionFixture(t, selected, target.ScopeGlobal)
				f.original, f.changed = "[prefs]", "[prefs]\nfoo=3\n[new]\nvalue=4\n"
				if selected == target.TargetPi {
					f.original, f.changed = `{"prefs":{}}`, `{"prefs":{"foo":3},"new":{"value":4}}`
				}
				testkit.WriteFile(t, f.workspace, filepath.Base(f.source), f.original)
				runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
				f.applyWhole(t)
				before := testkit.ReadFile(t, f.paths.StatefilePath)
				f.narrow(t, f.changed)
				code, stdout, stderr := invokeNativeRecoveryCommand(t, f.nativeRecoveryFixture, boundary, "apply", "--yes")
				if code == 0 || code == nativeRecoveryExit {
					t.Fatalf("expected returned failure: %d %s %s", code, stdout, stderr)
				}
				if boundary == "return-state-error" {
					requireNativeSharedValues(t, f.syntax, testkit.ReadFile(t, f.destination), f.changed, true)
					runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check", "--json")
				} else {
					testkit.AssertFileContent(t, f.destination, f.original)
					if !bytes.Equal(before, testkit.ReadFile(t, f.paths.StatefilePath)) {
						t.Fatal("compensation changed the actual whole baseline")
					}
				}
				testkit.AssertNoRecoveryArtifacts(t, f.workspace)
			})
		}
	}
}

func TestNativeDocumentWholeRecovery(t *testing.T) {
	for _, selected := range []target.Target{target.TargetCodex, target.TargetPi} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, remove := range []bool{false, true} {
				for _, boundary := range []string{"host", "state"} {
					t.Run(fmt.Sprintf("%s/%s/remove=%t/%s", selected, scope, remove, boundary), func(t *testing.T) {
						f := newNativeDocumentTransitionFixture(t, selected, scope)
						checkpoint := boundary
						var beforeState []byte
						if remove {
							f.applyWhole(t)
							beforeState = testkit.ReadFile(t, f.paths.StatefilePath)
							testkit.WriteFile(t, f.workspace, "daem.toml", fmt.Sprintf("version=1\ntargets=[%q]\n", selected))
							runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
							if boundary == "host" {
								checkpoint = "host-removal"
							}
						}
						code, stdout, stderr := invokeNativeRecoveryCommand(t, f.nativeRecoveryFixture, checkpoint, "apply", "--yes")
						if code != nativeRecoveryExit {
							t.Fatalf("whole command did not reach actual publication: %d %s %s", code, stdout, stderr)
						}
						published, publishErr := os.Stat(f.destination)
						if remove && !errors.Is(publishErr, os.ErrNotExist) || !remove && publishErr != nil {
							t.Fatal("wrong published whole image", publishErr)
						}
						runNativeLifecycleCommand(t, "recover", "--manifest", f.manifest, "--dry-run")
						runNativeLifecycleCommand(t, "recover", "--manifest", f.manifest, "--yes")
						if boundary == "host" {
							if remove {
								testkit.AssertFileContent(t, f.destination, f.original)
								if !bytes.Equal(beforeState, testkit.ReadFile(t, f.paths.StatefilePath)) {
									t.Fatal("whole removal rollback changed its persisted baseline")
								}
							} else {
								testkit.AssertPathMissing(t, f.destination)
							}
						} else {
							if remove {
								testkit.AssertPathMissing(t, f.destination)
							} else {
								after, err := os.Stat(f.destination)
								if err != nil || !os.SameFile(published, after) {
									t.Fatal("whole finalization replayed publication", err)
								}
								testkit.AssertFileContent(t, f.destination, f.original)
							}
						}
						runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes")
						runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check", "--json")
						testkit.AssertNoRecoveryArtifacts(t, f.workspace)
					})
				}
			}
		}
	}
}

func prepareNativeKeyToWholeFixture(t *testing.T, selected target.Target, scope target.Scope) nativeDocumentTransitionFixture {
	t.Helper()
	f := newNativeDocumentTransitionFixture(t, selected, scope)
	testkit.WriteFile(t, filepath.Dir(f.destination), filepath.Base(f.destination), f.original)
	if err := os.Chmod(f.destination, 0o600); err != nil {
		t.Fatal(err)
	}
	f.narrow(t, f.retained)
	runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes", "--manage-existing")
	before, err := statefile.Decode(testkit.ReadFile(t, f.paths.StatefilePath))
	if err != nil || len(before.ManagedPaths()) != 0 || len(before.ManagedAggregates()) != 1 {
		t.Fatal("real keyed producer did not establish the adoption baseline", err)
	}
	testkit.AssertFileContent(t, f.destination, f.original)
	testkit.WriteFile(t, f.workspace, filepath.Base(f.source), string(testkit.ReadFile(t, f.destination)))
	f.writeMode(t, "document")
	runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
	return f
}

func TestNativeDocumentKeyToWholeMatchingAdoption(t *testing.T) {
	for _, selected := range []target.Target{target.TargetCodex, target.TargetPi} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				f := prepareNativeKeyToWholeFixture(t, selected, scope)
				before, err := os.Stat(f.destination)
				if err != nil {
					t.Fatal(err)
				}
				preview := snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)
				runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--dry-run", "--manage-existing")
				if !reflect.DeepEqual(preview, snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)) {
					t.Fatal("whole adoption preview changed host or management evidence")
				}
				code, stdout, stderr := invokeNativeRecoveryCommand(t, f.nativeRecoveryFixture, "none", "apply", "--yes", "--manage-existing", "--verbose")
				if code != 0 {
					t.Fatalf("matching whole adoption failed: %d %s %s", code, stdout, stderr)
				}
				after, err := os.Stat(f.destination)
				if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
					t.Fatal("record-only adoption rewrote the host file or permissions", err)
				}
				testkit.AssertFileContent(t, f.destination, f.original)
				stateBytes := testkit.ReadFile(t, f.paths.StatefilePath)
				state, err := statefile.Decode(stateBytes)
				if err != nil || len(state.ManagedAggregates()) != 0 || len(state.ManagedPaths()) != 1 || state.ManagedPaths()[0].ContentHash() != artifact.HashFileContent([]byte(f.original)) {
					t.Fatal("whole adoption lost its actual baseline or retained key ownership", err)
				}
				runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check", "--json")
				runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes")
				if !bytes.Equal(stateBytes, testkit.ReadFile(t, f.paths.StatefilePath)) {
					t.Fatal("whole adoption retry changed its completed state")
				}
				testkit.AssertNoRecoveryArtifacts(t, f.workspace)
			})
		}
	}
}

func TestNativeDocumentKeyToWholeRefusesUnsafeAdoption(t *testing.T) {
	for _, selected := range []target.Target{target.TargetCodex, target.TargetPi} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, fault := range []string{"no consent", "different bytes", "stale lock", "mode mismatch", "missing file", "retarget"} {
				t.Run(string(selected)+"/"+string(scope)+"/"+fault, func(t *testing.T) {
					f := prepareNativeKeyToWholeFixture(t, selected, scope)
					other := filepath.Join(filepath.Dir(f.destination), "other"+filepath.Ext(f.destination))
					switch fault {
					case "stale lock":
						testkit.WriteFile(t, filepath.Dir(f.destination), filepath.Base(f.destination), f.changed)
						testkit.WriteFile(t, f.workspace, filepath.Base(f.source), f.changed)
					case "different bytes":
						testkit.WriteFile(t, f.workspace, filepath.Base(f.source), f.original+"\n")
						runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
					case "mode mismatch":
						if err := os.Chmod(f.destination, 0o644); err != nil {
							t.Fatal(err)
						}
					case "missing file":
						if err := os.Remove(f.destination); err != nil {
							t.Fatal(err)
						}
					case "retarget":
						testkit.WriteFile(t, filepath.Dir(other), filepath.Base(other), f.original)
						if err := os.Chmod(other, 0o600); err != nil {
							t.Fatal(err)
						}
						f.declaration = strings.Replace(f.declaration, filepath.Base(f.destination), filepath.Base(other), 1)
						f.writeMode(t, "document")
						runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
					}
					before := snapshotNativeRecoveryPaths(t, f.destination, other, f.paths.StatefilePath, f.paths.OwnershipRegistryPath, f.paths.RecoveryDir)
					args := []string{"apply", "--yes", "--json"}
					if fault != "no consent" {
						args = append(args, "--manage-existing")
					}
					code, stdout, stderr := invokeNativeRecoveryCommand(t, f.nativeRecoveryFixture, "none", args...)
					if code == 0 || !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, f.destination, other, f.paths.StatefilePath, f.paths.OwnershipRegistryPath, f.paths.RecoveryDir)) {
						t.Fatalf("unsafe adoption was accepted or had effects: %d %s %s", code, stdout, stderr)
					}
				})
			}
		}
	}
}

func TestNativeDocumentKeyToWholePreservesMatchingCurrentDrift(t *testing.T) {
	for _, selected := range []target.Target{target.TargetCodex, target.TargetPi} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				f := prepareNativeKeyToWholeFixture(t, selected, scope)
				testkit.WriteFile(t, filepath.Dir(f.destination), filepath.Base(f.destination), f.changed)
				testkit.WriteFile(t, f.workspace, filepath.Base(f.source), f.changed)
				runNativeLifecycleCommand(t, "lock", "--manifest", f.manifest)
				before, err := os.Stat(f.destination)
				if err != nil {
					t.Fatal(err)
				}
				runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes", "--manage-existing")
				after, err := os.Stat(f.destination)
				if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
					t.Fatal("matching current drift was overwritten during whole adoption", err)
				}
				testkit.AssertFileContent(t, f.destination, f.changed)
				runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check")
			})
		}
	}
}

func TestNativeDocumentKeyToWholeRecovery(t *testing.T) {
	for _, selected := range []target.Target{target.TargetCodex, target.TargetPi} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			for _, boundary := range []string{"before-state", "state"} {
				t.Run(string(selected)+"/"+string(scope)+"/"+boundary, func(t *testing.T) {
					f := prepareNativeKeyToWholeFixture(t, selected, scope)
					beforeState := testkit.ReadFile(t, f.paths.StatefilePath)
					original, err := os.Stat(f.destination)
					if err != nil {
						t.Fatal(err)
					}
					code, stdout, stderr := invokeNativeRecoveryCommand(t, f.nativeRecoveryFixture, boundary, "apply", "--yes", "--manage-existing")
					if code != nativeRecoveryExit {
						t.Fatalf("adoption did not reach state boundary: %d %s %s", code, stdout, stderr)
					}
					if boundary == "before-state" && !bytes.Equal(beforeState, testkit.ReadFile(t, f.paths.StatefilePath)) {
						t.Fatal("pre-state crash committed the whole baseline")
					}
					crashed, err := statefile.Decode(testkit.ReadFile(t, f.paths.StatefilePath))
					if err != nil {
						t.Fatal(err)
					}
					beforeRecovery := snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)
					runNativeLifecycleCommand(t, "recover", "--manifest", f.manifest, "--dry-run")
					if !reflect.DeepEqual(beforeRecovery, snapshotNativeRecoveryEvidence(t, f.nativeRecoveryFixture)) {
						t.Fatal("adoption recovery dry-run changed retained evidence")
					}
					runNativeLifecycleCommand(t, "recover", "--manifest", f.manifest, "--yes")
					state, err := statefile.Decode(testkit.ReadFile(t, f.paths.StatefilePath))
					if err != nil || boundary == "before-state" && (len(state.ManagedPaths()) != 0 || len(state.ManagedAggregates()) != 1) || boundary == "state" && (len(state.ManagedPaths()) != 1 || len(state.ManagedAggregates()) != 0) {
						t.Fatal("adoption recovery confused key/whole commit phases", err)
					}
					if !state.Equal(crashed) {
						t.Fatal("record-only recovery changed the complete committed state")
					}
					after, err := os.Stat(f.destination)
					if err != nil || !os.SameFile(original, after) || original.Mode() != after.Mode() || !original.ModTime().Equal(after.ModTime()) {
						t.Fatal("adoption recovery replayed a host write", err)
					}
					testkit.AssertFileContent(t, f.destination, f.original)
					runNativeLifecycleCommand(t, "apply", "--manifest", f.manifest, "--yes", "--manage-existing")
					runNativeLifecycleCommand(t, "status", "--manifest", f.manifest, "--check")
					testkit.AssertNoRecoveryArtifacts(t, f.workspace)
				})
			}
		}
	}
}
