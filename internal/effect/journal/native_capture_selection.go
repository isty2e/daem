package journal

import (
	"context"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// NativeDocumentMember projects prospective membership, not live protection
// metadata or write authority. Its namespace roots remain borrowed from the batch.
type NativeDocumentMember struct {
	namespace NativeDocumentNamespace
	bindings  []ownership.DocumentBindingIntent
	scope     ownership.DocumentScope
	promoting bool
}

func (member NativeDocumentMember) Namespace() NativeDocumentNamespace { return member.namespace }

func (member NativeDocumentMember) BindingIntents() []ownership.DocumentBindingIntent {
	return slices.Clone(member.bindings)
}

func (member NativeDocumentMember) RegistryScope() ownership.DocumentScope { return member.scope }

func (member NativeDocumentMember) RequiresPromotion() bool { return member.promoting }

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
func (batch NativeDocumentObservationBatch) CaptureMembers(after durable.Snapshot, operationID string) (NativeDocumentObservationBatch, []NativeDocumentMember, error) {
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
	result := make([]NativeDocumentMember, 0, len(members))
	for index, member := range members {
		scope, err := nativeNamespaceScope([]NativeDocumentNamespace{selected.namespaces[index]})
		if err != nil {
			return NativeDocumentObservationBatch{}, nil, err
		}
		promoting := len(member.provisional) != 0
		for _, binding := range member.bindings {
			if _, exact := binding.KnownAfter(); !exact {
				promoting = true
			}
		}
		result = append(result, NativeDocumentMember{
			namespace: selected.namespaces[index], bindings: slices.Clone(member.bindings), scope: scope, promoting: promoting,
		})
	}
	return selected, result, nil
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
