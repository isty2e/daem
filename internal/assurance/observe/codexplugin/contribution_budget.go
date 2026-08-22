package codexplugin

const (
	// MaximumContributionFileBytes bounds one Codex plugin contribution file snapshot.
	MaximumContributionFileBytes int64 = 4 << 20
	// MaximumObservationEntries bounds plugin config keys, directory names,
	// manifest paths/keys, and emitted contribution rows in one Codex plugin
	// observation operation.
	MaximumObservationEntries = 4096
	// MaximumObservationNameBytes bounds the aggregate name bytes counted toward
	// MaximumObservationEntries.
	MaximumObservationNameBytes = 1 << 20
	// MaximumObservationEntryNameBytes bounds one charged name.
	MaximumObservationEntryNameBytes = 4096
	// MaximumObservationSnapshotBytes bounds aggregate snapshot bytes in one
	// Codex plugin observation operation, including the config file on the
	// shared diagnostic path.
	MaximumObservationSnapshotBytes int64 = 64 << 20
	// MaximumObservationPathComponents bounds relative descent from a retained
	// plugin directory descriptor.
	MaximumObservationPathComponents = 64

	maximumObservationJSONKeys     = MaximumObservationEntries
	maximumObservationJSONKeyBytes = MaximumObservationNameBytes
)

type observationBudget struct {
	entries       int
	nameBytes     int
	jsonKeys      int
	jsonKeyBytes  int
	snapshotBytes int64
	exceeded      bool
}

func (budget *observationBudget) remainingEntries() int {
	if budget == nil || budget.exceeded || budget.entries >= MaximumObservationEntries {
		return 0
	}
	return MaximumObservationEntries - budget.entries
}

func (budget *observationBudget) remainingSnapshotBytes() int64 {
	if budget == nil || budget.exceeded || budget.snapshotBytes >= MaximumObservationSnapshotBytes {
		return 0
	}
	return MaximumObservationSnapshotBytes - budget.snapshotBytes
}

func (budget *observationBudget) exhaust() {
	if budget == nil {
		return
	}
	budget.exceeded = true
	budget.entries = MaximumObservationEntries
	budget.nameBytes = MaximumObservationNameBytes
	budget.jsonKeys = maximumObservationJSONKeys
	budget.jsonKeyBytes = maximumObservationJSONKeyBytes
	budget.snapshotBytes = MaximumObservationSnapshotBytes
}

func (budget *observationBudget) consumeNames(names []string) bool {
	if budget == nil {
		return true
	}
	if budget.exceeded {
		return true
	}
	for _, name := range names {
		nameBytes := len(name)
		if nameBytes > MaximumObservationEntryNameBytes ||
			budget.entries+1 > MaximumObservationEntries ||
			budget.nameBytes+nameBytes > MaximumObservationNameBytes {
			budget.exhaust()
			return true
		}
		budget.entries++
		budget.nameBytes += nameBytes
	}
	return false
}

func (budget *observationBudget) consumeJSONObjectKey(key string) bool {
	if budget == nil {
		return true
	}
	keyBytes := len(key)
	if budget.exceeded ||
		keyBytes > MaximumObservationEntryNameBytes ||
		budget.jsonKeys+1 > maximumObservationJSONKeys ||
		budget.jsonKeyBytes+keyBytes > maximumObservationJSONKeyBytes {
		budget.exhaust()
		return true
	}
	budget.jsonKeys++
	budget.jsonKeyBytes += keyBytes
	return false
}

func (budget *observationBudget) consumeKeep() bool {
	if budget == nil {
		return true
	}
	if budget.exceeded || budget.entries+1 > MaximumObservationEntries {
		budget.exhaust()
		return true
	}
	budget.entries++
	return false
}

func (budget *observationBudget) consumeSnapshotBytes(n int64) bool {
	if budget == nil {
		return true
	}
	if n < 0 {
		n = 0
	}
	if budget.exceeded || budget.snapshotBytes+n > MaximumObservationSnapshotBytes {
		budget.exhaust()
		return true
	}
	budget.snapshotBytes += n
	return false
}

func (budget *observationBudget) snapshotLimit() int64 {
	remaining := budget.remainingSnapshotBytes()
	if remaining <= 0 {
		return 0
	}
	if remaining > MaximumContributionFileBytes {
		return MaximumContributionFileBytes
	}
	return remaining
}
