package physicalwork

import "testing"

func TestCompletedCleanupClosesLifecycleWithoutTransferringCapacity(t *testing.T) {
	pending := NewCaptureBudget(nil)
	if err := pending.ConcludeCleanupLifecycle(); err == nil {
		t.Fatal("unobserved removal census was concluded")
	}
	budget, err := NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	entries, bytes := budget.RemainingEntries(), budget.RemainingBytes()
	if err := budget.ConcludeCleanupLifecycle(); err != nil {
		t.Fatal(err)
	}
	if entries != budget.RemainingEntries() || bytes != budget.RemainingBytes() {
		t.Fatal("cleanup conclusion transferred or replenished capacity")
	}
	if err := budget.ConcludeCleanupLifecycle(); err == nil {
		t.Fatal("completed cleanup concluded twice")
	}
	if _, err := budget.BeginReservedCleanupLifecycle(); err == nil {
		t.Fatal("completed cleanup transferred its capacity")
	}
	if err := budget.ReserveCleanupLifecycle(1, 1, 1, 0); err == nil {
		t.Fatal("completed cleanup admitted another reservation")
	}
}
