package execute

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func TestRollbackScratchCleanupKeepsSourceDepthCeiling(t *testing.T) {
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatalf("construct recovery budget: %v", err)
	}
	limits, err := recoveryStagingTraversalLimits(budget)
	if err != nil {
		t.Fatalf("construct recovery staging limits: %v", err)
	}
	if limits.MaximumDepth() != recovery.MaximumArtifactTreeDepth {
		t.Fatalf("source staging depth = %d, want %d", limits.MaximumDepth(), recovery.MaximumArtifactTreeDepth)
	}
}
