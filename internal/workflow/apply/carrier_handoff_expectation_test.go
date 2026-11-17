package apply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/desired"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/execute/nativeoperation"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func TestProspectiveCarrierHandoffCannotExecuteBeforeAnyEffect(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	absence := alreadyAbsentWorkflowAction(t, fixture)
	environment := applyCarrierEnvironment(t, "context7", desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@next")
	locked, err := lockbuild.BuildWithOptions(t.Context(), environment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatal(err)
	}
	record := locked.Locked.Subjects()[0]
	action := applyClaudePluginCarrierRelationAction(t, record, snapshottest.DelegatedRelation(t, record), observeclaudeplugin.InventorySpec{
		Availability: relationobserve.InventorySupported, Freshness: relationobserve.EvidenceFresh,
	})
	old, _ := absence.Observation()
	key, err := relationobserve.NewCorrelationKey(action.Subject(), action.ExpectedRelation())
	if err != nil {
		t.Fatal(err)
	}
	current, _ := action.Correlation()
	batch, err := relationobserve.NewBatch(relationobserve.BatchSpec{Correlations: []relationobserve.Correlation{
		old, {Key: key, Result: current},
	}})
	if err != nil {
		t.Fatal(err)
	}
	handoffs, err := reconcile.NewCarrierHandoffs(reconcile.CarrierHandoffsInput{
		CurrentOwner: fixture.claim.Owner(), Claims: []durablecarrier.ManagedCarrierClaim{fixture.claim},
		Absences: []carrierabsence.Action{absence}, Acquisitions: []reconcile.RelationAction{action}, Observations: batch,
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, Relations: []reconcile.RelationAction{action}})
	if err != nil {
		t.Fatal(err)
	}
	expectation, err := reconcile.NewCarrierHandoffExpectation(handoffs, after)
	if err != nil {
		t.Fatal(err)
	}
	prospective := expectation.After()

	for _, native := range []*nativeoperation.NativeApplyInput{nil, {}} {
		assessment := readiness.Assessment{
			CurrentState: fixture.current, GlobalCarrierClaims: fixture.globalClaims, Reconciliation: prospective,
		}
		result, err := runWithOptions(context.Background(), daempaths.Paths{}, desired.Environment{}, lock.File{},
			targetselection.Selection{}, assessment, runOptions{nativeInput: native, markExecutionAttempted: func() {
				t.Fatal("prospective remainder crossed an effect boundary")
			}})
		if !errors.Is(err, errProspectiveCarrierHandoff) || len(result.HostRouteAttempts) != 0 || len(result.DelegateAttempts) != 0 {
			t.Fatal("direct/native execution did not reject the prospective role before effects", err)
		}
	}
	result, err := ExecuteWithOptions(t.Context(), preparedWriteForTestResult(CommandResult{Reconciliation: prospective}), ExecuteOptions{})
	if !errors.Is(err, errProspectiveCarrierHandoff) || result.ExecutionAttempted || len(result.HostRouteAttempts) != 0 || len(result.DelegateAttempts) != 0 {
		t.Fatal("prepared execution did not reject the prospective role before effects", err)
	}
}

func TestExecuteCarrierSourceHandoffSettlesOldClaimBeforeNewInstall(t *testing.T) {
	for _, fault := range []string{"success", "removal-error", "cancellation", "stale-replan"} {
		t.Run(fault, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			root, manifest, lockfile, _, previous, _ := writeApplyClaudePluginCarrierCommandFixture(t)
			seedApplyCarrierClaim(t, root, manifest, previous, target.ScopeProject)
			configRoot := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
			inventory := filepath.Join(configRoot, "plugins", "installed_plugins.json")
			writeApplyFile(t, inventory, fmt.Sprintf(`{"version":2,"plugins":{"context7@market":[{"scope":"project","projectPath":%q}]}}`, root))
			writeApplyFile(t, manifest, `version = 1
targets = ["claude-code"]
[[extension]]
id = "context7"
carrier = "claude-code-plugin"
scope = "project"
source = { marketplace = "context7@next" }
`)
			if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
				t.Fatal(err)
			}
			finalManifest, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest, LockfilePath: lockfile})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			calls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
				calls++
				if slices.Contains(request.Args, "context7@market") {
					if fault == "removal-error" && calls == 1 {
						return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 23}
					}
					writeApplyFile(t, inventory, `{"version":2,"plugins":{}}`)
					if fault == "stale-replan" && calls == 1 {
						writeApplyFile(t, manifest, string(finalManifest)+"\n# changed during removal\n")
					}
					if fault == "cancellation" && calls == 1 {
						cancel()
					}
				} else if slices.Contains(request.Args, "context7@next") {
					state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
					if len(state.ManagedCarrierClaims()) != 0 || len(state.PendingCarrierRemovals()) != 0 || len(state.PendingCarrierInstalls()) != 1 {
						t.Fatal("new install started before old claim/removal debt settled and new intent persisted")
					}
					writeApplyFile(t, inventory, fmt.Sprintf(`{"version":2,"plugins":{"context7@next":[{"scope":"project","projectPath":%q}]}}`, root))
				} else {
					t.Fatalf("unexpected host command: %s %v", request.Command, request.Args)
				}
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			}})
			result, err := ExecuteWithOptions(ctx, prepared, ExecuteOptions{HostRouteExecutor: executor})
			attempts := len(result.HostRouteAttempts)
			if fault != "success" {
				if err == nil || calls != 1 || !result.UncompensatedEffectsAttempted {
					t.Fatal("partial retirement was reported complete or continued into acquisition", err)
				}
				state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
				if fault == "removal-error" && (len(state.ManagedCarrierClaims()) != 1 || len(state.PendingCarrierRemovals()) != 1) {
					t.Fatal("uncertain removal dropped its old claim or pending intent")
				}
				if fault == "stale-replan" {
					if result.CarrierHandoffProgress == nil || len(result.CarrierHandoffProgress.RetiredClaims) != 1 || result.CarrierHandoffProgress.Requalified || result.CarrierHandoffProgress.Complete {
						t.Fatal("stale replan hid a completed retirement or claimed completion")
					}
					writeApplyFile(t, manifest, string(finalManifest))
				}
				retry, planErr := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest, LockfilePath: lockfile})
				if planErr != nil {
					t.Fatal(planErr)
				}
				defer retry.Close()
				result, err = ExecuteWithOptions(t.Context(), retry, ExecuteOptions{HostRouteExecutor: executor})
				attempts += len(result.HostRouteAttempts)
			}
			if err != nil {
				t.Fatal(err)
			}
			wantCalls := 2
			if fault == "removal-error" {
				wantCalls = 3
			}
			if calls != wantCalls || fault != "cancellation" && attempts != wantCalls || attempts == 0 {
				t.Fatalf("calls=%d attempts=%d, want remove then install across same-manifest retries", calls, attempts)
			}
			state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
			if len(state.ManagedCarrierClaims()) != 1 || !state.ManagedCarrierClaims()[0].Identity().ExactEqual(prepared.Reconciliation.Relations()[0].CarrierIdentity()) ||
				len(state.PendingCarrierRemovals()) != 0 || len(state.PendingCarrierInstalls()) != 0 {
				t.Fatalf("source handoff did not converge: claims=%#v pending-removals=%#v pending-installs=%#v", state.ManagedCarrierClaims(), state.PendingCarrierRemovals(), state.PendingCarrierInstalls())
			}
		})
	}
}

