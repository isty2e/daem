//go:build darwin || linux

package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestNativePiKeysAndPackageOrderCLILifecycle(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newNativePiOrderFixture(t, scope)
			applied := runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes", "--json")
			requireNativePiOrderResult(t, applied, "converged", true, 2)
			requireNativePiOrderValues(t, fixture.destination, "next-theme", "npm:@acme/alpha@1", "npm:@acme/foreign@1", "npm:@acme/beta@1")
			requireNativeSharedMode(t, fixture.destination, 0o644)
			runNativeLifecycleCommand(t, "status", "--manifest", fixture.manifest, "--check", "--json")
			before := testkit.ReadFile(t, fixture.destination)
			retry := runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes", "--json")
			requireNativePiOrderResult(t, retry, "exact", false, 0)
			testkit.AssertFileContent(t, fixture.destination, string(before))
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
		})
	}
}

func TestNativePiOrderOnlyWithCurrentKeys(t *testing.T) {
	fixture := newNativePiOrderFixture(t, target.ScopeProject)
	testkit.WriteFile(t, filepath.Dir(fixture.manifest), "preferences.json", `{"theme":"fixture-theme"}`)
	runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
	applied := runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes", "--json")
	requireNativePiOrderResult(t, applied, "converged", true, 1)
	requireNativePiOrderValues(t, fixture.destination, "fixture-theme", "npm:@acme/alpha@1", "npm:@acme/foreign@1", "npm:@acme/beta@1")
	runNativeLifecycleCommand(t, "status", "--manifest", fixture.manifest, "--check", "--json")
}

func TestNativePiOrderWithKeyAdditionAndRetirement(t *testing.T) {
	for _, retire := range []bool{false, true} {
		t.Run(fmt.Sprintf("retire=%t", retire), func(t *testing.T) {
			fixture := newNativePiOrderFixture(t, target.ScopeProject)
			source := `{"theme":"fixture-theme","addition":true}`
			if retire {
				source = `{}`
			}
			testkit.WriteFile(t, filepath.Dir(fixture.manifest), "preferences.json", source)
			runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
			applied := runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes", "--json")
			requireNativePiOrderResult(t, applied, "converged", true, 2)
			var document struct {
				Theme    *string `json:"theme"`
				Addition bool    `json:"addition"`
			}
			if err := json.Unmarshal(testkit.ReadFile(t, fixture.destination), &document); err != nil {
				t.Fatal(err)
			}
			if retire && document.Theme != nil || !retire && (document.Theme == nil || *document.Theme != "fixture-theme" || !document.Addition) {
				t.Fatalf("order publication did not preserve its actual key lifecycle: %+v", document)
			}
			runNativeLifecycleCommand(t, "status", "--manifest", fixture.manifest, "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
		})
	}
}

func TestNativePiOrderReturnedStorageErrorsReportSettledEffects(t *testing.T) {
	for _, boundary := range []string{"return-error", "return-state-error"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newNativePiOrderFixture(t, target.ScopeProject)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, boundary, "apply", "--yes", "--json")
			if code == 0 || code == nativeRecoveryExit {
				t.Fatalf("expected a returned storage error, got %d %s %s", code, stdout, stderr)
			}
			if boundary == "return-error" {
				requireNativePiOrderResult(t, stdout, "failed", false, 0)
				requireNativePiOrderValues(t, fixture.destination, "fixture-theme", "npm:@acme/beta@1", "npm:@acme/foreign@1", "npm:@acme/alpha@1")
			} else {
				requireNativePiOrderResult(t, stdout, "failed", true, 2)
				requireNativePiOrderValues(t, fixture.destination, "next-theme", "npm:@acme/alpha@1", "npm:@acme/foreign@1", "npm:@acme/beta@1")
				if strings.Count(stderr, nativeRecoveryReceipt(fixture.destination)) != 1 {
					t.Fatal("committed key/order settlement replayed host publication", stderr)
				}
			}
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
		})
	}
}

