package apply

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestNativeApplyRetiresAlreadyAbsentCarrierClaim(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			storage := t.TempDir()
			t.Setenv("HOME", filepath.Join(storage, "home"))
			t.Setenv("USERPROFILE", filepath.Join(storage, "home"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(storage, "config"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(storage, "state"))
			t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(storage, "pi"))
			t.Setenv("CODEX_HOME", filepath.Join(storage, "codex"))
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(storage, "claude"))

			root, manifestPath, lockfilePath, missing, previous, _ := writeApplyPiPackageCarrierCommandFixtureForScope(t, scope)
			claim := seedApplyCarrierClaim(t, root, manifestPath, previous, scope)
			writeApplyFile(t, manifestPath, "version=1\ntargets=['pi']\n[[host_config]]\nid='preferences'\npath='settings.json'\nsource='./preferences.json'\nownership='keys'\n")
			writeApplyFile(t, filepath.Join(root, "preferences.json"), `{"theme":"native-retirement"}`)
			if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
				t.Fatal(err)
			}

			input := CommandInput{ManifestPath: manifestPath, LockfilePath: lockfilePath, RelationObservations: &missing}
			prepared, err := PlanWrite(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			if !prepared.Reconciliation.ContainsHostConfig() {
				t.Fatal("fixture did not select an actual native settings operation")
			}
			absences := prepared.Reconciliation.CarrierAbsences()
			if len(absences) != 1 || absences[0].Decision() != carrierabsence.DecisionRetireAlreadyAbsent || !absences[0].Claim().ExactEqual(claim) {
				t.Fatalf("carrier absences = %#v, want the exact already-absent claim", absences)
			}

			if _, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{PlanWasDisclosed: true}); err != nil {
				t.Fatal(err)
			}

			retry, err := PlanWrite(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			if len(retry.Reconciliation.CarrierAbsences()) != 0 {
				t.Fatalf("retired claim was planned again: %#v", retry.Reconciliation.CarrierAbsences())
			}
			if aggregates := retry.Reconciliation.Aggregates(); len(aggregates) != 1 || aggregates[0].Kind() != reconcile.AggregateNoOp {
				t.Fatalf("native settings did not converge: %#v", aggregates)
			}
			if scope != target.ScopeProject {
				return
			}

			current := retry.lifecycle.planned.assessment.CurrentState
			withClaim, err := current.WithManagedCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
			if err != nil {
				t.Fatal(err)
			}
			content, err := retry.lifecycle.planned.nativePlan.stateCodec.Encode(withClaim)
			if err != nil {
				t.Fatal(err)
			}
			writeApplyFile(t, retry.StatefilePath, string(content))

			noOpCore, err := PlanWrite(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			if aggregates := noOpCore.Reconciliation.Aggregates(); len(aggregates) != 1 || aggregates[0].Kind() != reconcile.AggregateNoOp {
				t.Fatalf("expected a native no-op core: %#v", aggregates)
			}
			if _, err := ExecuteWithOptions(t.Context(), noOpCore, ExecuteOptions{PlanWasDisclosed: true}); err != nil {
				t.Fatal(err)
			}
			settled, err := PlanWrite(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			if len(settled.Reconciliation.CarrierAbsences()) != 0 || len(settled.lifecycle.planned.assessment.CurrentState.PendingCarrierRemovals()) != 0 {
				t.Fatal("native no-op core left carrier retirement or a manufactured pending attempt")
			}
		})
	}
}

func TestProjectCarrierRetirementModeFollowsItsCommitOwner(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, owner := range []projectCarrierRetirementOwner{projectCarrierRetirementCore, projectCarrierRetirementContinuation} {
			fixture := newWorkflowFixture(t, scope)
			action := alreadyAbsentWorkflowAction(t, fixture)
			facts, err := applyCarrierScheduleFactsForOwner([]carrierabsence.Action{action}, owner)
			if err != nil {
				t.Fatal(err)
			}
			deferred := scope == target.ScopeProject && owner == projectCarrierRetirementContinuation
			if (facts[0].mode == applyCarrierScheduleRetireProject) != deferred || facts[0].work.RetiresProject != deferred {
				t.Fatal("compiled retirement route and reservation disagree with the actual commit owner")
			}
			plan, err := carrierRemovalStatefileEffectPlanForWorks(carrierScheduleWorks(facts))
			if err != nil {
				t.Fatal(err)
			}
			if plan.empty() == deferred {
				t.Fatal("statefile reservation does not cover the selected retirement route")
			}
			work := operationplan.ApplyWork{CarrierRemovals: carrierScheduleWorks(facts)}
			if work.MayReclassifyRelationOrder() {
				t.Fatal("metadata-only retirement was treated as a host membership change")
			}
			mustCompileSyntheticApplySchedule(t, applyScheduleInput{carrierRemovals: facts})
		}
	}
	if _, err := applyCarrierScheduleFactsForOwner(nil, 0); err == nil {
		t.Fatal("missing project retirement owner was accepted")
	}
}

