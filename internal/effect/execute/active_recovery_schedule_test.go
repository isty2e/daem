package execute

import (
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestActiveRecoveryOwnerStepSequencesRemainOrdered(t *testing.T) {
	t.Parallel()

	assertSteps := func(name string, got, want []activeRecoveryStep) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Fatalf("%s steps = %#v, want %#v", name, got, want)
		}
	}
	assertSteps("standalone prefix", activeRecoveryStandalonePrefixSteps(), []activeRecoveryStep{
		{id: "active-recovery/bind-journal-authority", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/bind-statefile-authority", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/establish-journal-basis", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/validate-journal-basis", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/bind-removal-authority", kind: operationplan.EffectStepObservation},
	})
	assertSteps("common preparation", activeRecoveryCommonPreparationSteps(), []activeRecoveryStep{
		{id: "active-recovery/prepare-mutation-authority", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/reload-before-effects", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/bind-reloaded-removal-authority", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/prepare-journal-retirement", kind: operationplan.EffectStepPersistence},
	})
	assertSteps("clean preparation", activeRecoveryCleanPreparationSteps(), []activeRecoveryStep{
		{id: "active-recovery/clean/reserve-semantic-validations", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/clean/conclude-rollback-stage-absent", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/clean/prepare-removal-cleanup", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/clean/begin-general-execution", kind: operationplan.EffectStepObservation},
	})
	assertSteps("finalize preparation", activeRecoveryFinalizePreparationSteps(), []activeRecoveryStep{
		{id: "active-recovery/finalize/reserve-semantic-validations", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/finalize/conclude-rollback-stage-absent", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/finalize/prepare-removal-cleanup", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/finalize/begin-general-execution", kind: operationplan.EffectStepObservation},
	})
	assertSteps("rollback preparation", activeRecoveryRollbackPreparationSteps(), []activeRecoveryStep{
		{id: "active-recovery/rollback/prepare-host-actions", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/rollback/reserve-semantic-validations", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/rollback/match-manifest-root", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/rollback/prepare-backups", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/rollback/prepare-forward-removals", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/rollback/stage-rollback", kind: operationplan.EffectStepPersistence},
	})
	assertSteps("standalone settlement", activeRecoveryOuterSettlementSteps(activeRecoveryScheduleInput{
		caller:              activeRecoveryCallerStandalone,
		hasBeforeRetirement: true,
	}), []activeRecoveryStep{
		{id: "active-recovery/outer/validate-project-before-retirement", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/outer/validate-visibility-before-retirement", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/outer/reload-after-effects", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/outer/before-retirement", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/retirement/validate-before-cleanup", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/retirement/bind-removal-authority", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/retirement/validate-clean-plan", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/retirement/prepare-tail", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/retirement/advance-basis", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/retirement/removal-cleanup", kind: operationplan.EffectStepCleanup},
		{id: "active-recovery/retirement/validate-journal", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/retirement/validate-semantics", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/retirement/execute-tail", kind: operationplan.EffectStepRetirement},
		{id: "active-recovery/outer/accept-visibility", kind: operationplan.EffectStepObservation},
		{id: "active-recovery/outer/validate-project-after-retirement", kind: operationplan.EffectStepObservation},
	})
}

func TestActiveRecoveryStructuresAreCompactAndHaveNoStateBarrierDemand(t *testing.T) {
	t.Parallel()

	inputs := []activeRecoveryScheduleInput{
		{
			classification:      recovery.ClassificationCleanBefore,
			caller:              activeRecoveryCallerStandalone,
			hasBeforeRetirement: true,
		},
		{
			classification: recovery.ClassificationCleanAfter,
			caller:         activeRecoveryCallerApplySettlement,
		},
		{
			classification:            recovery.ClassificationNeedsFinalize,
			hasClaimTransitions:       true,
			requiresOwnershipRegistry: true,
			caller:                    activeRecoveryCallerStandalone,
		},
		{
			classification:            recovery.ClassificationNeedsRollback,
			hostActionCount:           100_000,
			hasClaimTransitions:       true,
			requiresOwnershipRegistry: true,
			caller:                    activeRecoveryCallerApplySettlement,
		},
	}
	for _, input := range inputs {
		structure, err := compileActiveRecoveryStructure(input)
		if err != nil {
			t.Fatalf("compile active recovery structure %#v: %v", input, err)
		}
		demand, err := structure.LegacyDemand()
		if err != nil {
			t.Fatalf("project active recovery demand %#v: %v", input, err)
		}
		if demand != (operationplan.Demand{}) {
			t.Fatalf("active recovery demand %#v = %#v, want zero", input, demand)
		}
	}
}

func TestActiveRecoveryStandaloneCleanSuccessClosesOwnedAuthority(t *testing.T) {
	t.Parallel()

	input := activeRecoveryScheduleInput{
		classification:      recovery.ClassificationCleanBefore,
		caller:              activeRecoveryCallerStandalone,
		hasBeforeRetirement: true,
	}
	execution := mustActiveRecoveryExecution(t, input)
	runActiveRecoverySteps(t, execution, activeRecoveryStandalonePrefixSteps())
	runActiveRecoverySteps(t, execution, activeRecoveryCommonPreparationSteps())
	runActiveRecoverySteps(t, execution, activeRecoveryCleanPreparationSteps())
	runActiveRecoverySteps(t, execution, activeRecoveryOuterSettlementSteps(input))

	closeCalls := 0
	if err := execution.finish(nil, func() error {
		closeCalls++
		return nil
	}); err != nil {
		t.Fatalf("finish active recovery success: %v", err)
	}
	if closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", closeCalls)
	}
}

func TestActiveRecoveryOwnedFailurePreservesPrimaryAndCloseFailure(t *testing.T) {
	t.Parallel()

	input := activeRecoveryScheduleInput{
		classification: recovery.ClassificationCleanBefore,
		caller:         activeRecoveryCallerStandalone,
	}
	execution := mustActiveRecoveryExecution(t, input)
	primary := errors.New("reload failed")
	closeFailure := errors.New("close failed")
	steps := activeRecoveryStandalonePrefixSteps()
	for _, step := range steps[:2] {
		runActiveRecoveryStep(t, execution, step)
	}
	operationErr := execution.runTerminalStep(steps[2], func() error { return primary })
	if !errors.Is(operationErr, primary) {
		t.Fatalf("operation error = %v, want primary", operationErr)
	}
	result := execution.finish(operationErr, func() error { return closeFailure })
	if !errors.Is(result, primary) || !errors.Is(result, closeFailure) {
		t.Fatalf("finish error = %v, want primary and close failure", result)
	}
}

func TestActiveRecoveryBorrowedFinalizeFailureDoesNotCloseAuthority(t *testing.T) {
	t.Parallel()

	input := activeRecoveryScheduleInput{
		classification:            recovery.ClassificationNeedsFinalize,
		hasClaimTransitions:       true,
		requiresOwnershipRegistry: true,
		caller:                    activeRecoveryCallerApplySettlement,
	}
	execution := mustActiveRecoveryExecution(t, input)
	runActiveRecoverySteps(t, execution, activeRecoveryCommonPreparationSteps())
	runActiveRecoverySteps(t, execution, activeRecoveryFinalizePreparationSteps())

	primary := errors.New("claim finalization failed")
	claimStep := activeRecoveryStep{
		id:   "active-recovery/finalize/claims",
		kind: operationplan.EffectStepPersistence,
	}
	operationErr := execution.runTerminalStep(claimStep, func() error { return primary })
	closeCalls := 0
	result := execution.finish(operationErr, func() error {
		closeCalls++
		return nil
	})
	if !errors.Is(result, primary) {
		t.Fatalf("finish error = %v, want primary", result)
	}
	if closeCalls != 0 {
		t.Fatalf("borrowed authority close calls = %d, want 0", closeCalls)
	}
}

func TestActiveRecoveryRollbackHostSuccessConsumesExactVisitOrder(t *testing.T) {
	t.Parallel()

	input := activeRecoveryScheduleInput{
		classification:            recovery.ClassificationNeedsRollback,
		hostActionCount:           3,
		hasClaimTransitions:       true,
		requiresOwnershipRegistry: true,
		caller:                    activeRecoveryCallerApplySettlement,
	}
	execution := mustActiveRecoveryExecution(t, input)
	runActiveRecoverySteps(t, execution, activeRecoveryCommonPreparationSteps())
	runActiveRecoverySteps(t, execution, activeRecoveryRollbackPreparationSteps())
	runActiveRecoveryRollbackPostStageSteps(t, execution, true)

	expected := []activeRecoveryHostVisit{{index: 0}, {index: 2}, {index: 1}}
	if err := execution.beginHostBatch(expected); err != nil {
		t.Fatal(err)
	}
	for _, visit := range expected {
		if err := execution.visitHostAction(visit.index); err != nil {
			t.Fatalf("visit host action %d: %v", visit.index, err)
		}
	}
	if err := execution.settleHostBatch(nil); err != nil {
		t.Fatalf("settle host batch: %v", err)
	}
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/validate-after-host-actions",
		kind: operationplan.EffectStepObservation,
	})
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/claims",
		kind: operationplan.EffectStepPersistence,
	})
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/validate-after-claims",
		kind: operationplan.EffectStepObservation,
	})
	runActiveRecoveryStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/cleanup-after-success",
		kind: operationplan.EffectStepCleanup,
	})
	runActiveRecoverySteps(t, execution, activeRecoveryOuterSettlementSteps(input))
	if err := execution.finish(nil, nil); err != nil {
		t.Fatalf("finish rollback success: %v", err)
	}
}

