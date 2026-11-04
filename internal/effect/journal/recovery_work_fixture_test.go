package journal

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func newRecoveryPathBudget(t *testing.T) *physicalwork.Budget {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatalf("new recovery path budget: %v", err)
	}
	return budget
}

func recoveryBackupBudgetForTest(t *testing.T) *physicalwork.Budget {
	t.Helper()
	budget, err := physicalwork.NewBudget(1)
	if err != nil {
		t.Fatalf("construct recovery backup work budget: %v", err)
	}
	return budget
}
