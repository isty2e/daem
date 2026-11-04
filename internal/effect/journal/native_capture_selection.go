package journal

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
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

func (batch NativeDocumentObservationBatch) captureMembers(ctx context.Context, after durable.Snapshot, orders ...pipackage.OrderObservation) (NativeDocumentObservationBatch, error) {
	indexes, err := indexDocumentSnapshots(ctx, batch.before, after)
	if err != nil {
		return NativeDocumentObservationBatch{}, err
	}
	orderedDocuments := make(map[aggregate.DocumentAddress]bool)
	for _, order := range orders {
		if err := order.Validate(); err != nil {
			return NativeDocumentObservationBatch{}, err
		}
		if !order.Changed() {
			continue
		}
		alias, err := piOrderDocumentAlias(order.Scope())
		if err != nil {
			return NativeDocumentObservationBatch{}, err
		}
		orderedDocuments[alias] = false
	}
	selected := batch
	selected.namespaces = nil
	selected.originals = nil
	for index, namespace := range batch.namespaces {
		member, err := indexes.hasMembers(ctx, namespace.Addresses())
		if err != nil {
			return NativeDocumentObservationBatch{}, err
		}
		ordered := false
		for _, alias := range namespace.Addresses() {
			if _, present := orderedDocuments[alias]; present {
				ordered = true
				orderedDocuments[alias] = true
			}
		}
		if !member && !ordered {
			continue
		}
		selected.namespaces = append(selected.namespaces, namespace)
		selected.originals = append(selected.originals, batch.originals[index])
	}
	for _, observed := range orderedDocuments {
		if !observed {
			return NativeDocumentObservationBatch{}, fmt.Errorf("Pi order requires its observed settings namespace")
		}
	}
	return selected, ctx.Err()
}

// CaptureMembers derives actual actors and pending metadata from the retained
// snapshots, Registry and selected order work, without observation or publication.
func (batch NativeDocumentObservationBatch) CaptureMembers(after durable.Snapshot, operationID string, orders ...pipackage.OrderObservation) (NativeDocumentObservationBatch, []NativeDocumentMember, error) {
	ctx := context.Background()
	selected, err := batch.captureMembers(ctx, after, orders...)
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
