package recovery

import "testing"

func TestPhysicalPlanningPassesShareOnlyOperationPathCapacity(t *testing.T) {
	shared := NewPhysicalPathBudget()
	first, err := NewPhysicalWorkBudgetWithPathBudget(1, shared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewPhysicalWorkBudgetWithPathBudget(1, shared)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.AdmitPathComponents(7); err != nil {
		t.Fatal(err)
	}
	if err := second.AdmitPathComponents(11); err != nil {
		t.Fatal(err)
	}
	if shared.components != 18 {
		t.Fatalf("shared path components = %d, want 18", shared.components)
	}
	for range removalObservationsPerIntent {
		if err := first.AdmitObservation(); err != nil {
			t.Fatalf("first semantic pass: %v", err)
		}
		if err := second.AdmitObservation(); err != nil {
			t.Fatalf("second semantic pass: %v", err)
		}
	}
}
