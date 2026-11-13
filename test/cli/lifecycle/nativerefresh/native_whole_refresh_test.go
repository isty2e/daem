//go:build darwin || linux

package cli_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func nativeWholeRefreshFixture(t *testing.T, selected target.Target, extensionScope, wholeScope target.Scope) (nativefixture.PiInvocation, nativefixture.RecoveryFixture, string) {
	t.Helper()
	f := newNativePiRefreshFixture(t, extensionScope, "")
	name, root, content := "prefs.toml", filepath.Join(f.Workspace, ".codex"), "# whole preferences\nfoo=1\nbar=2\n"
	if wholeScope == target.ScopeGlobal {
		root = os.Getenv("CODEX_HOME")
	}
	if selected == target.TargetPi {
		name, root, content = "prefs.jsonc", filepath.Join(f.Workspace, ".pi"), "{\n // whole preferences\n \"foo\":1,\n \"bar\":2,\n}\n"
		if wholeScope == target.ScopeGlobal {
			root = os.Getenv("PI_CODING_AGENT_DIR")
		}
	}
	source := filepath.Join(f.Workspace, "whole"+filepath.Ext(name))
	testkit.WriteFile(t, f.Workspace, filepath.Base(source), content)
	body := fmt.Sprintf("version=1\ntargets=['pi','codex']\n[[extension]]\nid='tools'\ntargets=['pi']\ncarrier='pi-package'\nscope=%q\nsource={host_source='git:github.com/acme/pi-tools@v1'}\n[[host_config]]\nid='preferences'\ntargets=['pi']\nscope=%q\npath='settings.json'\nsource=%q\nownership='keys'\n\n[[host_config]]\nid='whole'\ntargets=[%q]\nscope=%q\npath=%q\nsource=%q\nownership='document'\n", extensionScope, extensionScope, filepath.Join(f.Workspace, "preferences.json"), selected, wholeScope, name, source)
	testkit.WriteFile(t, f.Workspace, "daem.toml", body)
	f.Require(t, "lock")
	paths, err := daempaths.Resolve(f.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	g := nativefixture.RecoveryFixture{Manifest: f.Manifest, Destination: filepath.Join(root, name), Paths: paths, Kind: "whole-refresh"}
	t.Setenv("DAEM_TEST_WHOLE_REFRESH_TARGET", g.Destination)
	t.Setenv("DAEM_TEST_WHOLE_REFRESH_FAULT", "")
	scriptPath, err := exec.LookPath("pi")
	if err != nil {
		t.Fatal(err)
	}
	script := string(testkit.ReadFile(t, scriptPath))
	marker := `printf 'refresh\n' >> "$DAEM_TEST_COMMAND_CALLS"`
	if strings.Count(script, marker) != 1 {
		t.Fatal("Refresh fixture has no unique command observation")
	}
	observer := `
case "$DAEM_TEST_WHOLE_REFRESH_FAULT" in
  bytes) printf 'changed-by-host\n' > "$DAEM_TEST_WHOLE_REFRESH_TARGET" ;;
  mode) /bin/chmod 666 "$DAEM_TEST_WHOLE_REFRESH_TARGET" ;;
  command-mode) /bin/chmod 666 "$DAEM_TEST_WHOLE_REFRESH_TARGET"; exit 23 ;;
  inaccessible) /bin/chmod 000 "$DAEM_TEST_WHOLE_REFRESH_TARGET" ;;
  terminate) /bin/chmod 666 "$DAEM_TEST_WHOLE_REFRESH_TARGET"; kill -KILL "$DAEM_TEST_REFRESH_PARENT_PID"; exit 0 ;;
esac
`
	testkit.WriteFile(t, filepath.Dir(scriptPath), filepath.Base(scriptPath), strings.Replace(script, marker, marker+observer, 1))
	if err := os.Chmod(scriptPath, 0o700); err != nil {
		t.Fatal(err)
	}
	return f, g, source
}

func TestNativeCurrentWholeDocumentWithOrdinaryChange(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				f, whole, _ := nativeWholeRefreshFixture(t, selected, scope, scope)
				f.Require(t, "apply", "--yes")
				before := testkit.ReadFile(t, whole.Destination)
				calls := string(testkit.ReadFile(t, f.Calls))
				f.AddInstructions(t, "independent ordinary change\n")
				f.Require(t, "apply", "--yes")
				if got := testkit.ReadFile(t, whole.Destination); !reflect.DeepEqual(got, before) {
					t.Fatal("ordinary change rewrote its retained whole document")
				}
				if got := string(testkit.ReadFile(t, f.Calls)); got != calls {
					t.Fatal("ordinary change repeated an already-current host command")
				}
				if got := string(testkit.ReadFile(t, filepath.Join(f.Workspace, "AGENTS.md"))); got != "independent ordinary change\n" {
					t.Fatal("ordinary write was omitted from whole-document capture")
				}
				f.Require(t, "status", "--check", "--json")
				testkit.AssertNoRecoveryArtifacts(t, f.Workspace)
			})
		}
	}
}

