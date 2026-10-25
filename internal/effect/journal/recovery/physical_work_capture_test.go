package recovery

import "testing"

func TestPhysicalCaptureAdmissionPreservesConsumedWork(t *testing.T) {
	budget := NewPhysicalCaptureBudget(nil)
	budget.pathComponentLimit = 7
	if err := budget.AdmitPathComponents(4); err != nil {
		t.Fatal(err)
	}
	consumed, _ := NewArtifactWork(MaximumPhysicalEntries-2, MaximumPhysicalBytes-3)
	if err := budget.AdmitTree(consumed); err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitObservation(); err == nil {
		t.Fatal("pending capture admitted a removal observation")
	}
	set := captureBudgetDemands(t)
	if err := budget.AdmitRemovalDemands(set); err != nil {
		t.Fatal(err)
	}
	if budget.RemainingEntries() != 2 || budget.RemainingBytes() != 3 {
		t.Fatal("admission replenished consumed tree work")
	}
	remaining, _ := NewArtifactWork(2, 3)
	if err := budget.AdmitTree(remaining); err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitPathComponents(3); err != nil {
		t.Fatal(err)
	}
	oneEntry, _ := NewArtifactWork(1, 0)
	oneByte, _ := NewArtifactWork(0, 1)
	if budget.AdmitTree(oneEntry) == nil || budget.AdmitTree(oneByte) == nil || budget.AdmitPathComponents(1) == nil {
		t.Fatal("pre-admission work no longer counts against the operation")
	}
	for range removalObservationsPerIntent {
		if err := budget.AdmitObservation(); err != nil {
			t.Fatal(err)
		}
	}
	if budget.AdmitRemovalDemands(set) == nil || budget.AdmitObservation() == nil {
		t.Fatal("a second admission replenished removal observations")
	}
}

func TestPhysicalCaptureEmptyAdmissionIsFinalAndDoesNotCountComparisons(t *testing.T) {
	budget := NewPhysicalCaptureBudget(nil)
	oneByte, _ := NewArtifactWork(0, 1)
	for range MaximumRemovalIntents + 1 {
		if err := budget.AdmitTree(oneByte); err != nil {
			t.Fatal("non-removing work was capped by removal cardinality", err)
		}
	}
	remaining := budget.RemainingBytes()
	if err := budget.AdmitRemovalDemands(RemovalDemandSet{}); err != nil {
		t.Fatal(err)
	}
	if budget.RemainingBytes() != remaining || budget.AdmitObservation() == nil {
		t.Fatal("empty admission changed consumed work or granted removal observations")
	}
	if budget.AdmitRemovalDemands(captureBudgetDemands(t)) == nil {
		t.Fatal("admitted empty set was mistaken for pending admission")
	}
	if _, err := budget.BeginReservedSemanticExecution(); err != nil {
		t.Fatal("empty admitted capture remained pending", err)
	}
}

func TestPhysicalCaptureInvalidDemandAdmissionIsAtomic(t *testing.T) {
	set := captureBudgetDemands(t)
	invalid := map[string]RemovalDemandSet{
		"invalid":    {demands: []RemovalDemand{{}}},
		"duplicates": {demands: []RemovalDemand{set.demands[0], set.demands[0]}},
		"over-limit": {demands: make([]RemovalDemand, MaximumRemovalIntents+1)},
	}
	for name, demands := range invalid {
		t.Run(name, func(t *testing.T) {
			budget := NewPhysicalCaptureBudget(nil)
			if budget.AdmitRemovalDemands(demands) == nil || budget.AdmitObservation() == nil {
				t.Fatal("invalid complete demand set opened removal admission")
			}
			if err := budget.AdmitRemovalDemands(set); err != nil {
				t.Fatal("invalid admission burned the transition", err)
			}
			for range removalObservationsPerIntent {
				if err := budget.AdmitObservation(); err != nil {
					t.Fatal(err)
				}
			}
			if budget.AdmitObservation() == nil {
				t.Fatal("invalid attempt increased exact observation capacity")
			}
		})
	}
	for _, count := range []int{0, 1} {
		budget, err := NewPhysicalWorkBudget(count)
		if err != nil {
			t.Fatal(err)
		}
		if budget.AdmitRemovalDemands(set) == nil {
			t.Fatal("eager budget was reopened as a capture allocation")
		}
	}
	var absent *PhysicalWorkBudget
	if absent.AdmitRemovalDemands(set) == nil {
		t.Fatal("missing budget admitted demands")
	}
}

