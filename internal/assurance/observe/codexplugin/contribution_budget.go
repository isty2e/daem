package codexplugin

const (
	// MaximumContributionFileBytes bounds one Codex plugin contribution file snapshot.
	MaximumContributionFileBytes int64 = 4 << 20
	// MaximumObservationEntries bounds cache-version and skill-child names in one
	// ObserveConfiguredPluginContributions call.
	MaximumObservationEntries = 4096
	// MaximumObservationNameBytes bounds the aggregate name bytes counted toward
	// MaximumObservationEntries.
	MaximumObservationNameBytes = 1 << 20
	// MaximumObservationEntryNameBytes bounds one directory entry name.
	MaximumObservationEntryNameBytes = 4096
)

type observationBudget struct {
	entries   int
	nameBytes int
	exceeded  bool
}

func (budget *observationBudget) remainingEntries() int {
	if budget == nil || budget.exceeded || budget.entries >= MaximumObservationEntries {
		return 0
	}
	return MaximumObservationEntries - budget.entries
}

func (budget *observationBudget) exhaust() {
	if budget == nil {
		return
	}
	budget.exceeded = true
	budget.entries = MaximumObservationEntries
	budget.nameBytes = MaximumObservationNameBytes
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