func TestNativeWholeRefreshPreservesCurrentOwnership(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				f, g, source := nativeWholeRefreshFixture(t, selected, scope, scope)
				f.Require(t, "apply", "--yes")
				before, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, g.Paths.StatefilePath))
				if err != nil {
					t.Fatal(err)
				}
				content := string(testkit.ReadFile(t, g.Destination))
				manifest, locked := string(testkit.ReadFile(t, f.Manifest)), string(testkit.ReadFile(t, g.Paths.LockfilePath))
				f.Require(t, "refresh", "extension", "tools", "--dry-run", "--json")
				testkit.AssertFileContent(t, f.Calls, "install\ninstall\n")
				for range 2 {
					f.Require(t, "refresh", "extension", "tools", "--yes", "--json")
				}
				store, err := ownershipstore.New(f.LaunchRegistry)
				if err != nil {
					t.Fatal(err)
				}
				registry, err := store.LoadProtectionRegistry(t.Context())
				if err != nil || len(registry.PendingProtections()) != 1 {
					t.Fatal("whole Refresh did not retain its bound protection at launch", err)
				}
				protection := registry.PendingProtections()[0]
				if _, bound := protection.Execution(); !bound || protection.Phase() != ownership.ProtectionBound || len(protection.Documents()) != 2 {
					t.Fatal("whole Refresh launched without both actual protected documents")
				}
				after, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, g.Paths.StatefilePath))
				if err != nil || !slices.EqualFunc(before.ManagedPaths(), after.ManagedPaths(), durable.ManagedPathState.Equal) || !slices.EqualFunc(before.ManagedAggregates(), after.ManagedAggregates(), durable.ManagedAggregateState.Equal) {
					t.Fatal("Refresh applied settings or changed their ownership", err)
				}
				testkit.AssertFileContent(t, g.Destination, content)
				testkit.AssertFileContent(t, source, content)
				testkit.AssertFileContent(t, f.Manifest, manifest)
				testkit.AssertFileContent(t, g.Paths.LockfilePath, locked)
				testkit.AssertFileContent(t, f.Calls, "install\ninstall\nrefresh\nrefresh\n")
				nativefixture.RequireSharedMode(t, g.Destination, 0o600)
				f.Require(t, "status", "--check")
				testkit.AssertNoRecoveryArtifacts(t, f.Workspace)
			})
		}
	}
}