func TestActiveRecoveryRollbackWithoutHostActionsUsesNoOpBatch(t *testing.T) {
	t.Parallel()

	input := activeRecoveryScheduleInput{
		classification: recovery.ClassificationNeedsRollback,
		caller:         activeRecoveryCallerApplySettlement,
	}
	execution := mustActiveRecoveryExecution(t, input)
	runActiveRecoverySteps(t, execution, activeRecoveryCommonPreparationSteps())
	runActiveRecoverySteps(t, execution, activeRecoveryRollbackPreparationSteps())
	runActiveRecoveryRollbackPostStageSteps(t, execution, false)
	if err := execution.beginHostBatch(nil); err != nil {
		t.Fatalf("begin empty host batch: %v", err)
	}
	if err := execution.settleHostBatch(nil); err != nil {
		t.Fatalf("settle empty host batch: %v", err)
	}
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/validate-after-host-actions",
		kind: operationplan.EffectStepObservation,
	})
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/claims",
		kind: operationplan.EffectStepNoOp,
	})
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/validate-after-claims",
		kind: operationplan.EffectStepObservation,
	})
	runActiveRecoveryStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/cleanup-after-success",
		kind: operationplan.EffectStepCleanup,
	})
	runActiveRecoverySteps(t, execution, activeRecoveryOuterSettlementSteps(input))
	if err := execution.finish(nil, nil); err != nil {
		t.Fatalf("finish empty rollback host batch: %v", err)
	}
}