func TestPhysicalCaptureBlocksEveryExecutionTransferBeforeAdmission(t *testing.T) {
	transfers := map[string]func(*PhysicalWorkBudget) error{
		"cleanup":   func(budget *PhysicalWorkBudget) error { _, err := budget.BeginReservedCleanupLifecycle(); return err },
		"semantic":  func(budget *PhysicalWorkBudget) error { _, err := budget.BeginReservedSemanticExecution(); return err },
		"backup":    func(budget *PhysicalWorkBudget) error { _, err := budget.BeginReservedBackupExecution(); return err },
		"forward":   func(budget *PhysicalWorkBudget) error { _, err := budget.BeginReservedForwardExecution(); return err },
		"execution": func(budget *PhysicalWorkBudget) error { _, err := budget.BeginReservedExecution(); return err },
		"retirement": func(budget *PhysicalWorkBudget) error {
			_, err := budget.BeginReservedRetirementExecution()
			return err
		},
		"scratch": func(budget *PhysicalWorkBudget) error { _, _, err := budget.BeginReservedScratchCleanup(); return err },
		"general": func(budget *PhysicalWorkBudget) error { _, _, err := budget.BeginGeneralExecution(); return err },
	}
	for name, transfer := range transfers {
		t.Run(name, func(t *testing.T) {
			budget := NewPhysicalCaptureBudget(nil)
			if name == "scratch" {
				if err := budget.ReserveScratchCleanup(ArtifactWork{}, 1); err != nil {
					t.Fatal(err)
				}
			}
			if name == "general" {
				// Satisfy the independent transfer prerequisites so their guards
				// cannot hide a missing admission check.
				budget.cleanupExecutionBegun = true
				budget.scratchCleanupDisposition = scratchCleanupNotApplicable
				budget.retirementDisposition = retirementNotApplicable
			}
			if err := transfer(budget); err == nil {
				t.Fatal("pending capture transferred execution capacity")
			}
			if err := budget.AdmitRemovalDemands(RemovalDemandSet{}); err != nil {
				t.Fatal(err)
			}
			if err := transfer(budget); err != nil {
				t.Fatal("failed early transfer consumed later admitted capacity", err)
			}
		})
	}
}

func TestPhysicalCapturePreservesSharedPathsAndExecutionReservations(t *testing.T) {
	shared := NewPhysicalPathBudget()
	shared.components = maximumPhysicalPathComponentVisits - 7
	budget := NewPhysicalCaptureBudget(shared)
	if err := budget.ReserveSemanticPathComponents(3); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupFileExecution(ArtifactWork{bytes: 5}); err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitRemovalDemands(RemovalDemandSet{}); err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitPathComponents(4); err != nil {
		t.Fatal(err)
	}
	if budget.AdmitPathComponents(1) == nil {
		t.Fatal("admission replaced the shared pre-decode path envelope")
	}
	semantic, err := budget.BeginReservedSemanticExecution()
	if err != nil {
		t.Fatal(err)
	}
	if semantic.AdmitPathComponents(3) != nil || semantic.AdmitPathComponents(1) == nil {
		t.Fatal("semantic reservation was reset or enlarged")
	}
	backup, err := budget.BeginReservedBackupExecution()
	if err != nil {
		t.Fatal(err)
	}
	if backup.RemainingBytes() != 6 {
		t.Fatal("backup reservation or overflow proof byte was lost")
	}
}

func captureBudgetDemands(t *testing.T) RemovalDemandSet {
	t.Helper()
	set, err := NewRemovalDemandSet([]RemovalDemand{testRemovalIntent(t).Demand()})
	if err != nil {
		t.Fatal(err)
	}
	return set
}
