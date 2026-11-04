package journal

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

// NativeDocumentObservationBatch pairs evidence with the operation's borrowed
// roots. Only observation and selection can construct a matching pair.
type NativeDocumentObservationBatch struct {
	evidence journalobservation.Batch
	roots    journalnamespace.Roots
	owner    stateauthority.Authority
	budget   *physicalwork.Budget
}

func ObserveNativeDocuments(ctx context.Context, input journalobservation.Input) (NativeDocumentObservationBatch, error) {
	evidence, err := journalobservation.Observe(ctx, input)
	if err != nil {
		return NativeDocumentObservationBatch{}, err
	}
	return NativeDocumentObservationBatch{evidence: evidence, roots: input.Roots, owner: input.Owner, budget: input.Budget}, nil
}

// Evidence returns observation facts, not the operation's retained root set.
func (batch NativeDocumentObservationBatch) Evidence() journalobservation.Batch {
	return batch.evidence
}

// CaptureMembers selects metadata while preserving the original borrowed roots;
// it neither requalifies those roots nor acquires publication authority.
func (batch NativeDocumentObservationBatch) CaptureMembers(after durable.Snapshot, operationID string, orders ...pipackage.OrderObservation) (NativeDocumentObservationBatch, []journalobservation.Member, error) {
	selected, members, err := batch.evidence.CaptureMembers(after, operationID, orders...)
	if err != nil {
		return NativeDocumentObservationBatch{}, nil, err
	}
	return NativeDocumentObservationBatch{evidence: selected, roots: batch.roots, owner: batch.owner, budget: batch.budget}, members, nil
}

func (batch NativeDocumentObservationBatch) captureMembers(ctx context.Context, after durable.Snapshot, orders ...pipackage.OrderObservation) (NativeDocumentObservationBatch, error) {
	selected, err := batch.evidence.SelectCaptureMembers(ctx, after, orders...)
	if err != nil {
		return NativeDocumentObservationBatch{}, err
	}
	return NativeDocumentObservationBatch{evidence: selected, roots: batch.roots, owner: batch.owner, budget: batch.budget}, nil
}

func (batch NativeDocumentObservationBatch) prepareOwnership(ctx context.Context, after durable.Snapshot, operationID string) ([]journalobservation.CaptureInput, error) {
	return batch.evidence.PrepareOwnership(ctx, batch.roots, after, operationID)
}

func (batch NativeDocumentObservationBatch) validateOriginals(ctx context.Context) error {
	return batch.evidence.ValidateOriginals(ctx, batch.roots)
}