func TestActiveRecoveryRollbackHostPreparationFailureConsumesCompensation(t *testing.T) {
	t.Parallel()

	input := activeRecoveryScheduleInput{
		classification:  recovery.ClassificationNeedsRollback,
		hostActionCount: 1,
		caller:          activeRecoveryCallerApplySettlement,
	}
	execution := mustActiveRecoveryExecution(t, input)
	runActiveRecoverySteps(t, execution, activeRecoveryCommonPreparationSteps())
	runActiveRecoverySteps(t, execution, activeRecoveryRollbackPreparationSteps())
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/prepare-removal-cleanup",
		kind: operationplan.EffectStepObservation,
	})
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/begin-general-execution",
		kind: operationplan.EffectStepObservation,
	})
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/rebind-ownership-registry",
		kind: operationplan.EffectStepNoOp,
	})

	primary := errors.New("prepare host execution failed")
	prepareErr := execution.runBranchingStep(
		activeRecoveryStep{
			id:   "active-recovery/rollback/prepare-host-execution",
			kind: operationplan.EffectStepObservation,
		},
		func() error { return primary },
	)
	if !errors.Is(prepareErr, primary) {
		t.Fatalf("prepare host execution error = %v, want primary", prepareErr)
	}
	prefix := "active-recovery/rollback/prepare-host-execution-failure"
	if err := execution.runContinuingOutcomeStep(
		activeRecoveryStep{id: prefix + "/restore", kind: operationplan.EffectStepCompensation},
		prefix+"/restore",
		func() error { return nil },
	); err != nil {
		t.Fatalf("consume host preparation rollback: %v", err)
	}
	if err := execution.runFailureCleanup(prefix, func() error { return nil }); err != nil {
		t.Fatalf("consume host preparation cleanup: %v", err)
	}
	if err := execution.finish(prepareErr, nil); !errors.Is(err, primary) {
		t.Fatalf("finish error = %v, want primary", err)
	}
}