func TestNativePiOrderOnlyProcessLossKeepsCommitBoundary(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newNativePiOrderFixture(t, target.ScopeProject)
			testkit.WriteFile(t, filepath.Dir(fixture.manifest), "preferences.json", `{"theme":"fixture-theme"}`)
			runNativeLifecycleCommand(t, "lock", "--manifest", fixture.manifest)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, boundary, "apply", "--yes")
			if code != nativeRecoveryExit {
				t.Fatalf("order-only work did not reach its commit boundary: %d %s %s", code, stdout, stderr)
			}
			runNativeLifecycleCommand(t, "recover", "--manifest", fixture.manifest, "--yes")
			if boundary == "host" {
				requireNativePiOrderValues(t, fixture.destination, "fixture-theme", "npm:@acme/beta@1", "npm:@acme/foreign@1", "npm:@acme/alpha@1")
			} else {
				requireNativePiOrderValues(t, fixture.destination, "fixture-theme", "npm:@acme/alpha@1", "npm:@acme/foreign@1", "npm:@acme/beta@1")
			}
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
		})
	}
}

func TestNativePiOrderProcessLossRecoversJointImage(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newNativePiOrderFixture(t, target.ScopeProject)
			code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, boundary, "apply", "--yes")
			if code != nativeRecoveryExit {
				t.Fatalf("joint key/order publication did not reach its interruption: %d %s %s", code, stdout, stderr)
			}
			requireNativePiOrderValues(t, fixture.destination, "next-theme", "npm:@acme/alpha@1", "npm:@acme/foreign@1", "npm:@acme/beta@1")
			var document map[string]json.RawMessage
			if err := json.Unmarshal(testkit.ReadFile(t, fixture.destination), &document); err != nil {
				t.Fatal(err)
			}
			document["later"] = json.RawMessage("true")
			content, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			testkit.WriteFile(t, filepath.Dir(fixture.destination), "settings.json", string(content))
			runNativeLifecycleCommand(t, "recover", "--manifest", fixture.manifest, "--dry-run")
			runNativeLifecycleCommand(t, "recover", "--manifest", fixture.manifest, "--yes")
			if boundary == "host" {
				requireNativePiOrderValues(t, fixture.destination, "fixture-theme", "npm:@acme/beta@1", "npm:@acme/foreign@1", "npm:@acme/alpha@1")
			} else {
				requireNativePiOrderValues(t, fixture.destination, "next-theme", "npm:@acme/alpha@1", "npm:@acme/foreign@1", "npm:@acme/beta@1")
				testkit.AssertFileContent(t, fixture.destination, string(content))
			}
			document = nil
			if err := json.Unmarshal(testkit.ReadFile(t, fixture.destination), &document); err != nil || string(document["later"]) != "true" {
				t.Fatal("recovery discarded the later unrelated value", err)
			}
			runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes")
			requireNativePiOrderValues(t, fixture.destination, "next-theme", "npm:@acme/alpha@1", "npm:@acme/foreign@1", "npm:@acme/beta@1")
			runNativeLifecycleCommand(t, "status", "--manifest", fixture.manifest, "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
		})
	}
}

