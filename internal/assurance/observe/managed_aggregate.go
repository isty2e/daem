package observe

import (
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

// AggregateEvidence is one fresh read of a selected aggregate document. It
// carries no desired policy, managed authority, or mutation permission.
type AggregateEvidence struct {
	document aggregate.Document
	snapshot aggregate.Snapshot
	fileMode os.FileMode
}

// AggregateObservationFailure is one fresh document read whose selected codec
// observation could not produce exact AggregateEvidence.
type AggregateObservationFailure struct {
	document    aggregate.Document
	selection   aggregate.Selection
	reason      aggregate.CodecFailureReason
	contentPath aggregate.ContentPath
}

// AggregatePreconditionEvidence is one fresh observation of a static
// codec-profile operation precondition.
type AggregatePreconditionEvidence struct {
	owner        aggregate.DocumentAddress
	precondition aggregate.OperationPrecondition
	satisfied    bool
}

// NewAggregateEvidence constructs one codec-correlated document observation.
func NewAggregateEvidence(
	document aggregate.Document,
	snapshot aggregate.Snapshot,
	fileMode os.FileMode,
) (AggregateEvidence, error) {
	if err := document.Validate(); err != nil {
		return AggregateEvidence{}, err
	}
	selection, err := snapshot.Selection()
	if err != nil {
		return AggregateEvidence{}, fmt.Errorf("aggregate evidence snapshot: %w", err)
	}
	if document.Exists() != snapshot.DocumentExisted() {
		return AggregateEvidence{}, fmt.Errorf("aggregate evidence document presence differs from codec snapshot")
	}
	if document.Exists() {
		if !fileMode.IsRegular() {
			return AggregateEvidence{}, fmt.Errorf("aggregate evidence requires a regular document")
		}
	} else if fileMode != 0 {
		return AggregateEvidence{}, fmt.Errorf("absent aggregate evidence cannot carry file mode")
	}
	if selection.CodecContractID() == "" {
		return AggregateEvidence{}, fmt.Errorf("aggregate evidence codec contract is required")
	}
	return AggregateEvidence{document: document, snapshot: snapshot, fileMode: fileMode}, nil
}

func (evidence AggregateEvidence) Document() aggregate.Document { return evidence.document }
func (evidence AggregateEvidence) Snapshot() aggregate.Snapshot { return evidence.snapshot }
func (evidence AggregateEvidence) FileMode() os.FileMode        { return evidence.fileMode }
func (evidence AggregateEvidence) Address() aggregate.DocumentAddress {
	selection, _ := evidence.snapshot.Selection()
	return selection.DocumentAddress()
}

// NewAggregateObservationFailure constructs one redaction-safe failed codec
// observation over a complete selection.
func NewAggregateObservationFailure(
	document aggregate.Document,
	selection aggregate.Selection,
	fileMode os.FileMode,
	failure *aggregate.CodecFailure,
) (AggregateObservationFailure, error) {
	if failure == nil {
		return AggregateObservationFailure{}, fmt.Errorf("aggregate observation failure is required")
	}
	if err := document.Validate(); err != nil {
		return AggregateObservationFailure{}, err
	}
	canonicalSelection, err := aggregate.NewSelection(selection.Contracts())
	if err != nil {
		return AggregateObservationFailure{}, err
	}
	if document.Exists() {
		if !fileMode.IsRegular() {
			return AggregateObservationFailure{}, fmt.Errorf("aggregate failed observation requires a regular document")
		}
	} else if fileMode != 0 {
		return AggregateObservationFailure{}, fmt.Errorf("absent aggregate failed observation cannot carry file mode")
	}
	canonicalFailure, err := aggregate.NewCodecFailure(
		failure.Reason(),
		failure.ContentPath(),
	)
	if err != nil {
		return AggregateObservationFailure{}, err
	}
	return AggregateObservationFailure{
		document:    document,
		selection:   canonicalSelection,
		reason:      canonicalFailure.Reason(),
		contentPath: canonicalFailure.ContentPath(),
	}, nil
}

func (failure AggregateObservationFailure) Address() aggregate.DocumentAddress {
	return failure.selection.DocumentAddress()
}

func (failure AggregateObservationFailure) Document() aggregate.Document {
	return failure.document
}

func (failure AggregateObservationFailure) Selection() aggregate.Selection {
	selection, _ := aggregate.NewSelection(failure.selection.Contracts())
	return selection
}

func (failure AggregateObservationFailure) Reason() aggregate.CodecFailureReason {
	return failure.reason
}

func (failure AggregateObservationFailure) ContentPath() aggregate.ContentPath {
	return failure.contentPath
}

func (failure AggregateObservationFailure) Error() string {
	codecFailure, _ := aggregate.NewCodecFailure(
		failure.reason,
		failure.contentPath,
	)
	return codecFailure.Error()
}

// NewAggregatePreconditionEvidence constructs one codec-correlated fresh
// precondition observation.
func NewAggregatePreconditionEvidence(
	owner aggregate.DocumentAddress,
	precondition aggregate.OperationPrecondition,
	satisfied bool,
) (AggregatePreconditionEvidence, error) {
	if err := owner.Validate(); err != nil {
		return AggregatePreconditionEvidence{}, fmt.Errorf("aggregate precondition owner: %w", err)
	}
	if err := precondition.Validate(); err != nil {
		return AggregatePreconditionEvidence{}, err
	}
	document := precondition.DocumentAddress()
	if owner.Target() != document.Target() || owner.Scope() != document.Scope() {
		return AggregatePreconditionEvidence{}, fmt.Errorf("aggregate precondition scope differs from its owner")
	}
	if owner == document {
		return AggregatePreconditionEvidence{}, fmt.Errorf("aggregate precondition must not target its owner document")
	}
	return AggregatePreconditionEvidence{
		owner: owner, precondition: precondition, satisfied: satisfied,
	}, nil
}

func (evidence AggregatePreconditionEvidence) Owner() aggregate.DocumentAddress {
	return evidence.owner
}

func (evidence AggregatePreconditionEvidence) Precondition() aggregate.OperationPrecondition {
	return evidence.precondition
}

func (evidence AggregatePreconditionEvidence) Satisfied() bool {
	return evidence.satisfied
}
