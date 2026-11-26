package qualification

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

// ReadBaselineTree establishes the correspondence between two digest formats.
// The selected path is not a recovery destination or a source of restore modes.
func ReadBaselineTree(ctx context.Context, path string, budget *physicalwork.Budget) (artifact.TreeInventory, error) {
	view, err := access.OpenView(path)
	if err != nil {
		return artifact.TreeInventory{}, err
	}
	entries := min(recovery.MaximumArtifactTreeEntries, budget.RemainingEntries()-1)
	if entries < 0 {
		return artifact.TreeInventory{}, fmt.Errorf("insufficient entry budget for the baseline tree")
	}
	limit, err := access.NewTraversalLimit(uint64(entries+1), min(recovery.MaximumArtifactTreeBytes, budget.RemainingBytes()))
	if err != nil {
		return artifact.TreeInventory{}, err
	}
	structure, err := access.NewTreeStructureLimit(entries, recovery.MaximumArtifactTreeDepth)
	if err != nil {
		return artifact.TreeInventory{}, err
	}
	tree, work, err := view.TreeInventoryWithLimits(ctx, limit, structure)
	if err != nil {
		return artifact.TreeInventory{}, fmt.Errorf("read baseline tree %q: %w", path, err)
	}
	if err := budget.AdmitPhysicalWork(0, work.DescendantEntries()+1, work.RegularFileBytes()); err != nil {
		return artifact.TreeInventory{}, err
	}
	return tree, nil
}
