package recovery

import "fmt"

type retirementDisposition uint8

const (
	retirementPending retirementDisposition = iota
	retirementNotApplicable
	retirementTransferred
)

// ReserveRetirementPathComponents charges path work used only by the prepared
// journal-retirement continuation.
func (budget *PhysicalWorkBudget) ReserveRetirementPathComponents(count int) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.retirementDisposition != retirementPending || budget.generalExecutionBegun {
		return fmt.Errorf("journal retirement capacity was already transferred")
	}
	before := budget.pathComponents
	if err := budget.AdmitPathComponents(count); err != nil {
		return err
	}
	budget.reservedRetirementPathComponents += budget.pathComponents - before
	return nil
}

// AdmitRetirementDirectoryObservation charges one completed preparation-time
// snapshot and one possible overflow-name probe. The probe proves the exact
// semantic entry ceiling; it does not enlarge that ceiling.
func (budget *PhysicalWorkBudget) AdmitRetirementDirectoryObservation(work ArtifactWork) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.retirementDisposition != retirementPending || budget.generalExecutionBegun {
		return fmt.Errorf("journal retirement capacity was already transferred")
	}
	if budget.RemainingEntries() < 1 || work.entries > budget.RemainingEntries()-1 {
		return fmt.Errorf("journal retirement observation exceeds operation entry capacity")
	}
	if err := budget.AdmitTree(work); err != nil {
		return err
	}
	budget.entries++
	return nil
}

// ReserveRetirementDirectoryPasses charges exact semantic tree work and one
// overflow-name probe for every future validation or cleanup pass.
func (budget *PhysicalWorkBudget) ReserveRetirementDirectoryPasses(
	work ArtifactWork,
	passes int,
) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.retirementDisposition != retirementPending || budget.generalExecutionBegun {
		return fmt.Errorf("journal retirement capacity was already transferred")
	}
	if passes <= 0 {
		return fmt.Errorf("journal retirement directory pass count must be positive")
	}
	if work.entries > MaximumArtifactTreeEntries || work.bytes > MaximumArtifactTreeBytes {
		return fmt.Errorf("journal retirement tree exceeds per-tree capacity")
	}
	if budget.RemainingEntries() < passes ||
		work.entries > (budget.RemainingEntries()-passes)/passes ||
		work.bytes > budget.RemainingBytes()/int64(passes) {
		return fmt.Errorf("journal retirement tree passes exceed operation capacity")
	}
	entries := (work.entries + 1) * passes
	bytes := work.bytes * int64(passes)
	if err := budget.AdmitTree(ArtifactWork{entries: entries, bytes: bytes}); err != nil {
		return err
	}
	budget.reservedRetirementEntries += entries
	budget.reservedRetirementBytes += bytes
	return nil
}

// ReserveRetirementRootedCleanup charges the complete recursive cleanup and
// destination-parent validation envelope owned by a prepared retirement.
func (budget *PhysicalWorkBudget) ReserveRetirementRootedCleanup(
	work ArtifactWork,
	parentValidationWork int,
) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.retirementDisposition != retirementPending || budget.generalExecutionBegun {
		return fmt.Errorf("journal retirement capacity was already transferred")
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
		return fmt.Errorf("journal retirement cleanup exceeds operation capacity: %w", err)
	}
	reserved.reservedRetirementEntries += envelope.EntryWork()
	reserved.reservedRetirementBytes += envelope.ByteWork()
	pathWork, err := envelope.PathWork(parentValidationWork)
	if err != nil {
		return fmt.Errorf("reserve journal retirement cleanup namespace: %w", err)
	}
	if err := reserved.admitAggregatePathComponents(pathWork); err != nil {
		return fmt.Errorf("reserve journal retirement cleanup namespace: %w", err)
	}
	reserved.reservedRetirementPathComponents += pathWork
	*budget = reserved
	return nil
}

// ReserveRetirementArtifactWork charges predictable non-traversal work such
// as creating one canonical control tree. It reserves no overflow probe.
func (budget *PhysicalWorkBudget) ReserveRetirementArtifactWork(work ArtifactWork) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.retirementDisposition != retirementPending || budget.generalExecutionBegun {
		return fmt.Errorf("journal retirement capacity was already transferred")
	}
	if err := budget.AdmitTree(work); err != nil {
		return err
	}
	budget.reservedRetirementEntries += work.entries
	budget.reservedRetirementBytes += work.bytes
	return nil
}

// ReserveRetirementFilePasses charges exact regular-file bytes for future
// bounded reads or writes. Retirement records and journals are always
// non-empty, so no empty-file proof allowance is needed here.
func (budget *PhysicalWorkBudget) ReserveRetirementFilePasses(
	work ArtifactWork,
	passes int,
) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.retirementDisposition != retirementPending || budget.generalExecutionBegun {
		return fmt.Errorf("journal retirement capacity was already transferred")
	}
	if work.entries != 0 || work.bytes <= 0 || passes <= 0 {
		return fmt.Errorf("journal retirement file passes require positive file work and pass count")
	}
	if work.bytes > budget.RemainingBytes()/int64(passes) {
		return fmt.Errorf("journal retirement file passes exceed operation capacity")
	}
	bytes := work.bytes * int64(passes)
	if err := budget.AdmitTree(ArtifactWork{bytes: bytes}); err != nil {
		return err
	}
	budget.reservedRetirementBytes += bytes
	return nil
}

// BeginReservedRetirementExecution transfers only capacity reserved for one
// prepared retirement continuation.
func (budget *PhysicalWorkBudget) BeginReservedRetirementExecution() (*PhysicalWorkBudget, error) {
	if budget == nil {
		return nil, fmt.Errorf("physical work budget is required")
	}
	if budget.retirementDisposition != retirementPending {
		return nil, fmt.Errorf("journal retirement disposition was already established")
	}
	if budget.generalExecutionBegun {
		return nil, fmt.Errorf("journal retirement capacity must be transferred before general execution")
	}
	budget.retirementDisposition = retirementTransferred
	return &PhysicalWorkBudget{
		pathComponentLimit: budget.reservedRetirementPathComponents,
		entryLimit:         budget.reservedRetirementEntries,
		byteLimit:          budget.reservedRetirementBytes,
	}, nil
}

// ConcludeRetirementNotApplicable closes the retirement phase for internal
// operations that do not own an active or cleanup-only journal.
func (budget *PhysicalWorkBudget) ConcludeRetirementNotApplicable() error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.retirementDisposition != retirementPending {
		return fmt.Errorf("journal retirement disposition was already established")
	}
	if budget.reservedRetirementPathComponents != 0 ||
		budget.reservedRetirementEntries != 0 ||
		budget.reservedRetirementBytes != 0 {
		return fmt.Errorf("reserved journal retirement cannot be marked not applicable")
	}
	if budget.generalExecutionBegun {
		return fmt.Errorf("journal retirement disposition must be established before general execution")
	}
	budget.retirementDisposition = retirementNotApplicable
	return nil
}
