package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestPiPackageAlreadyAbsentRetiresClaimWithoutHostInvocation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newPiRemovalLifecycleFixture(
				t,
				scope,
				"git",
				func(string) string { return "git:github.com/acme/pi-tools@v1" },
			)
			fixture.install(t)
			fixture.removeDeclaration(t)
			fixture.makeSelectedSourceAbsent(t)

			exitCode, stdout, stderr := fixture.runApplyWithRunner(
				t,
				func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					t.Fatal("already-absent relation invoked Pi")
					return subprocess.CommandResult{}
				},
			)
			if exitCode != 0 || stderr != "" {
				t.Fatalf(
					"apply exitCode=%d stdout=%q stderr=%q",
					exitCode,
					stdout,
					stderr,
				)
			}
			if len(fixture.requests) != 1 {
				t.Fatalf("requests = %#v, want install only", fixture.requests)
			}
			fixture.assertClaimCount(t, 0)
			fixture.assertRetainedBoundaries(t)
		})
	}
}

func TestPiPackagePartialRemovalSettlesWithoutHostReinvocation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newPiRemovalLifecycleFixture(
				t,
				scope,
				"npm",
				func(string) string { return "npm:pi-tools@1.2.3" },
			)
			fixture.install(t)
			fixture.removeDeclaration(t)

			exitCode, stdout, stderr := fixture.runApplyWithRunner(
				t,
				func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
					fixture.assertRequest(t, request, "remove")
					writePiPackageSettings(
						t,
						fixture.selectedSettings,
						[]string{"npm:unrelated@9"},
					)
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
				},
			)
			if exitCode != 1 || stderr != "" {
				t.Fatalf(
					"partial apply exitCode=%d stdout=%q stderr=%q, want JSON failure",
					exitCode,
					stdout,
					stderr,
				)
			}
			if !strings.Contains(stdout, "effect_postcondition_unsatisfied") {
				t.Fatalf("partial apply stdout = %q, want unsatisfied effect", stdout)
			}
			fixture.assertClaimCount(t, 1)
			if err := os.RemoveAll(fixture.artifactPath); err != nil {
				t.Fatal(err)
			}

			exitCode, stdout, stderr = fixture.runApplyWithRunner(
				t,
				func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					t.Fatal("pending settlement reinvoked Pi")
					return subprocess.CommandResult{}
				},
			)
			if exitCode != 0 || stderr != "" {
				t.Fatalf(
					"settlement apply exitCode=%d stdout=%q stderr=%q",
					exitCode,
					stdout,
					stderr,
				)
			}
			if len(fixture.requests) != 2 {
				t.Fatalf("requests = %#v, want install and one remove", fixture.requests)
			}
			fixture.assertClaimCount(t, 0)
			fixture.assertRetainedBoundaries(t)
		})
	}
}

func TestPiPackageFailedAttemptWithVerifiedEffectsConverges(t *testing.T) {
	results := []struct {
		name   string
		result subprocess.CommandResult
	}{
		{
			name: "nonzero",
			result: subprocess.CommandResult{
				Started:     true,
				HasExitCode: true,
				ExitCode:    17,
				Stderr:      "host reported failure after committing removal",
			},
		},
		{
			name: "timeout",
			result: subprocess.CommandResult{
				Started:  true,
				TimedOut: true,
				Err:      context.DeadlineExceeded,
			},
		},
	}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, result := range results {
			t.Run(string(scope)+"/"+result.name, func(t *testing.T) {
				fixture := newPiRemovalLifecycleFixture(
					t,
					scope,
					"git",
					func(string) string { return "git:github.com/acme/pi-tools@v1" },
				)
				fixture.install(t)
				fixture.removeDeclaration(t)

				exitCode, stdout, stderr := fixture.runApplyWithRunner(
					t,
					func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
						fixture.assertRequest(t, request, "remove")
						fixture.makeSelectedSourceAbsent(t)
						return result.result
					},
				)
				if exitCode != 0 || stderr != "" {
					t.Fatalf(
						"apply exitCode=%d stdout=%q stderr=%q, want verified convergence",
						exitCode,
						stdout,
						stderr,
					)
				}
				if len(fixture.requests) != 2 {
					t.Fatalf("requests = %#v, want install and one remove", fixture.requests)
				}
				fixture.assertClaimCount(t, 0)
				fixture.assertPendingRemovalCount(t, 0)
				fixture.assertRetainedBoundaries(t)
			})
		}
	}
}

