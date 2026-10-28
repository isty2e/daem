package journal

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func (batch NativeDocumentObservationBatch) captureMembers(ctx context.Context, after durable.Snapshot) (NativeDocumentObservationBatch, error) {
	indexes, err := indexDocumentSnapshots(ctx, batch.before, after)
	if err != nil {
		return NativeDocumentObservationBatch{}, err
	}
	selected := batch
	selected.namespaces = nil
	selected.originals = nil
	for index, namespace := range batch.namespaces {
		member, err := indexes.hasMembers(ctx, namespace.Addresses())
		if err != nil {
			return NativeDocumentObservationBatch{}, err
		}
		if !member {
			continue
		}
		selected.namespaces = append(selected.namespaces, namespace)
		selected.originals = append(selected.originals, batch.originals[index])
	}
	return selected, ctx.Err()
}

// CaptureMembers derives actual actors and pending metadata from the retained
// snapshots and Registry. It performs no filesystem observation or publication.
func (batch NativeDocumentObservationBatch) CaptureMembers(after durable.Snapshot, operationID string) (NativeDocumentObservationBatch, []bool, error) {
	ctx := context.Background()
	selected, err := batch.captureMembers(ctx, after)
	if err != nil {
		return NativeDocumentObservationBatch{}, nil, err
	}
	input := nativeCaptureOwnershipInput{
		owner: batch.owner, operationID: operationID, before: batch.before, after: after,
		namespaces: selected.namespaces, limits: batch.limits, budget: batch.budget,
	}
	members, err := input.prepareFromRegistry(ctx, batch.registry)
	if err != nil {
		return NativeDocumentObservationBatch{}, nil, err
	}
	promoting := make([]bool, len(members))
	for index, member := range members {
		promoting[index] = len(member.provisional) != 0
		for _, binding := range member.bindings {
			if _, exact := binding.KnownAfter(); !exact {
				promoting[index] = true
			}
		}
	}
	return selected, promoting, nil
}

func (indexes documentSnapshotIndexes) hasMembers(ctx context.Context, aliases []aggregate.DocumentAddress) (bool, error) {
	before, err := indexes.before.documentSubjects(ctx, aliases)
	if err != nil {
		return false, err
	}
	after, err := indexes.after.documentSubjects(ctx, aliases)
	if err != nil {
		return false, err
	}
	return len(before)+len(after) != 0, nil
}