func TestNativeWholeRefreshRequiresAppliedAndCurrentFiles(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, change := range []string{"desired", "host", "external-match", "missing", "widened", "tightened", "new", "narrow", "empty-narrow", "adopt"} {
			t.Run(string(selected)+"/"+change, func(t *testing.T) {
				f, g, source := nativeWholeRefreshFixture(t, selected, target.ScopeProject, target.ScopeProject)
				original := string(testkit.ReadFile(t, source))
				if change == "adopt" || change == "new" {
					testkit.WriteFile(t, filepath.Dir(g.Destination), filepath.Base(g.Destination), original)
					if err := os.Chmod(g.Destination, 0o600); err != nil {
						t.Fatal(err)
					}
					if change == "adopt" {
						body := string(testkit.ReadFile(t, f.Manifest))
						testkit.WriteFile(t, f.Workspace, "daem.toml", strings.Replace(body, "ownership='document'", "ownership='keys'", 1))
						fragment := "foo=1\n"
						if selected == target.TargetPi {
							fragment = `{"foo":1}`
						}
						testkit.WriteFile(t, f.Workspace, filepath.Base(source), fragment)
						f.Require(t, "lock")
						f.Require(t, "apply", "--yes", "--manage-existing")
						testkit.WriteFile(t, f.Workspace, "daem.toml", body)
						testkit.WriteFile(t, f.Workspace, filepath.Base(source), original)
					}
				} else {
					f.Require(t, "apply", "--yes")
				}
				changed := original + "# changed preferences\n"
				if selected == target.TargetPi {
					changed = original + "// changed preferences\n"
				}
				switch change {
				case "desired", "external-match":
					testkit.WriteFile(t, f.Workspace, filepath.Base(source), changed)
					if change == "external-match" {
						testkit.WriteFile(t, filepath.Dir(g.Destination), filepath.Base(g.Destination), changed)
					}
				case "host":
					testkit.WriteFile(t, filepath.Dir(g.Destination), filepath.Base(g.Destination), changed)
				case "missing":
					if err := os.Remove(g.Destination); err != nil {
						t.Fatal(err)
					}
				case "widened", "tightened":
					mode := os.FileMode(0o666)
					if change == "tightened" {
						mode = 0o400
					}
					if err := os.Chmod(g.Destination, mode); err != nil {
						t.Fatal(err)
					}
				case "narrow", "empty-narrow":
					fragment := "foo=1\n"
					if selected == target.TargetPi {
						fragment = `{"foo":1}`
					}
					if change == "empty-narrow" {
						fragment = ""
						if selected == target.TargetPi {
							fragment = "{}"
						}
					}
					testkit.WriteFile(t, f.Workspace, "daem.toml", strings.Replace(string(testkit.ReadFile(t, f.Manifest)), "ownership='document'", "ownership='keys'", 1))
					testkit.WriteFile(t, f.Workspace, filepath.Base(source), fragment)
				}
				f.Require(t, "lock")
				before := nativefixture.SnapshotRecoveryEvidence(t, g)
				if code, _, _ := f.Run("refresh", "extension", "tools", "--yes", "--json"); code == 0 || !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryEvidence(t, g)) {
					t.Fatal("Refresh consumed pending ownership or changed non-current settings")
				}
				testkit.AssertFileContent(t, f.Calls, "install\ninstall\n")
				testkit.AssertNoRecoveryArtifacts(t, f.Workspace)
			})
		}
	}
}

func TestNativeWholeRefreshRevalidatesDisclosure(t *testing.T) {
	for _, change := range []string{"contents", "mode", "source"} {
		t.Run(change, func(t *testing.T) {
			f, g, source := nativeWholeRefreshFixture(t, target.TargetPi, target.ScopeProject, target.ScopeGlobal)
			f.Require(t, "apply", "--yes")
			prepared, err := refreshworkflow.PlanWrite(t.Context(), refreshworkflow.CommandInput{ManifestPath: f.Manifest, ExtensionID: "tools"}, refreshworkflow.PlanOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if change == "mode" {
				if err := os.Chmod(g.Destination, 0o666); err != nil {
					t.Fatal(err)
				}
			} else {
				path := g.Destination
				if change == "source" {
					path = source
				}
				testkit.WriteFile(t, filepath.Dir(path), filepath.Base(path), string(testkit.ReadFile(t, path))+"// after disclosure\n")
			}
			result, err := refreshworkflow.Execute(t.Context(), prepared, refreshworkflow.ExecuteOptions{})
			if err == nil || result.Attempted || result.ResultClass != refreshworkflow.ResultRefused {
				t.Fatalf("disclosed whole settings were reused: %#v %v", result, err)
			}
			testkit.AssertFileContent(t, f.Calls, "install\ninstall\n")
		})
	}
}

func TestNativeWholeRefreshPreservesRetiringAndCrossScopeFiles(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		t.Run(string(selected), func(t *testing.T) {
			f, g, source := nativeWholeRefreshFixture(t, selected, target.ScopeProject, target.ScopeGlobal)
			f.Require(t, "apply", "--yes")
			original := string(testkit.ReadFile(t, g.Destination))
			before, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, g.Paths.StatefilePath))
			if err != nil {
				t.Fatal(err)
			}
			f.Require(t, "refresh", "extension", "tools", "--yes")
			body, _, present := strings.Cut(string(testkit.ReadFile(t, f.Manifest)), "\n[[host_config]]\nid='whole'")
			if !present {
				t.Fatal("whole Refresh fixture has no unique declaration")
			}
			testkit.WriteFile(t, f.Workspace, "daem.toml", body)
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
			f.Require(t, "lock")
			f.Require(t, "refresh", "extension", "tools", "--yes")
			after, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, g.Paths.StatefilePath))
			if err != nil || !slices.EqualFunc(before.ManagedPaths(), after.ManagedPaths(), durable.ManagedPathState.Equal) || !slices.EqualFunc(before.ManagedAggregates(), after.ManagedAggregates(), durable.ManagedAggregateState.Equal) {
				t.Fatal("Refresh retired or reclassified pending file ownership", err)
			}
			testkit.AssertFileContent(t, g.Destination, original)
			testkit.AssertFileContent(t, f.Calls, "install\ninstall\nrefresh\nrefresh\n")
			testkit.AssertNoRecoveryArtifacts(t, f.Workspace)
		})
	}
}