func TestActiveRecoveryRollbackHostFailureStopsLaterVisitsAndConsumesCompensation(t *testing.T) {
	t.Parallel()

	input := activeRecoveryScheduleInput{
		classification:  recovery.ClassificationNeedsRollback,
		hostActionCount: 3,
		caller:          activeRecoveryCallerApplySettlement,
	}
	execution := mustActiveRecoveryExecution(t, input)
	runActiveRecoverySteps(t, execution, activeRecoveryCommonPreparationSteps())
	runActiveRecoverySteps(t, execution, activeRecoveryRollbackPreparationSteps())
	runActiveRecoveryRollbackPostStageSteps(t, execution, false)

	expected := []activeRecoveryHostVisit{{index: 0}, {index: 2}, {index: 1}}
	if err := execution.beginHostBatch(expected); err != nil {
		t.Fatal(err)
	}
	if err := execution.visitHostAction(0); err != nil {
		t.Fatal(err)
	}
	primary := errors.New("host action failed")
	operationErr := execution.settleHostBatch(primary)
	if !errors.Is(operationErr, primary) {
		t.Fatalf("host batch error = %v, want primary", operationErr)
	}
	if err := execution.visitHostAction(2); err == nil {
		t.Fatal("host visit accepted after failed batch")
	}
	prefix := "active-recovery/rollback/host-failure"
	rollbackFailure := errors.New("rollback restore failed")
	restoreErr := execution.runContinuingOutcomeStep(
		activeRecoveryStep{id: prefix + "/restore", kind: operationplan.EffectStepCompensation},
		prefix+"/restore",
		func() error { return rollbackFailure },
	)
	if !errors.Is(restoreErr, rollbackFailure) {
		t.Fatalf("restore error = %v, want rollback failure", restoreErr)
	}
	cleanupErr := execution.runFailureCleanup(prefix, func() error { return nil })
	if cleanupErr != nil {
		t.Fatalf("cleanup failed: %v", cleanupErr)
	}
	result := execution.finish(errors.Join(operationErr, restoreErr), nil)
	if !errors.Is(result, primary) || !errors.Is(result, rollbackFailure) {
		t.Fatalf("finish error = %v, want primary and rollback failure", result)
	}
}

