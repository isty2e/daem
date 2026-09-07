package recoverygate

import "testing"

func TestStateDirOperationWorkBudgetAcceptsExactIndependentLimits(t *testing.T) {
	budget := &stateDirOperationWorkBudget{}
	if err := budget.AdmitPhysicalWork(
		defaultStateDirMaximumPathComponentWork,
		defaultStateDirMaximumEntryWork,
		defaultStateDirMaximumByteWork,
	); err != nil {
		t.Fatalf("exact StateDir operation work: %v", err)
	}
	for name, admit := range map[string]func() error{
		"path":  func() error { return budget.AdmitPhysicalWork(1, 0, 0) },
		"entry": func() error { return budget.AdmitPhysicalWork(0, 1, 0) },
		"byte":  func() error { return budget.AdmitPhysicalWork(0, 0, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := admit(); err == nil {
				t.Fatal("over-limit StateDir operation work succeeded")
			}
		})
	}
}

func TestStateDirOperationWorkBudgetRejectsAtomically(t *testing.T) {
	budget := &stateDirOperationWorkBudget{}
	if err := budget.AdmitPhysicalWork(1, defaultStateDirMaximumEntryWork+1, 1); err == nil {
		t.Fatal("invalid combined StateDir operation work succeeded")
	}
	if budget.paths != 0 || budget.entries != 0 || budget.bytes != 0 {
		t.Fatalf(
			"failed admission changed budget paths=%d entries=%d bytes=%d",
			budget.paths,
			budget.entries,
			budget.bytes,
		)
	}
}