func TestProjectCarrierRetirementRetainsTruthfulStateAcrossValidationFailure(t *testing.T) {
	for _, phase := range []string{"before commit", "after commit"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newWorkflowFixture(t, target.ScopeProject)
			fixture.action = alreadyAbsentWorkflowAction(t, fixture)
			input := fixture.input(t)
			facts, err := applyCarrierScheduleFactsForOwner(input.Actions, projectCarrierRetirementContinuation)
			if err != nil {
				t.Fatal(err)
			}
			plan := mustCompileSyntheticApplySchedule(t, applyScheduleInput{carrierRemovals: facts}).continuation
			reserve := input.ReserveStatefileAuthority
			failure := errors.New("injected retirement validation failure")
			failOn := 1
			if phase == "after commit" {
				failOn = 2
			}
			input.ReserveStatefileAuthority = func(path string, plan statefileEffectPlan) (statefileEffectReservation, error) {
				reserved, err := reserve(path, plan)
				if err != nil {
					return nil, err
				}
				return retirementValidationReservation{statefileEffectReservation: reserved, failOn: failOn, failure: failure}, nil
			}

			result, err := runScheduledCarrierRemovals(t.Context(), input, plan, plan)
			if !errors.Is(err, failure) {
				t.Fatalf("retirement error = %v", err)
			}
			committed := phase == "after commit"
			if (len(result.State.ManagedCarrierClaims()) == 0) != committed || (result.ActionCount == 1) != committed {
				t.Fatalf("committed=%t claims=%d actions=%d", committed, len(result.State.ManagedCarrierClaims()), result.ActionCount)
			}
			if !fixture.persistedState(t).Equal(result.State) || len(result.State.PendingCarrierRemovals()) != 0 || fixture.executorCalls != 0 {
				t.Fatal("retirement lost its durable state or manufactured a host attempt")
			}
			if !committed {
				input.ReserveStatefileAuthority = reserve
				retry, err := runScheduledCarrierRemovals(t.Context(), input, plan, plan)
				if err != nil || retry.ActionCount != 1 || len(retry.State.ManagedCarrierClaims()) != 0 {
					t.Fatalf("metadata retry failed: actions=%d: %v", retry.ActionCount, err)
				}
			}
		})
	}
}

func alreadyAbsentWorkflowAction(t *testing.T, fixture *workflowFixture) carrierabsence.Action {
	t.Helper()
	occupancy, err := durablecarrier.NewCarrierOccupancy(fixture.claim.Identity().Carrier(), []durablecarrier.ManagedCarrierClaim{fixture.claim})
	if err != nil {
		t.Fatal(err)
	}
	key, err := observerelation.NewCorrelationKey(fixture.claim.Identity().RelationSubject(), fixture.expected)
	if err != nil {
		t.Fatal(err)
	}
	action, err := carrierabsence.NewAction(carrierabsence.ActionInput{
		Claim: fixture.claim, Desired: carrierabsence.DesiredAbsent, Occupancy: occupancy,
		Observation: observerelation.Correlation{Key: key, Result: missingCorrelation(t, fixture.expected)},
		Route:       fixture.action.RouteAdmission(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !action.StateOnly() {
		t.Fatal("fixture did not establish an already-absent carrier action")
	}
	return action
}

type retirementValidationReservation struct {
	statefileEffectReservation
	failOn  int
	failure error
}

func (reservation retirementValidationReservation) Bind(ctx context.Context) (boundStatefileEffectAuthority, error) {
	bound, err := reservation.statefileEffectReservation.Bind(ctx)
	if err != nil {
		return nil, err
	}
	return &retirementValidationAuthority{boundStatefileEffectAuthority: bound, failOn: reservation.failOn, failure: reservation.failure}, nil
}

type retirementValidationAuthority struct {
	boundStatefileEffectAuthority
	failOn      int
	validations int
	failure     error
}

func (authority *retirementValidationAuthority) Validate(ctx context.Context) error {
	authority.validations++
	if authority.validations == authority.failOn {
		return authority.failure
	}
	return authority.boundStatefileEffectAuthority.Validate(ctx)
}
