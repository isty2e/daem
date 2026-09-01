//go:build darwin || linux

package recoverygate

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/operationplan"
)

func TestForwardEffectExecutionRejectsCursorMismatchBeforePhysicalConsumption(t *testing.T) {
	t.Parallel()
	authority, _ := newStructuralForwardTestAuthority(t, true)
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(operationplan.EffectSequence(
		builder.Step("barrier", operationplan.EffectStepValidateBarrier),
		builder.Step("terminal", operationplan.EffectStepTerminal),
	))
	if err != nil {
		t.Fatal(err)
	}

	execution, err := authority.ReserveForwardEffectExecution(structure, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.ValidateBarrier(t.Context(), "wrong"); err == nil {
		t.Fatal("mismatched cursor step was accepted")
	}
	if err := execution.ValidateBarrier(t.Context(), "barrier"); err != nil {
		t.Fatalf("matching barrier after mismatch: %v", err)
	}
	if err := execution.ConsumeLifecycle("terminal", operationplan.EffectStepTerminal); err != nil {
		t.Fatal(err)
	}
	if err := execution.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := execution.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestForwardEffectExecutionCloseAbortsOnlyBeforeEffect(t *testing.T) {
	t.Parallel()
	structure := forwardExecutionExternalStructure(t)

	beforeAuthority, _ := newStructuralForwardTestAuthority(t, true)
	before, err := beforeAuthority.ReserveForwardEffectExecution(structure, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := before.ValidateBarrier(t.Context(), "barrier"); err != nil {
		t.Fatal(err)
	}
	if err := before.Close(); err != nil {
		t.Fatalf("pre-effect close: %v", err)
	}

	afterAuthority, _ := newStructuralForwardTestAuthority(t, true)
	after, err := afterAuthority.ReserveForwardEffectExecution(structure, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := after.ValidateBarrier(t.Context(), "barrier"); err != nil {
		t.Fatal(err)
	}
	if err := after.ConsumeLifecycle("external", operationplan.EffectStepExternal); err != nil {
		t.Fatal(err)
	}
	if err := after.Close(); err == nil || !strings.Contains(err.Error(), "after an effect started") {
		t.Fatalf("post-effect incomplete close error = %v", err)
	}
}

func TestForwardEffectExecutionMapsForwardPhaseToStateDirLifecycle(t *testing.T) {
	t.Parallel()
	authority, stateDir := newStructuralForwardTestAuthority(t, false)
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(builder.ForwardPhase(
		"apply",
		operationplan.EffectSequence(
			builder.Step("first", operationplan.EffectStepForwardEffect),
			builder.Step("second", operationplan.EffectStepForwardEffect),
			builder.Step("terminal", operationplan.EffectStepTerminal),
		),
	))
	if err != nil {
		t.Fatal(err)
	}
	execution, err := authority.ReserveForwardEffectExecution(structure, "")
	if err != nil {
		t.Fatal(err)
	}

	peerValidations := 0
	created, err := execution.ConsumeForwardEffect(
		t.Context(),
		"first",
		func(context.Context) error {
			peerValidations++
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !created || peerValidations != 2 {
		t.Fatalf("first forward effect = created %t, peer validations %d; want true, 2", created, peerValidations)
	}
	created, err = execution.ConsumeForwardEffect(t.Context(), "second", nil)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second forward effect reported StateDir creation")
	}
	if _, err := os.Stat(stateDir); err != nil {
		t.Fatalf("created StateDir: %v", err)
	}
	if err := execution.ConsumeLifecycle("terminal", operationplan.EffectStepTerminal); err != nil {
		t.Fatal(err)
	}
	if err := execution.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := execution.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestForwardEffectExecutionAllowsSelectedShortBranchWithConservativeOverreservation(t *testing.T) {
	t.Parallel()
	authority, _ := newStructuralForwardTestAuthority(t, true)
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(builder.Choice(
		"branch",
		builder.Step("short-terminal", operationplan.EffectStepTerminal),
		operationplan.EffectSequence(
			builder.Step("barrier-1", operationplan.EffectStepValidateBarrier),
			builder.Step("barrier-2", operationplan.EffectStepValidateBarrier),
			builder.Step("long-terminal", operationplan.EffectStepTerminal),
		),
	))
	if err != nil {
		t.Fatal(err)
	}
	execution, err := authority.ReserveForwardEffectExecution(structure, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.SelectAlternative("branch", 0); err != nil {
		t.Fatal(err)
	}
	if err := execution.ConsumeLifecycle("short-terminal", operationplan.EffectStepTerminal); err != nil {
		t.Fatal(err)
	}
	if err := execution.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := execution.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestForwardEffectExecutionAllowsSettledFailureAfterEffectWithConservativeOverreservation(
	t *testing.T,
) {
	t.Parallel()
	authority, _ := newStructuralForwardTestAuthority(t, true)
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(operationplan.EffectSequence(
		builder.Step("external", operationplan.EffectStepExternal),
		builder.Choice(
			"outcome",
			builder.Step("failed-terminal", operationplan.EffectStepTerminal),
			operationplan.EffectSequence(
				builder.Step("barrier", operationplan.EffectStepValidateBarrier),
				builder.Step("success-terminal", operationplan.EffectStepTerminal),
			),
		),
	))
	if err != nil {
		t.Fatal(err)
	}
	execution, err := authority.ReserveForwardEffectExecution(structure, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.ConsumeLifecycle("external", operationplan.EffectStepExternal); err != nil {
		t.Fatal(err)
	}
	if err := execution.SelectAlternative("outcome", 0); err != nil {
		t.Fatal(err)
	}
	if err := execution.ConsumeLifecycle(
		"failed-terminal",
		operationplan.EffectStepTerminal,
	); err != nil {
		t.Fatal(err)
	}
	if err := execution.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := execution.Close(); err != nil {
		t.Fatal(err)
	}
}

func forwardExecutionExternalStructure(t *testing.T) operationplan.EffectStructure {
	t.Helper()
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(operationplan.EffectSequence(
		builder.Step("barrier", operationplan.EffectStepValidateBarrier),
		builder.Step("external", operationplan.EffectStepExternal),
		builder.Step("terminal", operationplan.EffectStepTerminal),
	))
	if err != nil {
		t.Fatal(err)
	}
	return structure
}
