package journal

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func TestLoadRecoverablePlanChargesInventoryToProvidedPhysicalBudget(t *testing.T) {
	budget := recovery.NewPhysicalPathBudget()
	for budget.AdmitPathComponents(recovery.MaximumPhysicalPathDepth) == nil {
	}

	_, err := LoadRecoverablePlanWithOptions(
		t.Context(),
		Paths{RecoveryDir: t.TempDir()},
		PlanLoadOptions{
			Filesystem:         journalTestFilesystem(),
			PhysicalPathBudget: budget,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "path-component work exceeds operation limit") {
		t.Fatalf("LoadRecoverablePlanWithOptions error = %v, want shared path-budget refusal", err)
	}
}
