package record

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func (journal Native) completeDocuments(ctx context.Context, indexes journaldocument.SnapshotIndexes, inputs []journaldocument.StateGroupInput, result LoadedDocuments, codecs aggregate.CodecCatalog) (LoadedDocuments, error) {
	groups, err := journaldocument.ReconstructStateGroups(ctx, journal.owner, journal.before, journal.after, inputs)
	if err != nil {
		return LoadedDocuments{}, err
	}
	if err := journal.validateExpectedBaselines(ctx, indexes.After(), groups, codecs); err != nil {
		return LoadedDocuments{}, err
	}
	documentWork := false
	for _, group := range groups {
		comparison := group.Comparison()
		documentWork = documentWork || !comparison.BeforeDocument().Equal(comparison.ExpectedDocument()) || !comparison.BeforeState().SameRootMetadata(comparison.ExpectedState())
	}
	if err := RequireDocumentCommitWork(journal.before, journal.after, documentWork); err != nil {
		return LoadedDocuments{}, err
	}
	result.groups = groups
	return result, nil
}

func (journal Native) validateExpectedBaselines(ctx context.Context, after journaldocument.SnapshotIndex, groups []journaldocument.StateGroup, codecs aggregate.CodecCatalog) error {
	images := make([]journaldocument.ImageIdentity, 0, len(journal.record.DocumentGroups))
	for _, group := range journal.record.DocumentGroups {
		identity, err := group.Expected.identity()
		if err != nil {
			return err
		}
		images = append(images, identity)
	}
	return after.ValidateExpectedBaselines(ctx, groups, codecs, images)
}

