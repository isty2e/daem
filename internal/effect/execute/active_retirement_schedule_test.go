package execute

import (
	"errors"
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/operationplan"
)

func TestActiveJournalRetirementStructureConsumesControlAlternatives(t *testing.T) {
	for _, test := range []struct {
		name        string
		alternative int
		stepID      string
		kind        operationplan.EffectStepKind
		settle      bool
	}{
		{
			name:        "present",
			alternative: 0,
			stepID:      activeRetirementControlPresentStep,
			kind:        operationplan.EffectStepNoOp,
		},
		{
			name:        "publish",
			alternative: 1,
			stepID:      activeRetirementControlPublishStep,
			kind:        operationplan.EffectStepPersistence,
			settle:      true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			structure, err := compileActiveJournalRetirementStructure()
			if err != nil {
				t.Fatalf("compileActiveJournalRetirementStructure returned error: %v", err)
			}
			cursor := structure.Begin()
			for _, step := range activeRetirementPreControlSteps {
				consumeActiveRetirementStepSuccess(t, cursor, step)
			}
			if err := cursor.SelectAlternative(activeRetirementControlModeChoice, test.alternative); err != nil {
				t.Fatalf("select control mode: %v", err)
			}
			if err := cursor.Consume(test.stepID, test.kind); err != nil {
				t.Fatalf("consume control mode: %v", err)
			}
			if test.settle {
				if err := cursor.SelectAlternative(activeRetirementControlOutcomeChoice, 1); err != nil {
					t.Fatalf("settle control mode: %v", err)
				}
			}
			for _, step := range activeRetirementPostControlSteps {
				consumeActiveRetirementStepSuccess(t, cursor, step)
			}
			if err := cursor.Consume(activeRetirementSuccessStep, operationplan.EffectStepTerminal); err != nil {
				t.Fatalf("consume success terminal: %v", err)
			}
			if err := cursor.FinishSuccess(); err != nil {
				t.Fatalf("finish active retirement: %v", err)
			}
		})
	}
}

func TestActiveJournalRetirementStructureStopsAfterEverySelectedFailure(t *testing.T) {
	for _, failed := range activeRetirementAllSteps() {
		t.Run(failed.id, func(t *testing.T) {
			structure, err := compileActiveJournalRetirementStructure()
			if err != nil {
				t.Fatalf("compileActiveJournalRetirementStructure returned error: %v", err)
			}
			cursor := structure.Begin()
			advanceActiveRetirementBeforeStep(t, cursor, failed)
			if err := cursor.Consume(failed.id, failed.kind); err != nil {
				t.Fatalf("consume failing step: %v", err)
			}
			if err := cursor.SelectAlternative(failed.id+"/outcome", 0); err != nil {
				t.Fatalf("select failure outcome: %v", err)
			}
			if err := cursor.Consume(failed.id+"/failed", operationplan.EffectStepTerminal); err != nil {
				t.Fatalf("consume failure terminal: %v", err)
			}
			if err := cursor.FinishSuccess(); err != nil {
				t.Fatalf("finish failed active retirement branch: %v", err)
			}
			if err := cursor.Consume(activeRetirementSuccessStep, operationplan.EffectStepTerminal); err == nil {
				t.Fatal("finished failure branch admitted a later retirement step")
			}
		})
	}
}

func TestActiveJournalRetirementStructureStopsAfterControlPublicationFailure(t *testing.T) {
	structure, err := compileActiveJournalRetirementStructure()
	if err != nil {
		t.Fatalf("compileActiveJournalRetirementStructure returned error: %v", err)
	}
	cursor := structure.Begin()
	for _, step := range activeRetirementPreControlSteps {
		consumeActiveRetirementStepSuccess(t, cursor, step)
	}
	if err := cursor.SelectAlternative(activeRetirementControlModeChoice, 1); err != nil {
		t.Fatalf("select control publication: %v", err)
	}
	if err := cursor.Consume(activeRetirementControlPublishStep, operationplan.EffectStepPersistence); err != nil {
		t.Fatalf("consume control publication: %v", err)
	}
	if err := cursor.SelectAlternative(activeRetirementControlOutcomeChoice, 0); err != nil {
		t.Fatalf("select control failure: %v", err)
	}
	if err := cursor.Consume(activeRetirementControlFailureStep, operationplan.EffectStepTerminal); err != nil {
		t.Fatalf("consume control failure terminal: %v", err)
	}
	if err := cursor.FinishSuccess(); err != nil {
		t.Fatalf("finish control failure branch: %v", err)
	}
}