func TestNativeWholeClaimRefusesPhysicalHostSettingsAliasBeforeRefresh(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			f := newNativePiRefreshFixture(t, scope, "")
			base, _, present := strings.Cut(string(testkit.ReadFile(t, f.Manifest)), "\n[[host_config]]")
			if !present {
				t.Fatal("Pi Refresh fixture has no key declaration")
			}
			testkit.WriteFile(t, f.Workspace, "daem.toml", base)
			f.Require(t, "lock")
			f.Require(t, "apply", "--yes")
			aliasRoot := filepath.Join(filepath.Dir(f.Settings), "alias")
			if err := os.Symlink(".", aliasRoot); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(f.Settings, 0o600); err != nil {
				t.Fatal(err)
			}
			testkit.WriteFile(t, f.Workspace, "whole.jsonc", string(testkit.ReadFile(t, f.Settings)))
			body := base + fmt.Sprintf("\n[[host_config]]\nid='whole'\nscope=%q\npath='alias/settings.json'\nsource=%q\nownership='document'\n", scope, filepath.Join(f.Workspace, "whole.jsonc"))
			testkit.WriteFile(t, f.Workspace, "daem.toml", body)
			f.Require(t, "lock")
			paths, err := daempaths.Resolve(f.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			g := nativefixture.RecoveryFixture{Manifest: f.Manifest, Destination: f.Settings, Paths: paths, Kind: "whole-refresh"}
			before := nativefixture.SnapshotRecoveryEvidence(t, g)
			code, stdout, stderr := f.Run("apply", "--yes", "--manage-existing")
			if code == 0 || !strings.Contains(stdout+stderr, "shared native configuration cannot be owned as a whole document") || !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryEvidence(t, g)) {
				t.Fatal("whole physical host-settings alias was admitted or changed management evidence", stdout, stderr)
			}
			testkit.AssertFileContent(t, f.Calls, "install\ninstall\n")
		})
	}
}