func TestNativePiOrderRecoveryUsesDurablePhaseForPartialGroups(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
		for _, variant := range []string{"keys expected", "order expected", "third key", "third order"} {
			t.Run(boundary+"/"+variant, func(t *testing.T) {
				fixture := newNativePiOrderFixture(t, target.ScopeProject)
				code, stdout, stderr := invokeNativeRecoveryCommand(t, fixture, boundary, "apply", "--yes")
				if code != nativeRecoveryExit {
					t.Fatalf("partial-group fixture did not reach its actual publication: %d %s %s", code, stdout, stderr)
				}
				theme := "next-theme"
				packages := `"npm:@acme/beta@1","npm:@acme/foreign@1","npm:@acme/alpha@1"`
				switch variant {
				case "order expected":
					theme = "fixture-theme"
					packages = `"npm:@acme/alpha@1","npm:@acme/foreign@1","npm:@acme/beta@1"`
				case "third key":
					theme = "third-theme"
				case "third order":
					packages = `"npm:@acme/alpha@1","npm:@acme/beta@1","npm:@acme/foreign@1"`
				}
				testkit.WriteFile(t, filepath.Dir(fixture.destination), "settings.json", fmt.Sprintf(`{"theme":%q,"packages":[%s],"foreign":{"retained":true},"later":true}`, theme, packages))
				paths, err := daempaths.Resolve(fixture.manifest)
				if err != nil {
					t.Fatal(err)
				}
				before := snapshotNativeRecoveryPaths(t, fixture.destination, paths.StatefilePath, paths.OwnershipRegistryPath, paths.RecoveryDir)
				if boundary == "state" || variant == "third key" || variant == "third order" {
					if code, stdout, stderr := runOwnershipCLI("recover", "--manifest", fixture.manifest, "--yes"); code == 0 {
						t.Fatalf("mixed After or third-value recovery was admitted: %s %s", stdout, stderr)
					}
					if !reflect.DeepEqual(before, snapshotNativeRecoveryPaths(t, fixture.destination, paths.StatefilePath, paths.OwnershipRegistryPath, paths.RecoveryDir)) {
						t.Fatal("refused partial-group recovery changed host, state, ownership or journal")
					}
					return
				}
				runNativeLifecycleCommand(t, "recover", "--manifest", fixture.manifest, "--dry-run")
				runNativeLifecycleCommand(t, "recover", "--manifest", fixture.manifest, "--yes")
				requireNativePiOrderValues(t, fixture.destination, "fixture-theme", "npm:@acme/beta@1", "npm:@acme/foreign@1", "npm:@acme/alpha@1")
				var document struct {
					Later bool `json:"later"`
				}
				if err := json.Unmarshal(testkit.ReadFile(t, fixture.destination), &document); err != nil || !document.Later {
					t.Fatal("partial-group rollback discarded the later unrelated value", err)
				}
				settled := snapshotNativeRecoveryPaths(t, fixture.destination, paths.StatefilePath, paths.OwnershipRegistryPath, paths.RecoveryDir)
				if code, stdout, stderr := runOwnershipCLI("recover", "--manifest", fixture.manifest, "--yes"); code == 0 {
					t.Fatalf("retired journal unexpectedly admitted recovery: %s %s", stdout, stderr)
				}
				if !reflect.DeepEqual(settled, snapshotNativeRecoveryPaths(t, fixture.destination, paths.StatefilePath, paths.OwnershipRegistryPath, paths.RecoveryDir)) {
					t.Fatal("recovery retry changed the settled operation")
				}
				runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes")
				runNativeLifecycleCommand(t, "status", "--manifest", fixture.manifest, "--check", "--json")
				testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
			})
		}
	}
}

func TestNativePiOrderPreservesPackageOptionsAndForeignSlots(t *testing.T) {
	fixture := newNativePiOrderFixture(t, target.ScopeProject)
	testkit.WriteFile(t, filepath.Dir(fixture.destination), "settings.json", `{"theme":"fixture-theme","packages":[{"source":"npm:@acme/beta@1","opaque":{"enabled":true}},"npm:@acme/foreign@1",{"source":"npm:@acme/alpha@1","skills":["./only"]},"npm:@acme/foreign-two@1"],"foreign":{"retained":true}}`)
	runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes")
	var document struct {
		Packages []json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(testkit.ReadFile(t, fixture.destination), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Packages) != 4 {
		t.Fatal("order convergence discarded a package row")
	}
	for index, expected := range []string{`{"source":"npm:@acme/alpha@1","skills":["./only"]}`, `"npm:@acme/foreign@1"`, `{"source":"npm:@acme/beta@1","opaque":{"enabled":true}}`, `"npm:@acme/foreign-two@1"`} {
		var actualValue, expectedValue any
		if err := json.Unmarshal(document.Packages[index], &actualValue); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(expected), &expectedValue); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actualValue, expectedValue) {
			t.Fatalf("order convergence changed a package row at %d: %s", index, document.Packages[index])
		}
	}
	runNativeLifecycleCommand(t, "status", "--manifest", fixture.manifest, "--check", "--json")
}

