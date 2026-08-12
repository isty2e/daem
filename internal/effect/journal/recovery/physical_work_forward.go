package recovery

import "fmt"

// ForwardRemovalCapacity is operation-budgeted capacity for one removable
// whole-path state. It carries no filesystem identity or deletion authority.
type ForwardRemovalCapacity struct {
	maximum     ArtifactWork
	directory   bool
	initialized bool
}

// MaximumWork returns the bounded semantic work ceiling reserved for the state.
func (capacity ForwardRemovalCapacity) MaximumWork() ArtifactWork {
	return capacity.maximum
}

// Admits reports whether fresh candidate work remains within this reservation.
func (capacity ForwardRemovalCapacity) Admits(work ArtifactWork) bool {
	return capacity.initialized &&
		work.entries <= capacity.maximum.entries &&
		work.bytes <= capacity.maximum.bytes
}

// Envelope returns a read-only observation ceiling covering both already
// reserved capacities. It does not reserve additional operation work.
func (capacity ForwardRemovalCapacity) Envelope(
	other ForwardRemovalCapacity,
) (ForwardRemovalCapacity, error) {
	if !capacity.initialized || !other.initialized {
		return ForwardRemovalCapacity{}, fmt.Errorf("forward removal capacity is uninitialized")
	}
	return ForwardRemovalCapacity{
		maximum: ArtifactWork{
			entries: max(capacity.maximum.entries, other.maximum.entries),
			bytes:   max(capacity.maximum.bytes, other.maximum.bytes),
		},
		directory:   capacity.directory || other.directory,
		initialized: true,
	}, nil
}

// BeginObservation opens the bounded local budget for one fresh candidate
// observation. Aggregate work was charged when the capacity was reserved.
func (capacity ForwardRemovalCapacity) BeginObservation() (*PhysicalWorkBudget, error) {
	if !capacity.initialized {
		return nil, fmt.Errorf("forward removal capacity is uninitialized")
	}
	entryLimit := capacity.maximum.entries
	if capacity.directory {
		entryLimit++
	}
	return &PhysicalWorkBudget{
		observationLimit: 1,
		entryLimit:       entryLimit,
		byteLimit:        capacity.maximum.bytes,
		probeByteLimit:   max(int64(1), capacity.maximum.bytes) - capacity.maximum.bytes,
	}, nil
}

// ReserveForwardRemoval charges one future fresh observation and the complete
// rooted-cleanup storage envelope. The observation that established work is
// charged separately by its producer.
func (budget *PhysicalWorkBudget) ReserveForwardRemoval(
	work ArtifactWork,
	directory bool,
	parentValidationWork int,
) (ForwardRemovalCapacity, error) {
	if budget == nil {
		return ForwardRemovalCapacity{}, fmt.Errorf("physical work budget is required")
	}
	if work.entries < 0 || work.bytes < 0 {
		return ForwardRemovalCapacity{}, fmt.Errorf("forward removal work must not be negative")
	}
	if work.entries > MaximumArtifactTreeEntries || work.bytes > MaximumArtifactTreeBytes {
		return ForwardRemovalCapacity{}, fmt.Errorf(
			"forward removal work exceeds per-tree limit entries=%d bytes=%d",
			MaximumArtifactTreeEntries,
			MaximumArtifactTreeBytes,
		)
	}
	if !directory && work.entries != 0 {
		return ForwardRemovalCapacity{}, fmt.Errorf(
			"forward file removal work must not contain descendant entries",
		)
	}
	envelope, err := rootedCleanupWorkEnvelope(work, directory)
	if err != nil {
		return ForwardRemovalCapacity{}, err
	}
	reserved := *budget
	if err := reserved.AdmitObservation(); err != nil {
		return ForwardRemovalCapacity{}, err
	}
	observationEntries := work.entries
	if directory {
		observationEntries++
	}
	if err := reserved.AdmitTree(ArtifactWork{
		entries: observationEntries,
		bytes:   work.bytes,
	}); err != nil {
		return ForwardRemovalCapacity{}, fmt.Errorf("reserve forward removal observation: %w", err)
	}
	if err := reserved.AdmitTree(ArtifactWork{
		entries: envelope.EntryWork(),
		bytes:   envelope.ByteWork(),
	}); err != nil {
		return ForwardRemovalCapacity{}, fmt.Errorf("reserve forward rooted cleanup: %w", err)
	}
	reserved.reservedForwardEntries += envelope.EntryWork()
	reserved.reservedForwardBytes += envelope.ByteWork()
	pathWork, err := envelope.PathWork(parentValidationWork)
	if err != nil {
		return ForwardRemovalCapacity{}, fmt.Errorf("reserve forward rooted cleanup namespace: %w", err)
	}
	if err := reserved.admitAggregatePathComponents(pathWork); err != nil {
		return ForwardRemovalCapacity{}, fmt.Errorf("reserve forward rooted cleanup namespace: %w", err)
	}
	reserved.reservedForwardPathComponents += pathWork
	*budget = reserved
	return ForwardRemovalCapacity{
		maximum: work, directory: directory, initialized: true,
	}, nil
}

// ReserveForwardExecutionPathWork charges the namespace observation and every
// worst-case physical path traversal for one reachable forward-removal state.
// Candidate content work remains owned by ForwardRemovalCapacity.
func (budget *PhysicalWorkBudget) ReserveForwardExecutionPathWork(
	destinationDepths []int,
) error {
	if budget == nil {
		return fmt.Errorf("physical work budget is required")
	}
	if budget.forwardExecutionBegun {
		return fmt.Errorf("forward removal execution capacity was already transferred")
	}
	reserved := *budget
	for _, destinationDepth := range destinationDepths {
		if err := reserved.AdmitObservation(); err != nil {
			return err
		}
		reserved.reservedForwardObservations++
		// Execution acquires a fresh removal capability, validates the selected
		// namespace, may recapture both a retained ancestor and a newly appeared
		// parent, and revalidates the candidate path. Every chain is no deeper
		// than the already bound physical destination.
		passes := forwardRemovalCapabilityPasses +
			forwardRemovalNamespacePasses +
			forwardRemovalCandidatePasses
		for range passes {
			before := reserved.pathComponents
			if err := reserved.AdmitPathComponents(destinationDepth); err != nil {
				return err
			}
			reserved.reservedForwardPathComponents += reserved.pathComponents - before
		}
	}
	*budget = reserved
	return nil
}

// BeginReservedForwardExecution transfers only pre-effect namespace and path
// capacity into the forward-removal execution phase.
func (budget *PhysicalWorkBudget) BeginReservedForwardExecution() (*PhysicalWorkBudget, error) {
	if budget == nil {
		return nil, fmt.Errorf("physical work budget is required")
	}
	if budget.forwardExecutionBegun {
		return nil, fmt.Errorf("forward removal execution capacity was already transferred")
	}
	budget.forwardExecutionBegun = true
	return &PhysicalWorkBudget{
		observationLimit:   budget.reservedForwardObservations,
		pathComponentLimit: budget.reservedForwardPathComponents,
		entryLimit:         budget.reservedForwardEntries,
		byteLimit:          budget.reservedForwardBytes,
	}, nil
}
