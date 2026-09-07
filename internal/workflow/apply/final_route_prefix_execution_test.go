package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestScheduledFinalRoutePrefixPersistsRejectionBeforeAcceptedSiblingHost(t *testing.T) {
	planned, plan := finalRoutePrefixMixedPlan(t)
	paths := planned.context.Paths
	invocations := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			invocations++
			persisted := loadApplyStatefile(t, paths.StatefilePath).HostRouteAttempts()
			if len(persisted) != 1 ||
				persisted[0].ResultClass() != durableattempt.HostRouteResultBlockedPreflight {
				t.Fatalf("persisted attempts before host = %#v, want one preflight rejection", persisted)
			}
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 1}
		},
	})
	options := applyDelegateRunOptions(t, paths, runOptions{HostRouteExecutor: executor})

	_, _, records, err := runScheduledHostRoutesAndPersistAttemptRecords(
		t.Context(),
		paths,
		planned.context.Lockfile,
		planned.assessment.StatePath,
		planned.assessment.CurrentState,
		planned.assessment.Owner,
		planned.assessment.GlobalCarrierClaims,
		options,
		plan,
		plan,
	)
	if err == nil {
		t.Fatal("scheduled mixed prefix returned no host-route failure")
	}
	if invocations != 1 {
		t.Fatalf(
			"host invocations = %d, want accepted sibling after rejection persistence: %v",
			invocations,
			err,
		)
	}
	if len(records) != 2 {
		t.Fatalf("host route records = %#v, want rejection and accepted-sibling attempt", records)
	}
}

func TestScheduledFinalRoutePrefixStopsBeforeHostWhenRejectionPublicationFails(t *testing.T) {
	planned, plan := finalRoutePrefixMixedPlan(t)
	paths := planned.context.Paths
	bound := &recordingBoundStatefileAuthority{}
	authority, err := newStatefileEffectAuthorityFromReservation(
		statefileEffectPlan{validations: 1, fileCommits: 1},
		&recordingStatefileReservation{bound: bound},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.Close() })
	invocations := 0
	options := applyDelegateRunOptions(t, paths, runOptions{
		statefileAuthority: authority,
		HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				invocations++
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	})

	_, _, records, err := runScheduledHostRoutesAndPersistAttemptRecords(
		t.Context(),
		paths,
		planned.context.Lockfile,
		planned.assessment.StatePath,
		planned.assessment.CurrentState,
		planned.assessment.Owner,
		planned.assessment.GlobalCarrierClaims,
		options,
		plan,
		plan,
	)
	if err == nil || !strings.Contains(err.Error(), "statefile effect entry authority is unavailable") {
		t.Fatalf("scheduled prefix error = %v, want rejection-publication failure", err)
	}
	if invocations != 0 {
		t.Fatalf("host invocations = %d, want none before prefix publication succeeds", invocations)
	}
	if len(records) != 1 || records[0].ResultClass() != durableattempt.HostRouteResultBlockedPreflight {
		t.Fatalf("in-memory records = %#v, want one bounded preflight rejection", records)
	}
}

