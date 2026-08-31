//go:build darwin || linux

package recoverygate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestStructuralForwardLoweringPreservesReachablePhysicalAlternatives(t *testing.T) {
	t.Parallel()
	authority, stateDir := newStructuralForwardTestAuthority(t, true)
	statefilePath := filepath.Join(stateDir, "state.json")
	structure := incomparableForwardStructure(t)
	alternatives, err := structure.DemandAlternatives()
	if err != nil {
		t.Fatal(err)
	}

	structural, err := authority.lowerStructuralForwardWork(alternatives, statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := authority.lowerForwardPlan(forwardEffectPlan{
		BarrierValidationCalls: 3,
		DescendantPath:         statefilePath,
		DescendantFileCommits:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.work.total.dominates(structural.total) {
		t.Fatalf("legacy total = %+v, structural total = %+v", legacy.work.total, structural.total)
	}
	if legacy.work.total.pathComponents <= structural.total.pathComponents {
		t.Fatalf(
			"legacy path work = %d, structural path work = %d; want unreachable semantic combination removed",
			legacy.work.total.pathComponents,
			structural.total.pathComponents,
		)
	}
}

func TestStructuralForwardLoweringUsesAbsentStateDirBranchCosts(t *testing.T) {
	t.Parallel()
	authority, _ := newStructuralForwardTestAuthority(t, false)
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(builder.Choice(
		"absent-state-dir-alternatives",
		builder.Step("ensure", operationplan.EffectStepEstablishStateDir),
		builder.Step("barrier", operationplan.EffectStepValidateBarrier),
	))
	if err != nil {
		t.Fatal(err)
	}
	alternatives, err := structure.DemandAlternatives()
	if err != nil {
		t.Fatal(err)
	}
	structural, err := authority.lowerStructuralForwardWork(alternatives, "")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := authority.lowerForwardPlan(forwardEffectPlan{
		EnsureCalls:            1,
		BarrierValidationCalls: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.work.total.dominates(structural.total) ||
		legacy.work.total.pathComponents <= structural.total.pathComponents {
		t.Fatalf(
			"absent StateDir legacy total = %+v, structural total = %+v",
			legacy.work.total,
			structural.total,
		)
	}
}

func TestReserveForwardEffectStructureRejectsLegacyDemandMismatch(t *testing.T) {
	t.Parallel()
	authority, stateDir := newStructuralForwardTestAuthority(t, true)
	_, err := authority.ReserveForwardEffectStructure(
		incomparableForwardStructure(t),
		operationplan.NewDemand(
			0,
			2,
			0,
			filepath.Join(stateDir, "state.json"),
			0,
			1,
		),
	)
	if err == nil || !strings.Contains(err.Error(), "differs from legacy demand") {
		t.Fatalf("ReserveForwardEffectStructure error = %v", err)
	}
}

func TestReserveForwardEffectStructureRetainsLegacyRuntimeReservation(t *testing.T) {
	t.Parallel()
	authority, stateDir := newStructuralForwardTestAuthority(t, true)
	forward, err := authority.ReserveForwardEffectStructure(
		incomparableForwardStructure(t),
		operationplan.NewDemand(
			0,
			3,
			0,
			filepath.Join(stateDir, "state.json"),
			0,
			1,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := forward.Validate(t.Context()); err != nil {
		t.Fatalf("first legacy barrier validation: %v", err)
	}
	if err := forward.Validate(t.Context()); err != nil {
		t.Fatalf("second legacy barrier validation: %v", err)
	}
	if err := forward.Validate(t.Context()); err != nil {
		t.Fatalf("third legacy barrier validation: %v", err)
	}
	if err := forward.Validate(t.Context()); err == nil ||
		!strings.Contains(err.Error(), "exceeded the reserved forward plan") {
		t.Fatalf("fourth legacy barrier validation error = %v", err)
	}
	if _, err := forward.TakeDescendant(); err != nil {
		t.Fatalf("take legacy descendant reservation: %v", err)
	}
}

func incomparableForwardStructure(t *testing.T) operationplan.EffectStructure {
	t.Helper()
	var builder operationplan.EffectStructureBuilder
	barriers := operationplan.EffectSequence(
		builder.Step("barrier-1", operationplan.EffectStepValidateBarrier),
		builder.Step("barrier-2", operationplan.EffectStepValidateBarrier),
		builder.Step("barrier-3", operationplan.EffectStepValidateBarrier),
	)
	choice := builder.Choice(
		"physical-alternatives",
		barriers,
		builder.Step("commit", operationplan.EffectStepPublishDescendant),
	)
	structure, err := builder.Compile(choice)
	if err != nil {
		t.Fatal(err)
	}
	return structure
}

func newStructuralForwardTestAuthority(t *testing.T, present bool) (EffectAuthority, string) {
	t.Helper()
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	if present {
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	authority, err := NewEffectAuthority(t.Context(), daempaths.Paths{
		StateDir:    stateDir,
		RecoveryDir: filepath.Join(stateDir, "recovery"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return authority, stateDir
}