func TestNativePiOrderAmbiguityRefusesBeforeKeyEffects(t *testing.T) {
	fixture := newNativePiOrderFixture(t, target.ScopeProject)
	const ambiguous = `{"theme":"fixture-theme","packages":["npm:@acme/beta@1","npm:@acme/foreign@1","npm:@acme/alpha@1","npm:@acme/foreign@2"],"foreign":{"retained":true}}`
	testkit.WriteFile(t, filepath.Dir(fixture.destination), "settings.json", ambiguous)
	statePath := filepath.Join(filepath.Dir(fixture.manifest), ".daem", "state.json")
	beforeState := testkit.ReadFile(t, statePath)
	if code, stdout, stderr := runOwnershipCLI("apply", "--manifest", fixture.manifest, "--yes"); code == 0 {
		t.Fatalf("ambiguous package identities admitted key/order effects: %s %s", stdout, stderr)
	}
	testkit.AssertFileContent(t, fixture.destination, ambiguous)
	testkit.AssertFileContent(t, statePath, string(beforeState))
	testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.manifest))
}

func newNativePiOrderFixture(t *testing.T, scope target.Scope) nativeRecoveryFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	setNativeRecoveryEnvironment(t, root)
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	settings := filepath.Join(workspace, ".pi", "settings.json")
	if scope == target.ScopeGlobal {
		settings = filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "settings.json")
	}
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, filepath.Dir(settings), "settings.json", `{"packages":["npm:@acme/alpha@1","npm:@acme/beta@1"],"foreign":{"retained":true}}`)
	runNativeLifecycleCommand(t, "import", "--manifest", manifest, "--target", "pi", "--scope", string(scope))
	runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
	runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--manage-existing", "--yes")

	declaration := string(testkit.ReadFile(t, manifest)) + fmt.Sprintf("\n[[host_config]]\nid='preferences'\nscope=%q\npath='settings.json'\nsource=%q\nownership='keys'\n", scope, filepath.Join(workspace, "preferences.json"))
	testkit.WriteFile(t, workspace, "daem.toml", declaration)
	testkit.WriteFile(t, workspace, "preferences.json", `{"theme":"fixture-theme"}`)
	runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
	runNativeLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")

	testkit.WriteFile(t, filepath.Dir(settings), "settings.json", `{"theme":"fixture-theme","packages":["npm:@acme/beta@1","npm:@acme/foreign@1","npm:@acme/alpha@1"],"foreign":{"retained":true}}`)
	if err := os.Chmod(settings, 0o644); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, workspace, "preferences.json", `{"theme":"next-theme"}`)
	runNativeLifecycleCommand(t, "lock", "--manifest", manifest)
	return nativeRecoveryFixture{manifest: manifest, destination: settings}
}

func requireNativePiOrderValues(t *testing.T, path, theme string, packages ...string) {
	t.Helper()
	var document struct {
		Theme    string   `json:"theme"`
		Packages []string `json:"packages"`
		Foreign  struct {
			Retained bool `json:"retained"`
		} `json:"foreign"`
	}
	content := testkit.ReadFile(t, path)
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	if document.Theme != theme || !document.Foreign.Retained || !slices.Equal(document.Packages, packages) {
		t.Fatalf("key/order convergence changed selected or unrelated values: %s", content)
	}
}

func requireNativePiOrderResult(t *testing.T, content, outcome string, changed bool, actions int) {
	t.Helper()
	var result clijson.ApplyResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.RelationOrderResults) != 1 || result.RelationOrderResults[0].Outcome != outcome || result.RelationOrderResults[0].Changed != changed || result.ActionCount != actions {
		t.Fatalf("joint key/order result does not describe its actual effects: %s", content)
	}
}