func TestNativeWholeRefreshContainsCommandViolations(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, fault := range []string{"bytes", "mode", "command-mode", "inaccessible"} {
			t.Run(string(selected)+"/"+fault, func(t *testing.T) {
				f, g, _ := nativeWholeRefreshFixture(t, selected, target.ScopeProject, target.ScopeGlobal)
				f.Require(t, "apply", "--yes")
				original := string(testkit.ReadFile(t, g.Destination))
				t.Setenv("DAEM_TEST_WHOLE_REFRESH_FAULT", fault)
				code, stdout, stderr := f.Run("refresh", "extension", "tools", "--yes", "--json")
				if code == 0 || decodeNativeRefreshClass(t, stdout) != "partial" {
					t.Fatalf("whole-file violation was not a truthful partial Refresh: %d %s %s", code, stdout, stderr)
				}
				if fault == "bytes" {
					testkit.AssertFileContent(t, g.Destination, "changed-by-host\n")
					testkit.WriteFile(t, filepath.Dir(g.Destination), filepath.Base(g.Destination), original)
				} else if fault == "inaccessible" {
					if code, _, _ := f.Run("refresh", "extension", "tools", "--yes"); code == 0 {
						t.Fatal("unqualified whole metadata became a fresh baseline")
					}
					if err := os.Chmod(g.Destination, 0o600); err != nil {
						t.Fatal(err)
					}
					f.Require(t, "recover", "--dry-run")
					f.Require(t, "recover", "--yes")
				}
				nativefixture.RequireSharedMode(t, g.Destination, 0o600)
				testkit.AssertFileContent(t, f.Calls, "install\ninstall\nrefresh\n")
				t.Setenv("DAEM_TEST_WHOLE_REFRESH_FAULT", "")
				f.Require(t, "refresh", "extension", "tools", "--yes")
				f.Require(t, "status", "--check")
				testkit.AssertFileContent(t, f.Calls, "install\ninstall\nrefresh\nrefresh\n")
				testkit.AssertNoRecoveryArtifacts(t, f.Workspace)
			})
		}
	}
}

func TestNativeWholeRefreshProcessLossKeepsFenceUntilRecovery(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				f, g, _ := nativeWholeRefreshFixture(t, selected, scope, scope)
				f.Require(t, "apply", "--yes")
				body := string(testkit.ReadFile(t, f.Manifest))
				original := string(testkit.ReadFile(t, g.Destination))
				t.Setenv("DAEM_TEST_WHOLE_REFRESH_FAULT", "terminate")
				code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, g, "native-refresh-command", "refresh", "extension", "tools", "--yes", "--json")
				if code == 0 || code == nativefixture.RecoveryExit {
					t.Fatalf("whole Refresh did not lose its actual process: %d %s %s", code, stdout, stderr)
				}
				store, err := ownershipstore.New(g.Paths.OwnershipRegistryPath)
				if err != nil {
					t.Fatal(err)
				}
				registry, err := store.LoadProtectionRegistry(t.Context())
				if err != nil || len(registry.PendingProtections()) != 1 || registry.PendingProtections()[0].Phase() != ownership.ProtectionBound || len(registry.PendingProtections()[0].Documents()) != 2 {
					t.Fatal("lost whole Refresh did not preserve its complete bound fence", err)
				}
				withoutDeclarations, _, present := strings.Cut(body, "\n[[host_config]]")
				if !present {
					t.Fatal("whole Refresh fixture has no native declarations")
				}
				testkit.WriteFile(t, f.Workspace, "daem.toml", withoutDeclarations)
				f.Require(t, "lock")
				t.Setenv("DAEM_TEST_WHOLE_REFRESH_FAULT", "")
				if code, _, _ := f.Run("refresh", "extension", "tools", "--yes"); code == 0 {
					t.Fatal("declaration removal bypassed a pending whole protection")
				}
				testkit.AssertFileContent(t, f.Calls, "install\ninstall\nrefresh\n")
				before := nativefixture.SnapshotRecoveryEvidence(t, g)
				f.Require(t, "recover", "--dry-run")
				if !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryEvidence(t, g)) {
					t.Fatal("whole protection recovery preview changed evidence")
				}
				f.Require(t, "recover", "--yes")
				nativefixture.RequireSharedMode(t, g.Destination, 0o600)
				testkit.AssertFileContent(t, g.Destination, original)
				testkit.AssertFileContent(t, f.Calls, "install\ninstall\nrefresh\n")
				testkit.WriteFile(t, f.Workspace, "daem.toml", body)
				f.Require(t, "lock")
				f.Require(t, "refresh", "extension", "tools", "--yes")
				f.Require(t, "status", "--check")
				testkit.AssertFileContent(t, f.Calls, "install\ninstall\nrefresh\nrefresh\n")
				testkit.AssertNoRecoveryArtifacts(t, f.Workspace)
			})
		}
	}
}