func TestActiveJournalRetirementStructureRejectsWrongOrder(t *testing.T) {
	structure, err := compileActiveJournalRetirementStructure()
	if err != nil {
		t.Fatalf("compileActiveJournalRetirementStructure returned error: %v", err)
	}
	cursor := structure.Begin()
	if err := cursor.Consume(
		activeRetirementPreControlSteps[1].id,
		activeRetirementPreControlSteps[1].kind,
	); err == nil {
		t.Fatal("active retirement accepted a later step before plan validation")
	}
	consumeActiveRetirementStepSuccess(t, cursor, activeRetirementPreControlSteps[0])
}

func TestActiveJournalRetirementStructureIsDeterministicAndHasNoBarrierDemand(t *testing.T) {
	first, err := compileActiveJournalRetirementStructure()
	if err != nil {
		t.Fatalf("compile first structure: %v", err)
	}
	second, err := compileActiveJournalRetirementStructure()
	if err != nil {
		t.Fatalf("compile second structure: %v", err)
	}
	if !first.Equal(second) {
		t.Fatal("active retirement structure changed across identical compilation")
	}
	demand, err := first.LegacyDemand()
	if err != nil {
		t.Fatalf("derive active retirement demand: %v", err)
	}
	if demand != (operationplan.Demand{}) {
		t.Fatalf("active retirement demand = %#v, want zero", demand)
	}
}

func TestActiveRetirementExecutionUsesSuppliedStructure(t *testing.T) {
	execution := newActiveRetirementExecution(operationplan.EffectStructure{})
	if err := execution.AdmitRetirementStep(journal.RetirementStepValidateActivePlan); err == nil {
		t.Fatal("active retirement recompiled instead of consuming the supplied structure")
	}
}

func TestActiveRetirementExecutionConsumesControlAlternatives(t *testing.T) {
	for _, controlStep := range []journal.RetirementExecutionStep{
		journal.RetirementStepControlPresent,
		journal.RetirementStepPublishControl,
	} {
		t.Run(activeRetirementControlStepName(controlStep), func(t *testing.T) {
			structure, err := compileActiveJournalRetirementStructure()
			if err != nil {
				t.Fatal(err)
			}
			execution := newActiveRetirementExecution(structure)
			for _, step := range activeRetirementRuntimeSteps(controlStep) {
				if err := execution.AdmitRetirementStep(step); err != nil {
					t.Fatalf("AdmitRetirementStep(%d): %v", step, err)
				}
				if err := execution.SettleRetirementStep(step, true); err != nil {
					t.Fatalf("SettleRetirementStep(%d): %v", step, err)
				}
			}
			if err := execution.finish(nil); err != nil {
				t.Fatalf("finish: %v", err)
			}
			if err := execution.AdmitRetirementStep(journal.RetirementStepValidateActivePlan); err == nil {
				t.Fatal("closed active retirement admitted another step")
			}
		})
	}
}

func TestActiveRetirementExecutionStopsAfterEverySelectedFailure(t *testing.T) {
	runtimeSteps := activeRetirementRuntimeSteps(journal.RetirementStepControlPresent)
	for _, failed := range runtimeSteps {
		if failed == journal.RetirementStepControlPresent {
			continue
		}
		t.Run(activeRetirementRuntimeStepName(failed), func(t *testing.T) {
			structure, err := compileActiveJournalRetirementStructure()
			if err != nil {
				t.Fatal(err)
			}
			execution := newActiveRetirementExecution(structure)
			for _, step := range runtimeSteps {
				if err := execution.AdmitRetirementStep(step); err != nil {
					t.Fatalf("AdmitRetirementStep(%d): %v", step, err)
				}
				succeeded := step != failed
				if err := execution.SettleRetirementStep(step, succeeded); err != nil {
					t.Fatalf("SettleRetirementStep(%d, %t): %v", step, succeeded, err)
				}
				if !succeeded {
					break
				}
			}
			if err := execution.AdmitRetirementStep(journal.RetirementStepRetireControl); err == nil {
				t.Fatal("failure branch admitted a later retirement step")
			}
			failure := errors.New("retirement failed")
			if err := execution.finish(failure); !errors.Is(err, failure) {
				t.Fatalf("finish error = %v, want retirement failure", err)
			}
		})
	}
}

