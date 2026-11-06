package recoveryoperation

import (
	"strings"
	"testing"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func TestLoadRecoverablePlanChargesInventoryToProvidedPhysicalBudget(t *testing.T) {
	budget := physicalwork.NewPathBudget()
	for budget.AdmitPathComponents(physicalwork.MaximumPhysicalPathDepth) == nil {
	}

	_, err := LoadRecoverablePlanWithOptions(
		t.Context(),
		journalpublication.Paths{RecoveryDir: t.TempDir()},
		PlanLoadOptions{
			Filesystem:         journalTestFilesystem(),
			PhysicalPathBudget: budget,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "path-component work exceeds operation limit") {
		t.Fatalf("LoadRecoverablePlanWithOptions error = %v, want shared path-budget refusal", err)
	}
}