func TestScheduledFinalRoutePrefixStopsBeforeHostOnDeclarationDrift(t *testing.T) {
	planned, plan := finalRoutePrefixMixedPlan(t)
	paths := planned.context.Paths
	original, err := os.ReadFile(paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	invocations := 0
	options := applyDelegateRunOptions(t, paths, runOptions{
		HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				invocations++
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	})
	options.markExecutionAttempted = func() {
		if err := os.WriteFile(paths.ManifestPath, append(original, []byte("\n# changed\n")...), 0o600); err != nil {
			t.Fatalf("change manifest after preflight persistence: %v", err)
		}
	}

	_, _, records, err := runScheduledHostRoutesAndPersistAttemptRecords(
		t.Context(),
		paths,
		planned.context.Lockfile,
		planned.assessment.StatePath,
		planned.assessment.CurrentState,
		planned.assessment.Owner,
		planned.assessment.GlobalCarrierClaims,
		options,
		plan,
		plan,
	)
	reason, classified := mutation.ReasonCodeOf(err)
	if !classified || reason != mutation.ReasonStaleSnapshot {
		t.Fatalf("scheduled declaration-drift error = %v, want stale snapshot", err)
	}
	if invocations != 0 {
		t.Fatalf("host invocations = %d, want none after declaration drift", invocations)
	}
	if len(records) != 1 || records[0].ResultClass() != durableattempt.HostRouteResultBlockedPreflight {
		t.Fatalf("in-memory records = %#v, want one persisted preflight rejection", records)
	}
}

func TestScheduledFinalRoutePrefixStopsBeforeHostOnCancellation(t *testing.T) {
	planned, plan := finalRoutePrefixMixedPlan(t)
	invocations := 0
	options := applyDelegateRunOptions(t, planned.context.Paths, runOptions{
		HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{
			Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				invocations++
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			},
		}),
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, _, err := runScheduledHostRoutesAndPersistAttemptRecords(
		ctx,
		planned.context.Paths,
		planned.context.Lockfile,
		planned.assessment.StatePath,
		planned.assessment.CurrentState,
		planned.assessment.Owner,
		planned.assessment.GlobalCarrierClaims,
		options,
		plan,
		plan,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scheduled canceled prefix error = %v, want context.Canceled", err)
	}
	if invocations != 0 {
		t.Fatalf("host invocations = %d, want none after prefix cancellation", invocations)
	}
}

func TestFinalRoutePrefixPhaseContinuationRequiresEarlierForwardWork(t *testing.T) {
	if applyCarrierRemovalScheduleHasForward([]applyCarrierScheduleFact{{
		mode: applyCarrierScheduleNoOp,
	}}) {
		t.Fatal("state-only carrier retirement established a forward phase")
	}
	if !applyCarrierRemovalScheduleHasForward([]applyCarrierScheduleFact{{
		mode: applyCarrierScheduleVerifyPending,
	}}) {
		t.Fatal("pending-removal verification did not establish a forward phase")
	}

	_, base := finalRoutePrefixMixedPlan(t)
	input := applyScheduleInput{
		hasGlobalRetirement: true,
		finalRoutes:         base.finalRoutePlan.routes,
	}
	plan := compileTestApplyContinuationPlan(t, input, false)
	if !plan.finalRoutePlan.phaseEstablished {
		t.Fatal("global retirement did not bind final routes to the established forward phase")
	}
	if plan.finalRoutePlan.statefileInitiallyBound {
		t.Fatal("unbound final-route statefile authority was reported as initially bound")
	}
	boundPlan := compileTestApplyContinuationPlan(t, input, true)
	if !boundPlan.finalRoutePlan.statefileInitiallyBound {
		t.Fatal("bound final-route statefile authority was not retained in plan identity")
	}
}

func TestFinalRoutePrefixCursorRejectsUnderConsumption(t *testing.T) {
	_, plan := finalRoutePrefixMixedPlan(t)
	execution, err := newApplyFinalRoutePrefixExecution(plan, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := consumeApplyRoutePreflight(execution, plan.finalRoutePlan.routes[0]); err != nil {
		t.Fatal(err)
	}
	if err := execution.finish(nil); err == nil {
		t.Fatal("under-consumed final-route prefix finished successfully")
	}
}

func finalRoutePrefixMixedPlan(t *testing.T) (commandPlan, applyContinuationPlan) {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	writeApplyFile(t, manifestPath, "version = 1\ntargets = [\"claude-code\"]\n")
	environment := desiredtest.Environment(t, desired.Spec{
		Targets:  []target.Target{target.TargetClaudeCode},
		Defaults: desiredtest.Defaults(t, target.ScopeProject, skill.InstallModeCopy),
		Extensions: []extension.Extension{
			writeAheadClaudeExtension(t, "rejected-plugin", "rejected@market", target.ScopeProject),
			writeAheadClaudeExtension(t, "accepted-plugin", "accepted@market", target.ScopeGlobal),
		},
	})
	locked, err := lockbuild.BuildWithOptions(t.Context(), environment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatal(err)
	}
	writeApplyLockfile(t, lockfilePath, locked)
	missing := applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	observations := applyClaudeObservationBatchForLocked(t, locked, missing)
	prepared, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"claude-code"},
		RelationObservations: &observations,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prepared.Close() })
	planned := prepared.lifecycle.planned
	input, err := applyScheduleInputFor(
		planned,
		nil,
		planned.assessment.CurrentState,
		operationplan.EffectSequence(),
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.finalRoutes) != 2 {
		t.Fatalf("final routes = %#v, want two", input.finalRoutes)
	}
	rejected, err := applyRouteScheduleFacts(
		input.finalRoutes[0].ref,
		planned.assessment.CurrentState,
		[]reconcile.RelationAction{input.finalRoutes[0].action},
		planned.context.Lockfile,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rejected) != 1 || !rejected[0].preflight.rejected() {
		t.Fatalf("rejected route facts = %#v, want one rejection", rejected)
	}
	input.finalRoutes[0].preflight = rejected[0].preflight
	return planned, compileTestApplyContinuationPlan(t, input, false)
}

func compileTestApplyContinuationPlan(
	t *testing.T,
	input applyScheduleInput,
	initiallyBound bool,
) applyContinuationPlan {
	t.Helper()
	var builder operationplan.EffectStructureBuilder
	statefile := applyStatefileSchedule{builder: &builder, bound: initiallyBound}
	plan, err := compileApplyContinuationPlan(&builder, &statefile, input)
	if err != nil {
		t.Fatal(err)
	}
	if statefile.err != nil {
		t.Fatal(statefile.err)
	}
	return plan
}
