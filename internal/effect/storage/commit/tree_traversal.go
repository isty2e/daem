package commit

import (
	"fmt"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

const (
	defaultTreeTraversalMaximumEntries = 100_000
	defaultTreeTraversalMaximumDepth   = 64
	defaultTreeTraversalMaximumBytes   = 4 << 30
)

type treeTraversalBudget struct {
	limits  mutationfs.TreeTraversalLimits
	entries int
	bytes   int64
}

func newTreeTraversalBudget(
	limits mutationfs.TreeTraversalLimits,
) (*treeTraversalBudget, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &treeTraversalBudget{limits: limits}, nil
}

func defaultTreeTraversalLimits() mutationfs.TreeTraversalLimits {
	limits, err := mutationfs.NewTreeTraversalLimits(
		defaultTreeTraversalMaximumEntries,
		defaultTreeTraversalMaximumDepth,
		defaultTreeTraversalMaximumBytes,
	)
	if err != nil {
		panic(err)
	}
	return limits
}

func (budget *treeTraversalBudget) admitBytes(count int64) error {
	if budget == nil || count < 0 {
		return fmt.Errorf("tree traversal budget is uninitialized")
	}
	if count > budget.limits.MaximumBytes()-budget.bytes {
		return fmt.Errorf(
			"tree exceeds %d regular-file bytes",
			budget.limits.MaximumBytes(),
		)
	}
	budget.bytes += count
	return nil
}

func (budget *treeTraversalBudget) remainingEntries() int {
	if budget == nil {
		return 0
	}
	return budget.limits.MaximumEntries() - budget.entries
}

func (budget *treeTraversalBudget) admitEntries(count int) error {
	if budget == nil || count < 0 {
		return fmt.Errorf("tree traversal budget is uninitialized")
	}
	if count > budget.remainingEntries() {
		return fmt.Errorf(
			"tree exceeds %d entries",
			budget.limits.MaximumEntries(),
		)
	}
	budget.entries += count
	return nil
}

func (budget *treeTraversalBudget) admitDepth(depth int) error {
	if budget == nil || depth < 0 {
		return fmt.Errorf("tree traversal budget is uninitialized")
	}
	if depth > budget.limits.MaximumDepth() {
		return fmt.Errorf(
			"tree exceeds maximum depth %d",
			budget.limits.MaximumDepth(),
		)
	}
	return nil
}
