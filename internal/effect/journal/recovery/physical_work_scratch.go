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

// ReserveScratchCleanup charges the complete rooted-cleanup storage and
// destination-parent validation envelope for the process-private rollback
// stage. The semantic cleanup ceiling remains distinct from execution work.
func (budget *PhysicalWorkBudget) ReserveScratchCleanup(
	work ArtifactWork,
	parentValidationWork int,
) error {
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
	envelope, err := rootedCleanupWorkEnvelope(work, true)
	if err != nil {
		return err
	}
	reserved := *budget
	if err := reserved.AdmitTree(ArtifactWork{
		entries: envelope.EntryWork(),
		bytes:   envelope.ByteWork(),
	}); err != nil {
		return fmt.Errorf("recovery scratch cleanup exceeds operation capacity: %w", err)
	}
	reserved.reservedScratchEntries += envelope.EntryWork()
	reserved.reservedScratchBytes += envelope.ByteWork()
	pathWork, err := envelope.PathWork(parentValidationWork)
	if err != nil {
		return fmt.Errorf("reserve recovery scratch cleanup namespace: %w", err)
	}
	if err := reserved.admitAggregatePathComponents(pathWork); err != nil {
		return fmt.Errorf("reserve recovery scratch cleanup namespace: %w", err)
	}
	reserved.reservedScratchPathComponents += pathWork
	reserved.scratchCleanupWork = work
	reserved.scratchCleanupReserved = true
	*budget = reserved
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
		entryLimit:         budget.reservedScratchEntries,
		byteLimit:          budget.reservedScratchBytes,
	}, budget.scratchCleanupWork, nil
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
