package observation

import (
	"context"
	"fmt"
	"slices"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// Member projects prospective membership, not live protection
// metadata or write authority. Its namespace roots remain borrowed from the batch.
type Member struct {
	namespace journalnamespace.DocumentNamespace
	bindings  []ownership.DocumentBindingIntent
	scope     ownership.DocumentScope
	promoting bool
}

func (member Member) Namespace() journalnamespace.DocumentNamespace {
	return member.namespace
}

func (member Member) BindingIntents() []ownership.DocumentBindingIntent {
	return slices.Clone(member.bindings)
}

func (member Member) RegistryScope() ownership.DocumentScope { return member.scope }

func (member Member) RequiresPromotion() bool { return member.promoting }

func (batch Batch) SelectCaptureMembers(ctx context.Context, after durable.Snapshot, orders ...relationhost.SelectedDocumentEdit) (Batch, error) {
	if err := batch.Validate(); err != nil {
		return Batch{}, err
	}
	indexes, err := journaldocument.IndexSnapshotsForClaims(ctx, batch.before, after, journaldocument.NativeClaims)
	if err != nil {
		return Batch{}, err
	}
	orderedDocuments := make(map[aggregate.DocumentAddress]bool)
	for _, order := range orders {
		if err := order.Validate(); err != nil {
			return Batch{}, err
		}
		if !order.Changed() {
			continue
		}
		alias := order.DocumentAddress()
		orderedDocuments[alias] = false
	}
	selected := batch
	selected.namespaces = nil
	selected.originals = nil
	for index, namespace := range batch.namespaces {
		member, err := indexes.HasMembers(ctx, namespace.Addresses())
		if err != nil {
			return Batch{}, err
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
			return Batch{}, fmt.Errorf("Pi order requires its observed settings namespace")
		}
	}
	return selected, ctx.Err()
}

// CaptureMembers derives actual actors and pending metadata from the retained
// snapshots, Registry and selected order work, without observation or publication.
func (batch Batch) CaptureMembers(after durable.Snapshot, operationID string, orders ...relationhost.SelectedDocumentEdit) (Batch, []Member, error) {
	ctx := context.Background()
	selected, err := batch.SelectCaptureMembers(ctx, after, orders...)
	if err != nil {
		return Batch{}, nil, err
	}
	input := nativeCaptureOwnershipInput{
		owner: batch.owner, operationID: operationID, before: batch.before, after: after,
		namespaces: selected.namespaces, limits: batch.limits, budget: batch.budget,
	}
	members, err := input.prepareFromRegistry(ctx, batch.registry)
	if err != nil {
		return Batch{}, nil, err
	}
	result := make([]Member, 0, len(members))
	for index, member := range members {
		scope, err := journalnamespace.RegistryScope([]journalnamespace.DocumentNamespace{selected.namespaces[index]})
		if err != nil {
			return Batch{}, nil, err
		}
		promoting := len(member.provisional) != 0
		for _, binding := range member.bindings {
			if _, exact := binding.KnownAfter(); !exact {
				promoting = true
			}
		}
		result = append(result, Member{
			namespace: selected.namespaces[index], bindings: slices.Clone(member.bindings), scope: scope, promoting: promoting,
		})
	}
	return selected, result, nil
}