func TestPlanCarrierHandoffBindsOneFinalLockWithoutPublishingProjectedState(t *testing.T) {
	root, manifest, lockfile, _, previous, _ := writeApplyClaudePluginCarrierCommandFixture(t)
	claim := seedApplyCarrierClaim(t, root, manifest, previous, target.ScopeProject)
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	writeApplyFile(t, filepath.Join(configRoot, "plugins", "installed_plugins.json"), fmt.Sprintf(`{"version":2,"plugins":{"context7@market":[{"scope":"project","projectPath":%q}]}}`, root))
	writeApplyFile(t, manifest, `version = 1
targets = ["claude-code"]
[[extension]]
id = "context7"
carrier = "claude-code-plugin"
scope = "project"
source = { marketplace = "context7@next" }
`)
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
		t.Fatal(err)
	}
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest, LockfilePath: lockfile})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Error(err)
		}
	}()
	expectation, present := prepared.Reconciliation.CarrierHandoff()
	if !present || prepared.Reconciliation.IsProspective() || !expectation.After().IsProspective() {
		t.Fatal("planner did not distinguish actual assessment and conditional remainder")
	}
	absences := prepared.Reconciliation.CarrierAbsences()
	if len(absences) != 1 || absences[0].Desired() != carrierabsence.DesiredReplaced || absences[0].Decision() != carrierabsence.DecisionRemove {
		t.Fatal("same final lock did not classify source replacement separately from deletion")
	}
	actual := prepared.lifecycle.planned.assessment
	if claims := actual.CurrentState.ManagedCarrierClaims(); len(claims) != 1 || !claims[0].ExactEqual(claim) ||
		len(expectation.Handoffs().ProspectiveClaims()) != 0 || len(expectation.After().CarrierAbsences()) != 0 {
		t.Fatal("prospective fact subtraction changed current state or kept an old obligation in the remainder")
	}
	if len(expectation.After().Relations()) != 1 || !expectation.After().Relations()[0].InvokesHostRoute() {
		t.Fatal("conditional acquisition was not planned from the final lock")
	}
	for _, invalid := range []reconcile.ResultInput{
		{Context: reconcile.ContextApply},
		{Context: reconcile.ContextApply, CarrierAbsences: absences},
		{Context: reconcile.ContextInspect, Relations: prepared.Reconciliation.Relations(), CarrierAbsences: absences},
	} {
		result, err := reconcile.NewResult(invalid)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := result.WithCarrierHandoff(expectation); err == nil {
			t.Fatal("current handoff binding accepted a missing member or a mismatched operation context")
		}
	}
}
