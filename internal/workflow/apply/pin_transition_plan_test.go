package apply

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestPiPinPlanBindsResumeEvidenceAndFitsReservedDemand(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newPiPinApplyFixture(t, scope)
			var prior string
			for _, phase := range []string{"initial", "reserved_old", "reserved_new"} {
				switch phase {
				case "reserved_old":
					fixture.reserve(t)
				case "reserved_new":
					writeApplyFile(t, fixture.settings, fmt.Sprintf("{\"packages\":[%q]}", fixture.after))
				}
				prepared, err := PlanWrite(t.Context(), fixture.input())
				if err != nil {
					t.Fatal(err)
				}
				planned := prepared.lifecycle.planned
				plan, err := stateDirEffectPlanFor(planned, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := requireLegacyApplyDemandDominance(plan.schedule.full, plan.demand); err != nil {
					t.Fatal(err)
				}
				validations, commits := 6, 3
				if scope == target.ScopeGlobal {
					validations, commits = 9, 1
				}
				if plan.demand.DescendantValidations() != validations || plan.demand.DescendantFileCommits() != commits {
					t.Fatalf("%s demand=%d/%d, want %d/%d", phase, plan.demand.DescendantValidations(), plan.demand.DescendantFileCommits(), validations, commits)
				}
				fingerprint, err := applyOperationFingerprint(planned, reconcile.ContextApply)
				if err != nil {
					t.Fatal(err)
				}
				legacy, err := legacyApplyOperationFingerprint(planned, reconcile.ContextApply)
				if err != nil || !fingerprint.Equal(legacy) {
					t.Fatalf("%s fingerprint parity: %v", phase, err)
				}
				facts, err := marshalProviderStableFingerprintProjection(relationFingerprintRows(planned.assessment.Reconciliation.Relations()))
				if err != nil {
					t.Fatal(err)
				}
				if string(facts) == prior {
					t.Fatalf("%s lost changed resume or observation facts", phase)
				}
				prior = string(facts)
				if err := prepared.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestPiPinTransitionSurvivesProviderReplan(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newPiPinApplyFixture(t, scope)
			content, err := os.ReadFile(fixture.manifest)
			if err != nil {
				t.Fatal(err)
			}
			writeApplyFile(t, fixture.manifest, string(content)+`
[[extension]]
id = "pi-mcp-adapter-project"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }

[[mcp_server]]
name = "context7"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
`)
			if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: fixture.manifest}); err != nil {
				t.Fatal(err)
			}
			prepared, err := PlanWrite(t.Context(), fixture.input())
			if err != nil {
				t.Fatal(err)
			}
			var sources []string
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Clock: fixedApplyHostRouteClock, Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
				if request.Command != "pi" || len(request.Args) < 2 || request.Args[0] != "install" {
					t.Fatalf("unexpected command: %+v", request)
				}
				source := request.Args[1]
				sources = append(sources, source)
				switch source {
				case piProviderSource:
					writePiProviderPackage(t, fixture.root, "2.15.0")
					settings := fmt.Sprintf("{\"packages\":[%q]}", piProviderSource)
					if scope == target.ScopeProject {
						settings = fmt.Sprintf("{\"packages\":[%q,%q]}", fixture.before, piProviderSource)
					}
					writeApplyFile(t, filepath.Join(fixture.root, ".pi", "settings.json"), settings)
				case fixture.after:
					settings := fmt.Sprintf("{\"packages\":[%q]}", fixture.after)
					if scope == target.ScopeProject {
						settings = fmt.Sprintf("{\"packages\":[%q,%q]}", fixture.after, piProviderSource)
					}
					writeApplyFile(t, fixture.settings, settings)
				default:
					t.Fatalf("unexpected source: %s", source)
				}
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			}})
			result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{HostRouteExecutor: executor})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(sources, []string{piProviderSource, fixture.after}) || len(result.HostRouteAttempts) != 2 {
				t.Fatalf("sources/attempts=%v/%d", sources, len(result.HostRouteAttempts))
			}
			for _, claim := range loadCarrierClaimsForScope(t, fixture.root, fixture.manifest, scope) {
				if claim.Identity().RelationSubject() == fixture.claim.Identity().RelationSubject() {
					if !claim.MatchesLockedRecord(fixture.locked.Locked.Subjects()[0]) {
						t.Fatal("provider replan lost completed pin management")
					}
					return
				}
			}
			t.Fatal("provider replan lost pin management")
		})
	}
}
