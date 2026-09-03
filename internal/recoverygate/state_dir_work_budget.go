package recoverygate

import (
	"fmt"
	"sync"

	"github.com/isty2e/daem/internal/effect/fileset"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

const (
	// StateDir operations use one fixed aggregate envelope across planning,
	// revalidation, file-set census, and retained descendant persistence.
	defaultStateDirMaximumPhysicalDepth           = mutationfs.MaximumPhysicalPathDepth
	defaultStateDirMaximumPathComponentWork       = mutationfs.MaximumPhysicalPathComponentVisits
	defaultStateDirMaximumEntryWork               = 400_000
	defaultStateDirMaximumByteWork          int64 = 16 << 30
)

type stateDirPhysicalWorkBudget = fileset.PhysicalWorkBudget

type stateDirOperationWorkBudget struct {
	mu      sync.Mutex
	paths   int
	entries int
	bytes   int64
}

func (budget *stateDirOperationWorkBudget) AdmitPathComponents(count int) error {
	return budget.AdmitPhysicalWork(count, 0, 0)
}

func (budget *stateDirOperationWorkBudget) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	if budget == nil {
		return fmt.Errorf("file-set state directory physical work budget is required")
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if pathComponents < 0 || entries < 0 || bytes < 0 {
		return fmt.Errorf("file-set state directory physical work is invalid")
	}
	if pathComponents > defaultStateDirMaximumPathComponentWork-budget.paths {
		return fmt.Errorf(
			"file-set state directory path work exceeds operation limit %d",
			defaultStateDirMaximumPathComponentWork,
		)
	}
	if entries > defaultStateDirMaximumEntryWork-budget.entries {
		return fmt.Errorf(
			"file-set state directory entry work exceeds operation limit %d",
			defaultStateDirMaximumEntryWork,
		)
	}
	if bytes > defaultStateDirMaximumByteWork-budget.bytes {
		return fmt.Errorf(
			"file-set state directory byte work exceeds operation limit %d",
			defaultStateDirMaximumByteWork,
		)
	}
	budget.paths += pathComponents
	budget.entries += entries
	budget.bytes += bytes
	return nil
}

type stateDirPhysicalWorkAdapter struct {
	mu      sync.Mutex
	path    rootedpath.PhysicalTraversalBudget
	entries int
	bytes   int64
}

func adaptStateDirPhysicalWorkBudget(
	budget rootedpath.PhysicalTraversalBudget,
) stateDirPhysicalWorkBudget {
	if physical, ok := budget.(stateDirPhysicalWorkBudget); ok {
		return physical
	}
	return &stateDirPhysicalWorkAdapter{path: budget}
}

func (budget *stateDirPhysicalWorkAdapter) AdmitPathComponents(count int) error {
	if budget == nil || budget.path == nil {
		return fmt.Errorf("file-set state directory physical work budget is required")
	}
	return budget.path.AdmitPathComponents(count)
}

func (budget *stateDirPhysicalWorkAdapter) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	if budget == nil || budget.path == nil {
		return fmt.Errorf("file-set state directory physical work budget is required")
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if pathComponents < 0 || entries < 0 || bytes < 0 {
		return fmt.Errorf("file-set state directory physical work is invalid")
	}
	if entries > defaultStateDirMaximumEntryWork-budget.entries {
		return fmt.Errorf(
			"file-set state directory entry work exceeds operation limit %d",
			defaultStateDirMaximumEntryWork,
		)
	}
	if bytes > defaultStateDirMaximumByteWork-budget.bytes {
		return fmt.Errorf(
			"file-set state directory byte work exceeds operation limit %d",
			defaultStateDirMaximumByteWork,
		)
	}
	if err := budget.path.AdmitPathComponents(pathComponents); err != nil {
		return err
	}
	budget.entries += entries
	budget.bytes += bytes
	return nil
}

type stateDirPhysicalWork struct {
	pathComponents int
	entries        int
	bytes          int64
}

func (work stateDirPhysicalWork) maximum(other stateDirPhysicalWork) stateDirPhysicalWork {
	return stateDirPhysicalWork{
		pathComponents: max(work.pathComponents, other.pathComponents),
		entries:        max(work.entries, other.entries),
		bytes:          max(work.bytes, other.bytes),
	}
}

func (work stateDirPhysicalWork) dominates(other stateDirPhysicalWork) bool {
	return work.pathComponents >= other.pathComponents &&
		work.entries >= other.entries &&
		work.bytes >= other.bytes
}

func (work stateDirPhysicalWork) add(other stateDirPhysicalWork) (stateDirPhysicalWork, error) {
	paths, err := checkedStateDirWorkAdd(work.pathComponents, other.pathComponents)
	if err != nil {
		return stateDirPhysicalWork{}, err
	}
	entries, err := checkedStateDirWorkAdd(work.entries, other.entries)
	if err != nil {
		return stateDirPhysicalWork{}, err
	}
	if work.bytes < 0 || other.bytes < 0 || other.bytes > int64(^uint64(0)>>1)-work.bytes {
		return stateDirPhysicalWork{}, fmt.Errorf("StateDir physical byte work overflows")
	}
	return stateDirPhysicalWork{
		pathComponents: paths,
		entries:        entries,
		bytes:          work.bytes + other.bytes,
	}, nil
}

func (work stateDirPhysicalWork) multiply(count int) (stateDirPhysicalWork, error) {
	paths, err := checkedStateDirWorkMultiply(work.pathComponents, count)
	if err != nil {
		return stateDirPhysicalWork{}, err
	}
	entries, err := checkedStateDirWorkMultiply(work.entries, count)
	if err != nil {
		return stateDirPhysicalWork{}, err
	}
	if count < 0 || work.bytes < 0 || work.bytes != 0 && int64(count) > int64(^uint64(0)>>1)/work.bytes {
		return stateDirPhysicalWork{}, fmt.Errorf("StateDir physical byte work overflows")
	}
	return stateDirPhysicalWork{
		pathComponents: paths,
		entries:        entries,
		bytes:          work.bytes * int64(count),
	}, nil
}
