package journal

import (
	"context"
	"fmt"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type nativeDocumentReadOptions struct {
	ordinaryCapabilities RootedCapabilityResolver
	artifacts            *rootedpath.CapturedRoot
	filesystem           mutationfs.RootedReader
	budget               *physicalwork.Budget
	limits               configdocument.Limits
	codecs               aggregate.CodecCatalog
}

// Reconstructed records retain all groups or none. They are not RecoverablePlan:
// active directory, live namespaces, registry phases and effect leases are absent.
type nativeDocumentRecords struct {
	groups      []journaldocument.StateGroup
	images      []loadedDocumentImages
	claims      []ownershipmutation.DocumentClaimTransition
	provisional [][]journaldocument.ProvisionalAcquire
}

func (journal nativeJournalSnapshots) loadDocuments(ctx context.Context, options nativeDocumentReadOptions) (nativeDocumentRecords, error) {
	if ctx == nil || options.artifacts == nil || options.filesystem == nil || options.budget == nil {
		return nativeDocumentRecords{}, fmt.Errorf("native document read context, artifact root, filesystem and shared budget are required")
	}
	session, err := newDocumentReconstruction(ctx, journal, options.limits, options.codecs, documentReconstructionLoaded, nil)
	if err != nil {
		return nativeDocumentRecords{}, err
	}
	for {
		demand, present, err := session.next(ctx)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		if !present {
			break
		}
		images, err := loadDocumentImages(ctx, demand.original, demand.expected, options.artifacts, options.filesystem, options.budget, recovery.MaximumRecoveryBackupFileBytes)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		if err := session.supplyLoadedImages(ctx, demand, images); err != nil {
			return nativeDocumentRecords{}, err
		}
	}
	return session.finishLoaded(ctx)
}

func (journal nativeJournalSnapshots) completeDocuments(ctx context.Context, indexes journaldocument.SnapshotIndexes, inputs []journaldocument.StateGroupInput, result nativeDocumentRecords, codecs aggregate.CodecCatalog) (nativeDocumentRecords, error) {
	groups, err := journaldocument.ReconstructStateGroups(ctx, journal.owner, journal.before, journal.after, inputs)
	if err != nil {
		return nativeDocumentRecords{}, err
	}
	if err := journal.validateExpectedBaselines(ctx, indexes.After(), groups, codecs); err != nil {
		return nativeDocumentRecords{}, err
	}
	documentWork := false
	for _, group := range groups {
		comparison := group.Comparison()
		documentWork = documentWork || !comparison.BeforeDocument().Equal(comparison.ExpectedDocument()) || !comparison.BeforeState().SameRootMetadata(comparison.ExpectedState())
	}
	if err := requireNativeDocumentCommitWork(journal.before, journal.after, documentWork); err != nil {
		return nativeDocumentRecords{}, err
	}
	result.groups = groups
	return result, nil
}

func (journal nativeJournalSnapshots) validateExpectedBaselines(ctx context.Context, after journaldocument.SnapshotIndex, groups []journaldocument.StateGroup, codecs aggregate.CodecCatalog) error {
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

func (journal nativeJournalSnapshots) documentBindings(ctx context.Context) ([][]ownership.DocumentBindingIntent, error) {
	if ctx == nil {
		return nil, fmt.Errorf("document binding context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	equalState := journal.before.Equal(journal.after)
	groups := make([][]ownership.DocumentBindingIntent, 0, len(journal.record.DocumentGroups))
	seenAliases := make(map[aggregate.DocumentAddress]struct{})
	for _, group := range journal.record.DocumentGroups {
		for _, alias := range group.Aliases {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			address, err := alias.Address.canonical()
			if err != nil {
				return nil, err
			}
			if _, repeated := seenAliases[address]; repeated {
				return nil, fmt.Errorf("document alias belongs to more than one group")
			}
			seenAliases[address] = struct{}{}
		}
		intents, err := group.canonicalBindingIntents(ctx, journal.owner)
		if err != nil {
			return nil, err
		}
		for _, intent := range intents {
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
	beforeByDestination := make(map[string]recoveryDocumentObservation, len(group.Aliases))
	for _, alias := range group.Aliases {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		beforeByDestination[alias.Address.Destination] = alias.ObservedBefore
	}
	intents := make([]ownership.DocumentBindingIntent, 0, len(group.BindingIntents))
	for _, record := range group.BindingIntents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		intent, err := record.canonical()
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
		if binding, present := intent.Before().Get(); present {
			if !before.matchesRetainedExact(binding.PathAuthority()) {
				return nil, fmt.Errorf("document binding baseline differs from its captured before namespace")
			}
		}
		if value, known := intent.KnownAfter(); known {
			if binding, present := value.Get(); present {
				exact := binding.PathAuthority()
				_, historical := intent.Before().Get()
				if historical || group.CapturedTarget.Kind == "exact" {
					if !group.CapturedTarget.matchesRetainedExact(exact) {
						return nil, fmt.Errorf("document binding after differs from the captured target")
					}
				} else {
					provisional, err := group.CapturedTarget.provisional()
					if err != nil || provisional.AdmitsExact(exact) != nil {
						return nil, fmt.Errorf("promoted document binding is outside the captured namespace")
					}
				}
			}
		} else {
			pending, _ := intent.ProvisionalAfter()
			captured, err := group.CapturedTarget.provisional()
			if err != nil || !pending.Equal(captured) {
				return nil, fmt.Errorf("provisional document binding differs from the captured target")
			}
		}
		intents = append(intents, intent)
	}
	return intents, nil
}

func (journal nativeJournalSnapshots) documentClaims(ctx context.Context, indexes journaldocument.SnapshotIndexes, record recoveryDocumentGroup, aliases []aggregate.DocumentAddress) (ownershipmutation.DocumentClaimTransition, []journaldocument.ProvisionalAcquire, error) {
	before, err := indexes.Before().Footprints(ctx, aliases)
	if err != nil {
		return ownershipmutation.DocumentClaimTransition{}, nil, err
	}
	after, err := indexes.After().Footprints(ctx, aliases)
	if err != nil {
		return ownershipmutation.DocumentClaimTransition{}, nil, err
	}
	if len(before) == 0 && len(after) == 0 {
		if record.ExclusiveClaims.Kind != "absent" {
			return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("document without global membership carries exclusive claims")
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
			return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("document claims do not cover full global Snapshot membership")
		}
		return claims, nil, nil
	case "provisional":
		provisional, err := record.CapturedTarget.provisional()
		if err != nil || len(before) != 0 {
			return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("provisional document claims require a newly acquired namespace")
		}
		acquires, err := record.ExclusiveClaims.canonicalProvisional(aliases, journal.owner, journal.record.OperationID, provisional)
		if err != nil {
			return ownershipmutation.DocumentClaimTransition{}, nil, err
		}
		footprints := make(map[output.Footprint]struct{}, len(acquires))
		for _, acquire := range acquires {
			footprints[acquire.Intent().Footprint()] = struct{}{}
		}
		if len(footprints) != len(after) {
			return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("provisional claims omit global Snapshot membership")
		}
		for footprint := range after {
			if _, found := footprints[footprint]; !found {
				return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("provisional claim footprint differs from global Snapshot membership")
			}
		}
		return ownershipmutation.DocumentClaimTransition{}, acquires, nil
	default:
		return ownershipmutation.DocumentClaimTransition{}, nil, fmt.Errorf("global document membership requires exact or provisional claims")
	}
}