func TestActiveRetirementExecutionStopsAfterControlPublicationFailure(t *testing.T) {
	structure, err := compileActiveJournalRetirementStructure()
	if err != nil {
		t.Fatal(err)
	}
	execution := newActiveRetirementExecution(structure)
	for _, step := range activeRetirementPreControlSteps {
		if err := execution.AdmitRetirementStep(step.retirementStep); err != nil {
			t.Fatal(err)
		}
		if err := execution.SettleRetirementStep(step.retirementStep, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := execution.AdmitRetirementStep(journal.RetirementStepPublishControl); err != nil {
		t.Fatal(err)
	}
	if err := execution.SettleRetirementStep(journal.RetirementStepPublishControl, false); err != nil {
		t.Fatal(err)
	}
	if err := execution.AdmitRetirementStep(journal.RetirementStepValidateActiveRecord); err == nil {
		t.Fatal("control-publication failure admitted active-record validation")
	}
	failure := errors.New("publish control failed")
	if err := execution.finish(failure); err != failure {
		t.Fatalf("finish error = %v, want original publication failure", err)
	}
}

func TestActiveRetirementExecutionMismatchDoesNotConsumeExpectedStep(t *testing.T) {
	structure, err := compileActiveJournalRetirementStructure()
	if err != nil {
		t.Fatal(err)
	}
	execution := newActiveRetirementExecution(structure)
	if err := execution.AdmitRetirementStep(journal.RetirementStepValidatePreparedLayout); err == nil {
		t.Fatal("mismatched active retirement step was admitted")
	}
	first := journal.RetirementStepValidateActivePlan
	if err := execution.AdmitRetirementStep(first); err != nil {
		t.Fatalf("admit expected step after mismatch: %v", err)
	}
	if err := execution.AdmitRetirementStep(first); err == nil {
		t.Fatal("active retirement admitted a duplicate pending step")
	}
	if err := execution.SettleRetirementStep(journal.RetirementStepValidatePreparedLayout, true); err == nil {
		t.Fatal("active retirement settled a step that was not admitted")
	}
	if err := execution.SettleRetirementStep(first, true); err != nil {
		t.Fatalf("settle expected step: %v", err)
	}
	if err := execution.finish(nil); err == nil {
		t.Fatal("under-consumed active retirement finished successfully")
	}
}

func activeRetirementAllSteps() []activeRetirementStep {
	steps := make([]activeRetirementStep, 0, len(activeRetirementPreControlSteps)+len(activeRetirementPostControlSteps))
	steps = append(steps, activeRetirementPreControlSteps[:]...)
	steps = append(steps, activeRetirementPostControlSteps[:]...)
	return steps
}

func advanceActiveRetirementBeforeStep(
	t *testing.T,
	cursor *operationplan.EffectCursor,
	target activeRetirementStep,
) {
	t.Helper()
	for _, step := range activeRetirementPreControlSteps {
		if step == target {
			return
		}
		consumeActiveRetirementStepSuccess(t, cursor, step)
	}
	if err := cursor.SelectAlternative(activeRetirementControlModeChoice, 0); err != nil {
		t.Fatalf("select present control: %v", err)
	}
	if err := cursor.Consume(activeRetirementControlPresentStep, operationplan.EffectStepNoOp); err != nil {
		t.Fatalf("consume present control: %v", err)
	}
	for _, step := range activeRetirementPostControlSteps {
		if step == target {
			return
		}
		consumeActiveRetirementStepSuccess(t, cursor, step)
	}
	t.Fatalf("active retirement step %q is not part of the schedule", target.id)
}

func consumeActiveRetirementStepSuccess(
	t *testing.T,
	cursor *operationplan.EffectCursor,
	step activeRetirementStep,
) {
	t.Helper()
	if err := cursor.Consume(step.id, step.kind); err != nil {
		t.Fatalf("consume %s: %v", step.id, err)
	}
	if err := cursor.SelectAlternative(step.id+"/outcome", 1); err != nil {
		t.Fatalf("settle %s: %v", step.id, err)
	}
}

func activeRetirementRuntimeSteps(
	controlStep journal.RetirementExecutionStep,
) []journal.RetirementExecutionStep {
	steps := make(
		[]journal.RetirementExecutionStep,
		0,
		activeRetirementStepCount(),
	)
	for _, step := range activeRetirementPreControlSteps {
		steps = append(steps, step.retirementStep)
	}
	steps = append(steps, controlStep)
	for _, step := range activeRetirementPostControlSteps {
		steps = append(steps, step.retirementStep)
	}
	return steps
}

func activeRetirementControlStepName(step journal.RetirementExecutionStep) string {
	if step == journal.RetirementStepControlPresent {
		return "present"
	}
	return "publish"
}

func activeRetirementRuntimeStepName(step journal.RetirementExecutionStep) string {
	return fmt.Sprintf("step-%d", step)
}
