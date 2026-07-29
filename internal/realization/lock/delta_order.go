package lock

import (
	"sort"

	hostrelation "github.com/isty2e/daem/internal/realization/relation"
)

// OrderDeltaEntry describes one changed or unchanged class-relative order.
type OrderDeltaEntry struct {
	Status DeltaStatus
	Key    hostrelation.OrderClassID
	Before hostrelation.RelationOrderConstraint
	After  hostrelation.RelationOrderConstraint
}

func buildOrderDelta(before File, after File) []OrderDeltaEntry {
	beforeEntries := orderConstraintMap(before)
	afterEntries := orderConstraintMap(after)
	keys := make([]hostrelation.OrderClassID, 0, len(beforeEntries)+len(afterEntries))
	seen := make(
		map[hostrelation.OrderClassID]struct{},
		len(beforeEntries)+len(afterEntries),
	)
	for key := range beforeEntries {
		keys = appendOrderClassKey(keys, seen, key)
	}
	for key := range afterEntries {
		keys = appendOrderClassKey(keys, seen, key)
	}
	sort.Slice(keys, func(left int, right int) bool {
		return keys[left] < keys[right]
	})

	entries := make([]OrderDeltaEntry, 0, len(keys))
	for _, key := range keys {
		beforeConstraint, hadBefore := beforeEntries[key]
		afterConstraint, hasAfter := afterEntries[key]
		switch {
		case !hadBefore && hasAfter:
			entries = append(entries, OrderDeltaEntry{
				Status: DeltaStatusAdded,
				Key:    key,
				After:  afterConstraint,
			})
		case hadBefore && !hasAfter:
			entries = append(entries, OrderDeltaEntry{
				Status: DeltaStatusRemoved,
				Key:    key,
				Before: beforeConstraint,
			})
		case beforeConstraint.Equal(afterConstraint):
			entries = append(entries, OrderDeltaEntry{
				Status: DeltaStatusUnchanged,
				Key:    key,
				Before: beforeConstraint,
				After:  afterConstraint,
			})
		default:
			entries = append(entries, OrderDeltaEntry{
				Status: DeltaStatusChanged,
				Key:    key,
				Before: beforeConstraint,
				After:  afterConstraint,
			})
		}
	}
	return entries
}

// OrderEntries returns a stable copy of class-relative order delta entries.
func (delta Delta) OrderEntries() []OrderDeltaEntry {
	return append([]OrderDeltaEntry(nil), delta.orderEntries...)
}

// OrderEntriesWithStatus returns order entries with the selected status.
func (delta Delta) OrderEntriesWithStatus(status DeltaStatus) []OrderDeltaEntry {
	entries := make([]OrderDeltaEntry, 0)
	for _, entry := range delta.orderEntries {
		if entry.Status == status {
			entries = append(entries, entry)
		}
	}
	return entries
}

// OrderCounts returns class-relative order status counts.
func (delta Delta) OrderCounts() DeltaCounts {
	counts := DeltaCounts{}
	for _, entry := range delta.orderEntries {
		switch entry.Status {
		case DeltaStatusAdded:
			counts.Added++
		case DeltaStatusRemoved:
			counts.Removed++
		case DeltaStatusChanged:
			counts.Changed++
		case DeltaStatusUnchanged:
			counts.Unchanged++
		}
	}
	return counts
}

func orderConstraintMap(
	file File,
) map[hostrelation.OrderClassID]hostrelation.RelationOrderConstraint {
	constraints := file.Locked.OrderConstraints()
	entries := make(
		map[hostrelation.OrderClassID]hostrelation.RelationOrderConstraint,
		len(constraints),
	)
	for _, constraint := range constraints {
		entries[constraint.ClassID()] = constraint
	}
	return entries
}

func appendOrderClassKey(
	keys []hostrelation.OrderClassID,
	seen map[hostrelation.OrderClassID]struct{},
	key hostrelation.OrderClassID,
) []hostrelation.OrderClassID {
	if _, exists := seen[key]; exists {
		return keys
	}
	seen[key] = struct{}{}
	return append(keys, key)
}
