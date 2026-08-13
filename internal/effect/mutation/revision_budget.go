package mutation

import "fmt"

const (
	maximumRevisionTreeEntries      = 100_000
	maximumRevisionTreeDepth        = 64
	maximumRevisionOperationEntries = 400_000
	maximumRevisionOperationBytes   = int64(16 << 30)
)

type revisionLimitScope string

const (
	revisionLimitTree      revisionLimitScope = "tree"
	revisionLimitOperation revisionLimitScope = "operation"
)

type revisionLimitResource string

const (
	revisionLimitEntries revisionLimitResource = "entries"
	revisionLimitDepth   revisionLimitResource = "depth"
	revisionLimitBytes   revisionLimitResource = "bytes"
)

// RevisionLimitError reports deterministic mutation-evidence exhaustion.
type RevisionLimitError struct {
	scope    revisionLimitScope
	resource revisionLimitResource
	limit    int64
	observed int64
}

func (err RevisionLimitError) Error() string {
	return fmt.Sprintf(
		"mutation revision %s %s exceed limit %d (observed %d)",
		err.scope,
		err.resource,
		err.limit,
		err.observed,
	)
}

// Limit returns the exhausted ceiling.
func (err RevisionLimitError) Limit() int64 { return err.limit }

// Observed returns the first rejected work total.
func (err RevisionLimitError) Observed() int64 { return err.observed }

type revisionCaptureLimits struct {
	maximumTreeEntries      int
	maximumTreeDepth        int
	maximumOperationEntries int
	maximumOperationBytes   int64
	initialized             bool
}

func newRevisionCaptureLimits(
	maximumTreeEntries int,
	maximumTreeDepth int,
	maximumOperationEntries int,
	maximumOperationBytes int64,
) (revisionCaptureLimits, error) {
	if maximumTreeEntries < 0 {
		return revisionCaptureLimits{}, fmt.Errorf("mutation revision tree entries must not be negative")
	}
	if maximumTreeDepth < 0 {
		return revisionCaptureLimits{}, fmt.Errorf("mutation revision tree depth must not be negative")
	}
	if maximumOperationEntries < 0 {
		return revisionCaptureLimits{}, fmt.Errorf("mutation revision operation entries must not be negative")
	}
	if maximumOperationBytes < 0 {
		return revisionCaptureLimits{}, fmt.Errorf("mutation revision operation bytes must not be negative")
	}
	return revisionCaptureLimits{
		maximumTreeEntries:      maximumTreeEntries,
		maximumTreeDepth:        maximumTreeDepth,
		maximumOperationEntries: maximumOperationEntries,
		maximumOperationBytes:   maximumOperationBytes,
		initialized:             true,
	}, nil
}

func defaultRevisionCaptureLimits() revisionCaptureLimits {
	limits, err := newRevisionCaptureLimits(
		maximumRevisionTreeEntries,
		maximumRevisionTreeDepth,
		maximumRevisionOperationEntries,
		maximumRevisionOperationBytes,
	)
	if err != nil {
		panic(err)
	}
	return limits
}

type revisionCaptureBudget struct {
	limits  revisionCaptureLimits
	entries int
	bytes   int64
}

func newRevisionCaptureBudget(limits revisionCaptureLimits) (*revisionCaptureBudget, error) {
	if !limits.initialized {
		return nil, fmt.Errorf("mutation revision capture limits are uninitialized")
	}
	return &revisionCaptureBudget{limits: limits}, nil
}

func (budget *revisionCaptureBudget) beginTree() (*revisionTreeBudget, error) {
	if budget == nil || !budget.limits.initialized {
		return nil, fmt.Errorf("mutation revision capture budget is required")
	}
	return &revisionTreeBudget{operation: budget}, nil
}

func (budget *revisionCaptureBudget) remainingEntries() int {
	return budget.limits.maximumOperationEntries - budget.entries
}

func (budget *revisionCaptureBudget) remainingBytes() int64 {
	return budget.limits.maximumOperationBytes - budget.bytes
}

type revisionTreeBudget struct {
	operation *revisionCaptureBudget
	entries   int
}

func (budget *revisionTreeBudget) remainingEntries() int {
	return min(
		budget.operation.limits.maximumTreeEntries-budget.entries,
		budget.operation.remainingEntries(),
	)
}

func (budget *revisionTreeBudget) admitEntries(count int) error {
	if budget == nil || budget.operation == nil || count < 0 {
		return fmt.Errorf("mutation revision tree entry budget is invalid")
	}
	treeObserved := budget.entries + count
	if treeObserved > budget.operation.limits.maximumTreeEntries {
		return RevisionLimitError{
			scope: revisionLimitTree, resource: revisionLimitEntries,
			limit: int64(budget.operation.limits.maximumTreeEntries), observed: int64(treeObserved),
		}
	}
	operationObserved := budget.operation.entries + count
	if operationObserved > budget.operation.limits.maximumOperationEntries {
		return RevisionLimitError{
			scope: revisionLimitOperation, resource: revisionLimitEntries,
			limit: int64(budget.operation.limits.maximumOperationEntries), observed: int64(operationObserved),
		}
	}
	budget.entries = treeObserved
	budget.operation.entries = operationObserved
	return nil
}

func (budget *revisionTreeBudget) admitDirectoryDepth(depth int) error {
	if budget == nil || budget.operation == nil || depth < 0 {
		return fmt.Errorf("mutation revision tree depth budget is invalid")
	}
	if depth > budget.operation.limits.maximumTreeDepth {
		return RevisionLimitError{
			scope: revisionLimitTree, resource: revisionLimitDepth,
			limit: int64(budget.operation.limits.maximumTreeDepth), observed: int64(depth),
		}
	}
	return nil
}

func (budget *revisionTreeBudget) remainingBytes() int64 {
	return budget.operation.remainingBytes()
}

func (budget *revisionTreeBudget) admitBytes(count int64) error {
	if budget == nil || budget.operation == nil || count < 0 {
		return fmt.Errorf("mutation revision byte budget is invalid")
	}
	observed := budget.operation.bytes + count
	if observed > budget.operation.limits.maximumOperationBytes {
		return RevisionLimitError{
			scope: revisionLimitOperation, resource: revisionLimitBytes,
			limit: budget.operation.limits.maximumOperationBytes, observed: observed,
		}
	}
	budget.operation.bytes = observed
	return nil
}