func TestActiveRecoveryRollbackRejectsWrongAndDuplicateHostVisit(t *testing.T) {
	t.Parallel()

	input := activeRecoveryScheduleInput{
		classification:  recovery.ClassificationNeedsRollback,
		hostActionCount: 2,
		caller:          activeRecoveryCallerApplySettlement,
	}
	execution := mustActiveRecoveryExecution(t, input)
	runActiveRecoverySteps(t, execution, activeRecoveryCommonPreparationSteps())
	runActiveRecoverySteps(t, execution, activeRecoveryRollbackPreparationSteps())
	runActiveRecoveryRollbackPostStageSteps(t, execution, false)
	if err := execution.beginHostBatch([]activeRecoveryHostVisit{{index: 1}, {index: 0}}); err != nil {
		t.Fatal(err)
	}
	if err := execution.visitHostAction(0); err == nil {
		t.Fatal("wrong first host visit accepted")
	}
	if err := execution.visitHostAction(1); err != nil {
		t.Fatal(err)
	}
	if err := execution.visitHostAction(1); err == nil {
		t.Fatal("duplicate host visit accepted")
	}
}

func TestActiveRecoveryHostBatchRejectsIncompleteOrDuplicateBinding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected []activeRecoveryHostVisit
	}{
		{name: "incomplete", expected: []activeRecoveryHostVisit{{index: 0}}},
		{name: "duplicate", expected: []activeRecoveryHostVisit{{index: 0}, {index: 0}}},
		{name: "negative", expected: []activeRecoveryHostVisit{{index: -1}, {index: 0}}},
		{name: "out of range", expected: []activeRecoveryHostVisit{{index: 0}, {index: 2}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			execution := mustActiveRecoveryExecution(t, activeRecoveryScheduleInput{
				classification:  recovery.ClassificationNeedsRollback,
				hostActionCount: 2,
				caller:          activeRecoveryCallerApplySettlement,
			})
			runActiveRecoverySteps(t, execution, activeRecoveryCommonPreparationSteps())
			runActiveRecoverySteps(t, execution, activeRecoveryRollbackPreparationSteps())
			runActiveRecoveryRollbackPostStageSteps(t, execution, false)
			if err := execution.beginHostBatch(test.expected); err == nil {
				t.Fatalf("invalid host binding accepted: %#v", test.expected)
			}
		})
	}
}

func TestActiveRecoveryStructureRejectsInvalidBoundaryInputs(t *testing.T) {
	t.Parallel()

	for _, input := range []activeRecoveryScheduleInput{
		{classification: recovery.ClassificationNeedsRollback, hostActionCount: -1, caller: activeRecoveryCallerStandalone},
		{classification: recovery.ClassificationBlocked, caller: activeRecoveryCallerStandalone},
		{classification: recovery.ClassificationCleanBefore, caller: activeRecoveryCallerApplySettlement, hasBeforeRetirement: true},
		{classification: recovery.ClassificationCleanBefore},
		{classification: recovery.ClassificationCleanBefore, hasClaimTransitions: true, caller: activeRecoveryCallerStandalone},
		{classification: recovery.ClassificationCleanBefore, hostActionCount: 1, caller: activeRecoveryCallerStandalone},
	} {
		if _, err := compileActiveRecoveryStructure(input); err == nil {
			t.Fatalf("invalid active recovery input accepted: %#v", input)
		}
	}
}

func TestActiveRecoveryExecutionRejectsDifferentPlanAuthority(t *testing.T) {
	t.Parallel()

	expected := activeRecoverySchedulePlan(t, "operation:expected", "fingerprint:expected")
	different := activeRecoverySchedulePlan(t, "operation:different", "fingerprint:different")
	execution, err := newActiveRecoveryExecutionForPlan(
		expected,
		activeRecoveryCallerStandalone,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.requirePlan(expected); err != nil {
		t.Fatalf("require expected active recovery plan: %v", err)
	}
	if err := execution.requirePlan(different); err == nil {
		t.Fatal("different active recovery plan authority was accepted")
	}
}

func TestActiveRecoveryFinishRejectsUnderconsumedOwnedScheduleAndCloses(t *testing.T) {
	t.Parallel()

	execution := mustActiveRecoveryExecution(t, activeRecoveryScheduleInput{
		classification: recovery.ClassificationCleanBefore,
		caller:         activeRecoveryCallerStandalone,
	})
	closeCalls := 0
	err := execution.finish(nil, func() error {
		closeCalls++
		return nil
	})
	if err == nil {
		t.Fatal("underconsumed active recovery schedule finished successfully")
	}
	if closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", closeCalls)
	}
}

