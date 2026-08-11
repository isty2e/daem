package recovery

import "fmt"

type scratchCleanupDisposition uint8

const (
	scratchCleanupPending scratchCleanupDisposition = iota
	scratchCleanupNotApplicable
	scratchCleanupTransferred
)

// ReserveScratchPathComponents charges path work used only to bind and clean
// the process-private rollback stage after host execution.
func (budget *PhysicalWorkBudget) ReserveScratchPathComponents(count int) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.scratchCleanupDisposition != scratchCleanupPending || budget.generalExecutionBegun {
		return fmt.Errorf("recovery scratch cleanup capacity was already transferred")
	}
	before := budget.pathComponents
	if err := budget.AdmitPathComponents(count); err != nil {
		return err
	}
	budget.reservedScratchPathComponents += budget.pathComponents - before
	return nil
}

// ReserveScratchCleanup charges both exact recursive storage passes and one
// overflow-name probe per pass. The probes detect growth but do not enlarge the
// semantic cleanup ceiling returned to storage.
func (budget *PhysicalWorkBudget) ReserveScratchCleanup(work ArtifactWork) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.scratchCleanupDisposition != scratchCleanupPending || budget.generalExecutionBegun {
		return fmt.Errorf("recovery scratch cleanup capacity was already transferred")
	}
	if budget.scratchCleanupReserved {
		return fmt.Errorf("recovery scratch cleanup capacity was already reserved")
	}
	if work.entries < 0 || work.bytes < 0 {
		return fmt.Errorf("recovery scratch cleanup work must not be negative")
	}
	if budget.RemainingEntries() < 2 ||
		work.entries > (budget.RemainingEntries()-2)/2 ||
		work.bytes > budget.RemainingBytes()/2 {
		return fmt.Errorf("recovery scratch cleanup exceeds operation capacity")
	}
	reservedEntries := 2 * (work.entries + 1)
	reservedBytes := 2 * work.bytes
	if err := budget.AdmitTree(ArtifactWork{entries: reservedEntries, bytes: reservedBytes}); err != nil {
		return err
	}
	budget.reservedScratchEntries = work.entries
	budget.reservedScratchBytes = work.bytes
	budget.scratchCleanupReserved = true
	return nil
}

// BeginReservedScratchCleanup transfers the exact path capacity and returns
// the semantic tree ceiling reserved before the first host effect.
func (budget *PhysicalWorkBudget) BeginReservedScratchCleanup() (
	*PhysicalWorkBudget,
	ArtifactWork,
	error,
) {
	if budget == nil {
		return nil, ArtifactWork{}, fmt.Errorf("physical work budget is required")
	}
	if budget.scratchCleanupDisposition != scratchCleanupPending {
		return nil, ArtifactWork{}, fmt.Errorf("recovery scratch cleanup capacity was already transferred")
	}
	if budget.generalExecutionBegun {
		return nil, ArtifactWork{}, fmt.Errorf("recovery scratch cleanup must be transferred before general execution")
	}
	if !budget.scratchCleanupReserved {
		return nil, ArtifactWork{}, fmt.Errorf("recovery scratch cleanup capacity was not reserved")
	}
	budget.scratchCleanupDisposition = scratchCleanupTransferred
	return &PhysicalWorkBudget{
			pathComponentLimit: budget.reservedScratchPathComponents,
		}, ArtifactWork{
			entries: budget.reservedScratchEntries,
			bytes:   budget.reservedScratchBytes,
		}, nil
}

// ConcludeScratchCleanupNotApplicable closes the scratch-cleanup phase when
// recovery has no rollback stage. It transfers no path or tree capacity.
func (budget *PhysicalWorkBudget) ConcludeScratchCleanupNotApplicable() error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.scratchCleanupDisposition != scratchCleanupPending {
		return fmt.Errorf("recovery scratch cleanup disposition was already established")
	}
	if budget.scratchCleanupReserved {
		return fmt.Errorf("reserved recovery scratch cleanup cannot be marked not applicable")
	}
	if budget.generalExecutionBegun {
		return fmt.Errorf("recovery scratch cleanup disposition must be established before general execution")
	}
	budget.scratchCleanupDisposition = scratchCleanupNotApplicable
	return nil
}
