package qualification

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func TestRecoveryDirectoryObservationRequiresOverflowCapacityBeforeRead(t *testing.T) {
	budget, err := physicalwork.NewBudget(1)
	if err != nil {
		t.Fatalf("construct recovery budget: %v", err)
	}
	allEntries, err := workmeasure.NewArtifactWork(budget.RemainingEntries(), 0)
	if err != nil {
		t.Fatalf("construct aggregate entry work: %v", err)
	}
	if err := budget.AdmitTree(allEntries); err != nil {
		t.Fatalf("consume aggregate entry capacity: %v", err)
	}
	if _, _, err := recoveryArtifactWorkLimits(true, recovery.MaximumArtifactTreeBytes, budget); err == nil || !strings.Contains(err.Error(), "remaining operation entry capacity") {
		t.Fatalf("directory limits error = %v, want pre-read overflow-capacity rejection", err)
	}
}