func activeRecoverySchedulePlan(
	t *testing.T,
	operationID string,
	fingerprint string,
) recovery.Plan {
	t.Helper()

	subject, err := topology.NewSubjectID(topology.SubjectProjection, "test.project", "agents")
	if err != nil {
		t.Fatal(err)
	}
	mode := recovery.PermissionMode(0o600)
	entry, err := recovery.NewEntry(
		subject,
		target.TargetCodex,
		nil,
		target.ScopeProject,
		"AGENTS.md",
		"",
		"",
		recovery.BeforePathState{Existed: false},
		recovery.ExpectedPathState{
			Existed:     true,
			Kind:        recovery.PathKindFile,
			ContentHash: "sha256:after",
			PathMode:    &mode,
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := recovery.NewRootProvenance(
		"/manifest",
		"sha256:object",
		"sha256:mount",
	)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := recovery.NewAuthority(
		operationID,
		"recovery/"+operationID,
		[]recovery.Entry{entry},
		durable.EmptySnapshot(),
		durable.EmptySnapshot(),
		nil,
		nil,
		provenance,
		fingerprint,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := recovery.NewSelection(authority, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := recovery.Classify(
		authority,
		selection,
		durable.EmptySnapshot(),
		[]recovery.PathEvidence{{Path: "AGENTS.md"}},
		nil,
		ownership.EmptyRegistry(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func mustActiveRecoveryExecution(
	t *testing.T,
	input activeRecoveryScheduleInput,
) *activeRecoveryExecution {
	t.Helper()
	structure, err := compileActiveRecoveryStructure(input)
	if err != nil {
		t.Fatal(err)
	}
	return &activeRecoveryExecution{
		cursor: structure.Begin(),
		input:  input,
	}
}

func runActiveRecoverySteps(
	t *testing.T,
	execution *activeRecoveryExecution,
	steps []activeRecoveryStep,
) {
	t.Helper()
	for _, step := range steps {
		runActiveRecoveryStep(t, execution, step)
	}
}

func runActiveRecoveryStep(
	t *testing.T,
	execution *activeRecoveryExecution,
	step activeRecoveryStep,
) {
	t.Helper()
	if err := execution.runTerminalStep(step, func() error { return nil }); err != nil {
		t.Fatalf("consume active recovery step %q: %v", step.id, err)
	}
}

func runActiveRecoveryBranchingStep(
	t *testing.T,
	execution *activeRecoveryExecution,
	step activeRecoveryStep,
) {
	t.Helper()
	if err := execution.runBranchingStep(step, func() error { return nil }); err != nil {
		t.Fatalf("consume active recovery branching step %q: %v", step.id, err)
	}
}

func runActiveRecoveryRollbackPostStageSteps(
	t *testing.T,
	execution *activeRecoveryExecution,
	requiresOwnershipRegistry bool,
) {
	t.Helper()
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/prepare-removal-cleanup",
		kind: operationplan.EffectStepObservation,
	})
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/begin-general-execution",
		kind: operationplan.EffectStepObservation,
	})
	registryKind := operationplan.EffectStepNoOp
	if requiresOwnershipRegistry {
		registryKind = operationplan.EffectStepObservation
	}
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/rebind-ownership-registry",
		kind: registryKind,
	})
	runActiveRecoveryBranchingStep(t, execution, activeRecoveryStep{
		id:   "active-recovery/rollback/prepare-host-execution",
		kind: operationplan.EffectStepObservation,
	})
}
