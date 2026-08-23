//go:build windows

package commit

import (
	"context"
	"errors"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type windowsAbsenceRecordingBudget struct {
	pathCharges     int
	physicalCharges []int
	cancelAt        int
	cancel          context.CancelFunc
}

func (budget *windowsAbsenceRecordingBudget) AdmitPathComponents(count int) error {
	budget.pathCharges += count
	return nil
}

func (budget *windowsAbsenceRecordingBudget) AdmitPhysicalWork(pathComponents int, _ int, _ int64) error {
	budget.physicalCharges = append(budget.physicalCharges, pathComponents)
	if budget.cancel != nil && budget.cancelAt == len(budget.physicalCharges) {
		budget.cancel()
	}
	return nil
}

type windowsAbsencePathOnlyBudget struct {
	components int
}

func (budget *windowsAbsencePathOnlyBudget) AdmitPathComponents(count int) error {
	budget.components += count
	return nil
}

func TestWindowsRootedAbsenceAdmitsPathOnlyBudget(t *testing.T) {
	rootPath := t.TempDir()
	budget := &windowsAbsencePathOnlyBudget{}
	capability := acquireWindowsBoundedAbsenceCapability(t, rootPath, "missing/nested", budget)
	outcome, err := ConfirmRootedEntryAbsentWithOutcome(t.Context(), capability)
	if err != nil || outcome.State() != mutationfs.CommitOutcomeComplete {
		t.Fatalf("path-only bounded absence = %q, %v, want complete", outcome.State(), err)
	}
	if budget.components == 0 {
		t.Fatal("path-only bounded absence charged no component work")
	}
}

func TestWindowsRootedAbsenceUsesFourCompletePathObservations(t *testing.T) {
	rootPath := t.TempDir()
	budget := &windowsAbsenceRecordingBudget{}
	capability := acquireWindowsBoundedAbsenceCapability(t, rootPath, "missing/nested", budget)
	before := len(budget.physicalCharges)
	if _, err := ConfirmRootedEntryAbsentWithOutcome(t.Context(), capability); err != nil {
		t.Fatal(err)
	}
	charges := budget.physicalCharges[before:]
	if len(charges) != 4 {
		t.Fatalf("absence physical observations = %d, want 4: %v", len(charges), charges)
	}
	for index, charge := range charges {
		if charge != 2 {
			t.Fatalf("absence observation %d path components = %d, want 2", index, charge)
		}
	}
}

func TestWindowsRootedAbsencePreservesCallerCancellationBetweenPasses(t *testing.T) {
	rootPath := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	budget := &windowsAbsenceRecordingBudget{cancelAt: 2, cancel: cancel}
	capability := acquireWindowsBoundedAbsenceCapability(t, rootPath, "missing/nested", budget)
	before := len(budget.physicalCharges)
	_, err := ConfirmRootedEntryAbsentWithOutcome(ctx, capability)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("absence cancellation error = %v, want context.Canceled", err)
	}
	if got := len(budget.physicalCharges[before:]); got != 2 {
		t.Fatalf("absence observations before cancellation = %d, want 2", got)
	}
}

func acquireWindowsBoundedAbsenceCapability(
	t *testing.T,
	rootPath string,
	relativePath string,
	budget rootedpath.PhysicalTraversalBudget,
) rootedpath.CommitCapability {
	t.Helper()
	root, err := rootedpath.CaptureRootNoFollow(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := root.Authority()
	if err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination(relativePath)
	if err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	capability, err := root.AcquireBounded(destination, 64, budget)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	return capability
}
