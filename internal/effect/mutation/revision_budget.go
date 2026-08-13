package mutation

import (
	"errors"
	"fmt"
	"math"
)

const (
	maximumRevisionTreeEntries      = 100_000
	maximumRevisionTreeDepth        = 64
	maximumRevisionOperationEntries = 400_000
	maximumRevisionOperationBytes   = int64(16 << 30)
)

// RevisionLimitKind identifies one exhausted mutation revision dimension.
type RevisionLimitKind string

const (
	RevisionLimitTreeEntries      RevisionLimitKind = "tree_entries"
	RevisionLimitTreeDepth        RevisionLimitKind = "tree_depth"
	RevisionLimitOperationEntries RevisionLimitKind = "operation_entries"
	RevisionLimitOperationBytes   RevisionLimitKind = "operation_bytes"
)

// ErrRevisionLimitExceeded classifies deterministic mutation revision budget
// exhaustion.
var ErrRevisionLimitExceeded = errors.New("mutation revision limit exceeded")

// RevisionLimitError reports deterministic mutation-evidence exhaustion.
type RevisionLimitError struct {
	kind     RevisionLimitKind
	limit    int64
	observed int64
}

func (err *RevisionLimitError) Error() string {
	if err == nil {
		return ErrRevisionLimitExceeded.Error()
	}
	return fmt.Sprintf(
		"%s: %s observed=%d limit=%d",
		ErrRevisionLimitExceeded,
		err.kind,
		err.observed,
		err.limit,
	)
}

func (err *RevisionLimitError) Unwrap() error { return ErrRevisionLimitExceeded }

// Kind returns the exhausted mutation revision dimension.
func (err *RevisionLimitError) Kind() RevisionLimitKind {
	if err == nil {
		return ""
	}
	return err.kind
}

// Limit returns the exhausted ceiling.
func (err *RevisionLimitError) Limit() int64 {
	if err == nil {
		return 0
	}
	return err.limit
}

// Observed returns the first rejected work total.
func (err *RevisionLimitError) Observed() int64 {
	if err == nil {
		return 0
	}
	return err.observed
}

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
		return &RevisionLimitError{
			kind:  RevisionLimitTreeEntries,
			limit: int64(budget.operation.limits.maximumTreeEntries), observed: int64(treeObserved),
		}
	}
	operationObserved := budget.operation.entries + count
	if operationObserved > budget.operation.limits.maximumOperationEntries {
		return &RevisionLimitError{
			kind:  RevisionLimitOperationEntries,
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
		return &RevisionLimitError{
			kind:  RevisionLimitTreeDepth,
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
	if count > budget.operation.remainingBytes() {
		return &RevisionLimitError{
			kind:     RevisionLimitOperationBytes,
			limit:    budget.operation.limits.maximumOperationBytes,
			observed: budget.rejectedByteTotal(count),
		}
	}
	observed := budget.operation.bytes + count
	budget.operation.bytes = observed
	return nil
}

func (budget *revisionTreeBudget) rejectedByteTotal(count int64) int64 {
	if count > math.MaxInt64-budget.operation.bytes {
		return math.MaxInt64
	}
	return budget.operation.bytes + count
}
