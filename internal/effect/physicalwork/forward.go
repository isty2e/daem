package physicalwork

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

// ForwardRemovalCapacity is operation-budgeted capacity for one removable
// whole-path state. It carries no filesystem identity or deletion authority.
type ForwardRemovalCapacity struct {
	maximum     workmeasure.ArtifactWork
	directory   bool
	initialized bool
}

// MaximumWork returns the bounded semantic work ceiling reserved for the state.
func (capacity ForwardRemovalCapacity) MaximumWork() workmeasure.ArtifactWork {
	return capacity.maximum
}

// Admits reports whether fresh candidate work remains within this reservation.
func (capacity ForwardRemovalCapacity) Admits(work workmeasure.ArtifactWork) bool {
	return capacity.initialized &&
		work.Entries() <= capacity.maximum.Entries() &&
		work.Bytes() <= capacity.maximum.Bytes()
}

// Envelope returns a read-only observation ceiling covering both already
// reserved capacities. It does not reserve additional operation work.
func (capacity ForwardRemovalCapacity) Envelope(
	other ForwardRemovalCapacity,
) (ForwardRemovalCapacity, error) {
	if !capacity.initialized || !other.initialized {
		return ForwardRemovalCapacity{}, fmt.Errorf("forward removal capacity is uninitialized")
	}
	maximum, err := workmeasure.NewArtifactWork(max(capacity.maximum.Entries(), other.maximum.Entries()), max(capacity.maximum.Bytes(), other.maximum.Bytes()))
	if err != nil {
		return ForwardRemovalCapacity{}, err
	}
	return ForwardRemovalCapacity{
		maximum:     maximum,
		directory:   capacity.directory || other.directory,
		initialized: true,
	}, nil
}

// BeginObservation opens the bounded local budget for one fresh candidate
// observation. Aggregate work was charged when the capacity was reserved.
func (capacity ForwardRemovalCapacity) BeginObservation() (*Budget, error) {
	if !capacity.initialized {
		return nil, fmt.Errorf("forward removal capacity is uninitialized")
	}
	entryLimit := capacity.maximum.Entries()
	if capacity.directory {
		entryLimit++
	}
	return &Budget{
		observationLimit: 1,
		entryLimit:       entryLimit,
		byteLimit:        capacity.maximum.Bytes(),
		probeByteLimit:   max(int64(1), capacity.maximum.Bytes()) - capacity.maximum.Bytes(),
	}, nil
}

// ReserveForwardRemoval charges one future fresh observation and the complete
// rooted-cleanup storage envelope. The observation that established work is
// charged separately by its producer.
func (budget *Budget) ReserveForwardRemoval(
	work workmeasure.ArtifactWork,
	directory bool,
	parentValidationWork int,
) (ForwardRemovalCapacity, error) {
	if budget == nil {
		return ForwardRemovalCapacity{}, fmt.Errorf("physical work budget is required")
	}
	if work.Entries() < 0 || work.Bytes() < 0 {
		return ForwardRemovalCapacity{}, fmt.Errorf("forward removal work must not be negative")
	}
	if work.Entries() > recovery.MaximumArtifactTreeEntries || work.Bytes() > recovery.MaximumArtifactTreeBytes {
		return ForwardRemovalCapacity{}, fmt.Errorf(
			"forward removal work exceeds per-tree limit entries=%d bytes=%d",
			recovery.MaximumArtifactTreeEntries,
			recovery.MaximumArtifactTreeBytes,
		)
	}
	if !directory && work.Entries() != 0 {
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
	observationEntries := work.Entries()
	if directory {
		observationEntries++
	}
	if err := reserved.admitTreeCounts(observationEntries, work.Bytes()); err != nil {
		return ForwardRemovalCapacity{}, fmt.Errorf("reserve forward removal observation: %w", err)
	}
	if err := reserved.admitTreeCounts(envelope.EntryWork(), envelope.ByteWork()); err != nil {
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
func (budget *Budget) ReserveForwardExecutionPathWork(
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
func (budget *Budget) BeginReservedForwardExecution() (*Budget, error) {
	if err := budget.requireRemovalAdmission(); err != nil {
		return nil, err
	}
	if budget.forwardExecutionBegun {
		return nil, fmt.Errorf("forward removal execution capacity was already transferred")
	}
	budget.forwardExecutionBegun = true
	return &Budget{
		observationLimit:   budget.reservedForwardObservations,
		pathComponentLimit: budget.reservedForwardPathComponents,
		entryLimit:         budget.reservedForwardEntries,
		byteLimit:          budget.reservedForwardBytes,
	}, nil
}
