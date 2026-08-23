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
	calibrationBudget := &windowsAbsenceRecordingBudget{}
	calibration := acquireWindowsBoundedAbsenceCapability(t, rootPath, "missing/nested", calibrationBudget)
	beforeOpen := calibrationBudget.pathCharges
	rootFile, err := calibration.OpenRootDirectoryForMutation()
	if err != nil {
		_ = calibration.Close()
		t.Fatal(err)
	}
	perOpen := calibrationBudget.pathCharges - beforeOpen
	if err := rootFile.Close(); err != nil {
		_ = calibration.Close()
		t.Fatal(err)
	}
	if err := calibration.Close(); err != nil {
		t.Fatal(err)
	}

	budget := &windowsAbsenceRecordingBudget{}
	capability := acquireWindowsBoundedAbsenceCapability(t, rootPath, "missing/nested", budget)
	before := budget.pathCharges
	if _, err := ConfirmRootedEntryAbsentWithOutcome(t.Context(), capability); err != nil {
		t.Fatal(err)
	}
	charged := budget.pathCharges - before
	if charged != 4*perOpen {
		t.Fatalf("absence path work = %d components, want four root opens of %d", charged, perOpen)
	}
}

func TestWindowsRootedAbsencePreservesCallerCancellationBetweenPasses(t *testing.T) {
	rootPath := t.TempDir()
	calibrationBudget := &windowsAbsenceRecordingBudget{}
	calibration := acquireWindowsBoundedAbsenceCapability(t, rootPath, "missing/nested", calibrationBudget)
	beforeOpen := calibrationBudget.pathCharges
	rootFile, err := calibration.OpenRootDirectoryForMutation()
	if err != nil {
		_ = calibration.Close()
		t.Fatal(err)
	}
	perOpen := calibrationBudget.pathCharges - beforeOpen
	if err := rootFile.Close(); err != nil {
		_ = calibration.Close()
		t.Fatal(err)
	}
	if err := calibration.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	budget := &windowsAbsenceRecordingBudget{}
	capability := acquireWindowsBoundedAbsenceCapability(t, rootPath, "missing/nested", budget)
	before := budget.pathCharges
	faults := faultPlan{actions: map[phase]func(){phaseSyncCleanupParent: cancel}}
	err = confirmWindowsRootedAbsenceWithFaults(ctx, capability, faults)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("absence cancellation error = %v, want context.Canceled", err)
	}
	if got := budget.pathCharges - before; got != perOpen {
		t.Fatalf("absence observations before cancellation charged %d components, want one root open of %d", got, perOpen)
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