func (journal Native) documentBindings(ctx context.Context) ([][]ownership.DocumentBindingIntent, error) {
	if ctx == nil {
		return nil, fmt.Errorf("document binding context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	equalState := journal.before.Equal(journal.after)
	groups := make([][]ownership.DocumentBindingIntent, 0, len(journal.record.DocumentGroups))
	placement, err := journal.documentPlacement(ctx)
	if err != nil {
		return nil, err
	}
	for index, group := range journal.record.DocumentGroups {
		sources := make(map[output.Destination]recoveryDocumentObservation)
		if journal.record.Version == native17Version {
			for _, alias := range group.namespace.aliases {
				before, present := placement.BeforeGroup(alias.address)
				if present && before != index {
					sources[alias.address.AggregateRoot()] = journal.record.DocumentGroups[before].CapturedTarget
				}
			}
		}
		intents, err := canonicalDocumentBindingIntentsFromSources(ctx, group.nativeDocumentBody, group.namespace.aliases, journal.owner, sources)
		if err != nil {
			return nil, err
		}
		for _, intent := range intents {
			for _, alias := range group.namespace.aliases {
				beforeGroup, _ := placement.BeforeGroup(alias.address)
				afterGroup, _ := placement.AfterGroup(alias.address)
				if alias.address.AggregateRoot() == intent.Destination() && beforeGroup != afterGroup && afterGroup != index {
					return nil, fmt.Errorf("carrier transfer binding intent must belong to its after group")
				}
			}
			changes, err := intent.ChangesBinding()
			if err != nil {
				return nil, err
			}
			if changes && equalState {
				return nil, fmt.Errorf("equal Snapshot operation cannot carry nontrivial document binding debt")
			}
		}
		groups = append(groups, intents)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

func (group recoveryDocumentGroup) canonicalBindingIntents(ctx context.Context, owner stateauthority.Authority) ([]ownership.DocumentBindingIntent, error) {
	aliases, err := legacyDocumentAliasFacts(group.Aliases)
	if err != nil {
		return nil, err
	}
	return canonicalDocumentBindingIntents(ctx, native16GroupBody(group), aliases, owner)
}

func (group nativeDocumentRecord) canonicalBindingIntents(ctx context.Context, owner stateauthority.Authority) ([]ownership.DocumentBindingIntent, error) {
	return canonicalDocumentBindingIntents(ctx, group.nativeDocumentBody, group.namespace.aliases, owner)
}

func canonicalDocumentBindingIntents(ctx context.Context, group nativeDocumentBody, aliases []documentAliasFacts, owner stateauthority.Authority) ([]ownership.DocumentBindingIntent, error) {
	return canonicalDocumentBindingIntentsFromSources(ctx, group, aliases, owner, nil)
}

func canonicalDocumentBindingIntentsFromSources(ctx context.Context, group nativeDocumentBody, aliases []documentAliasFacts, owner stateauthority.Authority, sources map[output.Destination]recoveryDocumentObservation) ([]ownership.DocumentBindingIntent, error) {
	beforeByDestination := make(map[string]recoveryDocumentObservation, len(aliases))
	for _, alias := range aliases {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		beforeByDestination[alias.address.AggregateRoot().String()] = alias.before
	}
	intents := make([]ownership.DocumentBindingIntent, 0, len(group.BindingIntents))
	for _, record := range group.BindingIntents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		destination, err := output.Parse(record.Destination)
		if err != nil {
			return nil, err
		}
		var source *recoveryDocumentObservation
		if paired, present := sources[destination]; present {
			source = &paired
		}
		intent, err := record.canonicalFromSource(source)
		if err != nil {
			return nil, err
		}
		if !intent.Owner().Equal(owner) {
			return nil, fmt.Errorf("document binding has another operation owner")
		}
		before, present := beforeByDestination[intent.Destination().String()]
		if !present {
			return nil, fmt.Errorf("document binding has no group alias")
		}
		if source != nil {
			before = *source
		}
		if binding, present := intent.Before().Get(); present {
			if !before.matchesRetainedExact(binding.PathAuthority()) {
				return nil, fmt.Errorf("document binding baseline differs from its captured before namespace")
			}
		}
		if err := requireCapturedBindingTarget(intent, group.CapturedTarget); err != nil {
			return nil, err
		}
		intents = append(intents, intent)
	}
	return intents, nil
}

func requireCapturedBindingTarget(intent ownership.DocumentBindingIntent, captured recoveryDocumentObservation) error {
	if value, known := intent.KnownAfter(); known {
		if binding, present := value.Get(); present {
			exact := binding.PathAuthority()
			before, retained := intent.Before().Get()
			if captured.Kind == "exact" || retained && before.PathAuthority().Equal(exact) {
				if !captured.matchesRetainedExact(exact) {
					return fmt.Errorf("document binding after differs from the captured target")
				}
			} else {
				provisional, err := captured.provisional()
				if err != nil || provisional.AdmitsExact(exact) != nil {
					return fmt.Errorf("promoted document binding is outside the captured namespace")
				}
			}
		}
	} else {
		pending, _ := intent.ProvisionalAfter()
		provisional, err := captured.provisional()
		if err != nil || !pending.Equal(provisional) {
			return fmt.Errorf("provisional document binding differs from the captured target")
		}
	}
	return nil
}

func (journal Native) documentClaims(ctx context.Context, indexes journaldocument.SnapshotIndexes, record nativeDocumentRecord, beforeAliases, afterAliases []aggregate.DocumentAddress) (ownershipmutation.DocumentClaimTransition, []journaldocument.ProvisionalAcquire, error) {
	before, err := indexes.Before().Footprints(ctx, beforeAliases)
	if err != nil {
		return ownershipmutation.DocumentClaimTransition{}, nil, err
	}
	after, err := indexes.After().Footprints(ctx, afterAliases)
	if err != nil {
		return ownershipmutation.DocumentClaimTransition{}, nil, err
	}
	if len(before) == 0 && len(after) == 0 {
		if record.ExclusiveClaims.Kind != "absent" {
			return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("document without claim membership carries exclusive claims")
		}
		return ownershipmutation.DocumentClaimTransition{}, nil, nil
	}
	switch record.ExclusiveClaims.Kind {
	case "exact":
		claims, err := record.ExclusiveClaims.canonicalObservedExact(journal.owner, journal.record.OperationID, record.CapturedTarget)
		if err != nil {
			return ownershipmutation.DocumentClaimTransition{}, nil, err
		}
		if !journaldocument.SameClaimFootprints(before, claims.Before()) || !journaldocument.SameClaimFootprints(after, claims.After()) {
			return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("document claims do not cover full Snapshot claim membership")
		}
		return claims, nil, nil
	case "provisional":
		provisional, err := record.CapturedTarget.provisional()
		if err != nil || len(before) != 0 {
			return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("provisional document claims require a newly acquired namespace")
		}
		acquires, err := record.ExclusiveClaims.canonicalProvisional(afterAliases, journal.owner, journal.record.OperationID, provisional, journal.namespace.claims)
		if err != nil {
			return ownershipmutation.DocumentClaimTransition{}, nil, err
		}
		footprints := make(map[output.Footprint]struct{}, len(acquires))
		for _, acquire := range acquires {
			footprints[acquire.Intent().Footprint()] = struct{}{}
		}
		if len(footprints) != len(after) {
			return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("provisional claims omit Snapshot claim membership")
		}
		for footprint := range after {
			if _, found := footprints[footprint]; !found {
				return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("provisional claim footprint differs from Snapshot claim membership")
			}
		}
		return ownershipmutation.DocumentClaimTransition{}, acquires, nil
	default:
		return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("document claim membership requires exact or provisional claims")
	}
}

// Reconstructed records retain all groups or none. They are not RecoverablePlan:
// active directory, live namespaces, registry phases and effect leases are absent.
type LoadedDocuments struct {
	groups      []journaldocument.StateGroup
	images      []LoadedImages
	claims      []ownershipmutation.DocumentClaimTransition
	provisional [][]journaldocument.ProvisionalAcquire
}

type LoadedDocument struct {
	group       journaldocument.StateGroup
	images      LoadedImages
	claims      ownershipmutation.DocumentClaimTransition
	provisional []journaldocument.ProvisionalAcquire
}

func (records LoadedDocuments) Len() int { return len(records.groups) }

func (records LoadedDocuments) At(index int) (LoadedDocument, bool) {
	if index < 0 || index >= len(records.groups) {
		return LoadedDocument{}, false
	}
	return LoadedDocument{group: records.groups[index], images: records.images[index], claims: records.claims[index], provisional: records.provisional[index]}, true
}

func (document LoadedDocument) Group() journaldocument.StateGroup { return document.group }

func (document LoadedDocument) Images() LoadedImages { return document.images }

func (document LoadedDocument) Claims() ownershipmutation.DocumentClaimTransition {
	return document.claims
}

func (document LoadedDocument) Provisional() []journaldocument.ProvisionalAcquire {
	return slices.Clone(document.provisional)
}