func TestPiPackagePartialAndUnsafeEffectsRequireFreshSettlement(t *testing.T) {
	tests := []struct {
		name           string
		sourceKind     string
		source         func(string) string
		beforeRemoval  func(*testing.T, *piRemovalLifecycleFixture)
		effect         func(*testing.T, *piRemovalLifecycleFixture)
		repair         func(*testing.T, *piRemovalLifecycleFixture)
		assertRetained bool
	}{
		{
			name:       "artifact-only",
			sourceKind: "npm",
			source:     func(string) string { return "npm:pi-tools@1.2.3" },
			effect: func(t *testing.T, fixture *piRemovalLifecycleFixture) {
				t.Helper()
				if err := os.RemoveAll(fixture.artifactPath); err != nil {
					t.Fatal(err)
				}
			},
			repair: func(t *testing.T, fixture *piRemovalLifecycleFixture) {
				t.Helper()
				fixture.makeSelectedSourceAbsent(t)
			},
			assertRetained: true,
		},
		{
			name:       "local-source-mutated",
			sourceKind: "local",
			source:     func(root string) string { return filepath.Join(root, "local-pi-tools") },
			effect: func(t *testing.T, fixture *piRemovalLifecycleFixture) {
				t.Helper()
				writePiPackageSettings(t, fixture.selectedSettings, []string{"npm:unrelated@9"})
				if err := os.WriteFile(fixture.localCanary, []byte("mutated\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			repair: func(t *testing.T, fixture *piRemovalLifecycleFixture) {
				t.Helper()
				if err := os.WriteFile(fixture.localCanary, []byte("retain me\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			assertRetained: true,
		},
		{
			name:       "settings-malformed",
			sourceKind: "npm",
			source:     func(string) string { return "npm:pi-tools@1.2.3" },
			effect: func(t *testing.T, fixture *piRemovalLifecycleFixture) {
				t.Helper()
				if err := os.RemoveAll(fixture.artifactPath); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(
					fixture.selectedSettings,
					[]byte(`{"packages":null}`),
					0o644,
				); err != nil {
					t.Fatal(err)
				}
			},
			repair: func(t *testing.T, fixture *piRemovalLifecycleFixture) {
				t.Helper()
				writePiPackageSettings(t, fixture.selectedSettings, []string{"npm:unrelated@9"})
			},
			assertRetained: true,
		},
		{
			name:       "settings-symlink",
			sourceKind: "npm",
			source:     func(string) string { return "npm:pi-tools@1.2.3" },
			effect: func(t *testing.T, fixture *piRemovalLifecycleFixture) {
				t.Helper()
				if err := os.RemoveAll(fixture.artifactPath); err != nil {
					t.Fatal(err)
				}
				decoy := filepath.Join(fixture.root, "decoy-settings.json")
				writePiPackageSettings(t, decoy, []string{"npm:unrelated@9"})
				if err := os.Remove(fixture.selectedSettings); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(decoy, fixture.selectedSettings); err != nil {
					t.Fatal(err)
				}
			},
			repair: func(t *testing.T, fixture *piRemovalLifecycleFixture) {
				t.Helper()
				if err := os.Remove(fixture.selectedSettings); err != nil {
					t.Fatal(err)
				}
				writePiPackageSettings(t, fixture.selectedSettings, []string{"npm:unrelated@9"})
			},
			assertRetained: true,
		},
		{
			name:       "absent-local-source-created",
			sourceKind: "local",
			source:     func(root string) string { return filepath.Join(root, "local-pi-tools") },
			beforeRemoval: func(t *testing.T, fixture *piRemovalLifecycleFixture) {
				t.Helper()
				if err := os.RemoveAll(filepath.Dir(fixture.localCanary)); err != nil {
					t.Fatal(err)
				}
			},
			effect: func(t *testing.T, fixture *piRemovalLifecycleFixture) {
				t.Helper()
				writePiPackageSettings(t, fixture.selectedSettings, []string{"npm:unrelated@9"})
				if err := os.MkdirAll(filepath.Dir(fixture.localCanary), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fixture.localCanary, []byte("created\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			repair: func(t *testing.T, fixture *piRemovalLifecycleFixture) {
				t.Helper()
				if err := os.RemoveAll(filepath.Dir(fixture.localCanary)); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, test := range tests {
			t.Run(string(scope)+"/"+test.name, func(t *testing.T) {
				fixture := newPiRemovalLifecycleFixture(
					t,
					scope,
					test.sourceKind,
					test.source,
				)
				fixture.install(t)
				if test.beforeRemoval != nil {
					test.beforeRemoval(t, fixture)
				}
				fixture.removeDeclaration(t)

				exitCode, stdout, stderr := fixture.runApplyWithRunner(
					t,
					func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
						fixture.assertRequest(t, request, "remove")
						test.effect(t, fixture)
						return subprocess.CommandResult{
							Started:     true,
							HasExitCode: true,
							ExitCode:    0,
						}
					},
				)
				assertPiRemovalDidNotConverge(t, exitCode, stdout, stderr)
				fixture.assertClaimCount(t, 1)
				fixture.assertPendingRemovalCount(t, 1)

				test.repair(t, fixture)
				exitCode, stdout, stderr = fixture.runApplyWithRunner(
					t,
					func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
						t.Fatal("repaired settlement reinvoked Pi")
						return subprocess.CommandResult{}
					},
				)
				if exitCode != 0 || stderr != "" {
					t.Fatalf(
						"settlement apply exitCode=%d stdout=%q stderr=%q",
						exitCode,
						stdout,
						stderr,
					)
				}
				fixture.assertClaimCount(t, 0)
				fixture.assertPendingRemovalCount(t, 0)
				if test.assertRetained {
					fixture.assertRetainedBoundaries(t)
				}
			})
		}
	}
}

func assertPiRemovalDidNotConverge(
	t *testing.T,
	exitCode int,
	stdout string,
	stderr string,
) {
	t.Helper()
	if exitCode != 1 || stderr != "" {
		t.Fatalf(
			"partial apply exitCode=%d stdout=%q stderr=%q, want JSON failure",
			exitCode,
			stdout,
			stderr,
		)
	}
	if !strings.Contains(stdout, "postcondition") &&
		!strings.Contains(stdout, "observation_") {
		t.Fatalf("partial apply stdout = %q, want postcondition failure", stdout)
	}
}
