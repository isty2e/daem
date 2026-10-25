package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type nativeDocumentReadOptions struct {
	artifacts  *rootedpath.CapturedRoot
	filesystem mutationfs.RootedReader
	budget     *recovery.PhysicalWorkBudget
	limits     configdocument.Limits
	codecs     aggregate.CodecCatalog
}

// Reconstructed records retain all groups or none. They are not RecoverablePlan:
// active directory, live namespaces, registry phases and effect leases are absent.
type nativeDocumentRecords struct {
	groups      []documentStateGroup
	images      []loadedDocumentImages
	claims      []ownershipmutation.DocumentClaimTransition
	provisional [][]documentProvisionalAcquire
}

func (journal nativeJournalSnapshots) loadDocuments(ctx context.Context, options nativeDocumentReadOptions) (nativeDocumentRecords, error) {
	if ctx == nil || options.artifacts == nil || options.filesystem == nil || options.budget == nil {
		return nativeDocumentRecords{}, fmt.Errorf("native document read context, artifact root, filesystem and shared budget are required")
	}
	if err := ctx.Err(); err != nil {
		return nativeDocumentRecords{}, err
	}
	if err := journal.record.validateRemovalCoverage(); err != nil {
		return nativeDocumentRecords{}, err
	}
	if err := journal.owner.Validate(); err != nil {
		return nativeDocumentRecords{}, err
	}
	if err := options.limits.Validate(); err != nil {
		return nativeDocumentRecords{}, err
	}
	indexes, err := indexDocumentSnapshots(ctx, journal.before, journal.after)
	if err != nil {
		return nativeDocumentRecords{}, err
	}
	bindingGroups, err := journal.documentBindings(ctx)
	if err != nil {
		return nativeDocumentRecords{}, err
	}
	var inputs []documentStateGroupInput
	result := nativeDocumentRecords{}
	for groupIndex, record := range journal.record.DocumentGroups {
		if err := ctx.Err(); err != nil {
			return nativeDocumentRecords{}, err
		}
		aliases, subjects, err := record.canonicalReferences(ctx)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		selections, err := indexes.selectedContracts(ctx, subjects, aliases)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		bindings := bindingGroups[groupIndex]
		claims, provisional, err := journal.documentClaims(ctx, indexes, record, aliases)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		images, err := loadDocumentImages(ctx, record.Original, record.Expected, options.artifacts, options.filesystem, options.budget, recovery.MaximumRecoveryBackupFileBytes)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		if err := record.validateRemovalImages(images); err != nil {
			return nativeDocumentRecords{}, err
		}
		comparison, err := reconstructNativeDocumentComparison(ctx, record, images, selections, options.limits, options.codecs)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		inputs = append(inputs, documentStateGroupInput{aliases: aliases, selectedSubjects: subjects, comparison: comparison, bindingIntents: bindings})
		result.images = append(result.images, images)
		result.claims = append(result.claims, claims)
		result.provisional = append(result.provisional, provisional)
	}
	return journal.completeDocuments(ctx, indexes, inputs, result, options.codecs)
}

func (journal nativeJournalSnapshots) completeDocuments(ctx context.Context, indexes documentSnapshotIndexes, inputs []documentStateGroupInput, result nativeDocumentRecords, codecs aggregate.CodecCatalog) (nativeDocumentRecords, error) {
	groups, err := reconstructDocumentStateGroups(ctx, journal.owner, journal.before, journal.after, inputs)
	if err != nil {
		return nativeDocumentRecords{}, err
	}
	if err := journal.validateExpectedBaselines(ctx, indexes.after, groups, codecs); err != nil {
		return nativeDocumentRecords{}, err
	}
	result.groups = groups
	return result, nil
}

func (journal nativeJournalSnapshots) validateExpectedBaselines(ctx context.Context, after documentSnapshotIndex, groups []documentStateGroup, codecs aggregate.CodecCatalog) error {
	aliases := make(map[aggregate.DocumentAddress]int)
	for index, group := range groups {
		for _, alias := range group.aliases {
			aliases[alias] = index
		}
	}
	for _, row := range after.aggregates {
		if err := ctx.Err(); err != nil {
			return err
		}
		contribution := row.Contribution()
		contract := contribution.Contract()
		index, present := aliases[contract.Address().Document()]
		if !present || !groups[index].covers(contract) {
			continue
		}
		codec, found := codecs.Lookup(contract.CodecContractID())
		if !found {
			return fmt.Errorf("expected baseline codec is not admitted by the operation")
		}
		selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
		if err != nil {
			return err
		}
		snapshot, failure := codec.Read(groups[index].comparison.expected, selection)
		if failure != nil {
			return failure
		}
		state, found := snapshot.State(contract)
		if !found || !state.Present() || state.CanonicalProjection() != contribution.CanonicalContribution() {
			return fmt.Errorf("selected after baseline differs from the actual expected image")
		}
	}
	for _, row := range after.paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		id, native := topologyprojection.EntityID(row.Subject())
		if !native || id.Kind() != entity.KindHostConfig {
			continue
		}
		for _, selected := range row.ConsumerTargets() {
			address, err := aggregate.NewDocumentAddress(selected, row.Scope(), row.Destination())
			if err != nil {
				return err
			}
			if index, present := aliases[address]; present {
				image := journal.record.DocumentGroups[index].Expected
				if image.Kind != "file" || image.Artifact.Digest != row.ContentHash() || image.Mode == nil || uint32(*image.Mode) != uint32(row.FileMode().Perm()) {
					return fmt.Errorf("whole after baseline differs from the actual expected image or mode")
				}
			}
		}
	}
	return ctx.Err()
}

func nativeJournalParticipant(selection aggregate.Selection, codecs aggregate.CodecCatalog) (compose.Participant, error) {
	codec, found := codecs.Lookup(selection.CodecContractID())
	if !found {
		return compose.Participant{}, fmt.Errorf("document selection codec is not admitted by the operation")
	}
	var syntax configdocument.Syntax
	var areas []output.KeyPath
	if grammar, native := aggregate.HostConfigSyntaxForCodec(selection.CodecContractID()); native {
		var err error
		syntax, err = configdocument.ParseSyntax(grammar)
		if err != nil {
			return compose.Participant{}, err
		}
		for _, contract := range selection.Contracts() {
			if _, err := profile.HostConfigPlacementForContract(contract); err != nil {
				return compose.Participant{}, err
			}
			areas = append(areas, contract.Address().KeyPath())
		}
	} else {
		address := selection.DocumentAddress()
		if address.Target() != target.TargetPi && address.Target() != target.TargetCodex {
			return compose.Participant{}, fmt.Errorf("native document composition requires an admitted Pi or Codex participant")
		}
		for _, contract := range selection.Contracts() {
			placement, found := aggregate.MCPPlacementForCodec(aggregate.MCPPlacementID(contract.Address().PlacementID()), contract.CodecContractID())
			if !found || placement.Target() != address.Target() || placement.Scope() != address.Scope() || placement.ConfigPath() != address.AggregateRoot() {
				return compose.Participant{}, fmt.Errorf("document MCP selection differs from its admitted placement")
			}
			footprint, err := output.FootprintFromLegacy(output.ContentPath(placement.ContentPathPrefix()))
			if err != nil {
				return compose.Participant{}, err
			}
			area, key := footprint.Key()
			if !key {
				return compose.Participant{}, fmt.Errorf("MCP rewrite area must be a selected key")
			}
			areas = append(areas, area)
		}
		syntax = configdocument.JSON
		if address.Target() == target.TargetCodex {
			syntax = configdocument.TOML
		}
	}
	return compose.NewParticipant(codec, selection, syntax, output.TopmostKeyPaths(areas))
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
			if before.Kind != "exact" || *persistedPathAuthority(binding.PathAuthority()) != *before.Authority {
				return nil, fmt.Errorf("document binding baseline differs from its captured before namespace")
			}
		}
		if value, known := intent.KnownAfter(); known {
			if binding, present := value.Get(); present {
				exact := binding.PathAuthority()
				if group.CapturedTarget.Kind == "exact" {
					if *persistedPathAuthority(exact) != *group.CapturedTarget.Authority {
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

func (journal nativeJournalSnapshots) documentClaims(ctx context.Context, indexes documentSnapshotIndexes, record recoveryDocumentGroup, aliases []aggregate.DocumentAddress) (ownershipmutation.DocumentClaimTransition, []documentProvisionalAcquire, error) {
	before, err := indexes.before.documentFootprints(ctx, aliases)
	if err != nil {
		return ownershipmutation.DocumentClaimTransition{}, nil, err
	}
	after, err := indexes.after.documentFootprints(ctx, aliases)
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
		var claims ownershipmutation.DocumentClaimTransition
		var err error
		if record.CapturedTarget.Kind == "exact" {
			exact, exactErr := canonicalPathAuthority(*record.CapturedTarget.Authority)
			if exactErr != nil {
				return claims, nil, exactErr
			}
			claims, err = record.ExclusiveClaims.canonicalExact(journal.owner, journal.record.OperationID, exact)
		} else {
			if len(before) != 0 || record.ExclusiveClaims.After == nil || len(*record.ExclusiveClaims.After) == 0 {
				return claims, nil, fmt.Errorf("promoted provisional claims cannot reuse prior global membership")
			}
			value, valueErr := canonicalClaimValue((*record.ExclusiveClaims.After)[0])
			if valueErr != nil {
				return claims, nil, valueErr
			}
			claim, present := value.Get()
			provisional, provisionalErr := record.CapturedTarget.provisional()
			if !present || provisionalErr != nil || provisional.AdmitsExact(claim.Address().PathAuthority()) != nil {
				return claims, nil, fmt.Errorf("promoted document claims are outside the captured namespace")
			}
			claims, err = record.ExclusiveClaims.canonicalExact(journal.owner, journal.record.OperationID, claim.Address().PathAuthority())
		}
		if err != nil {
			return ownershipmutation.DocumentClaimTransition{}, nil, err
		}
		if !sameDocumentClaimFootprints(before, claims.Before()) || !sameDocumentClaimFootprints(after, claims.After()) {
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
			footprints[acquire.intent.Footprint()] = struct{}{}
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

func (index documentSnapshotIndex) documentFootprints(ctx context.Context, aliases []aggregate.DocumentAddress) (map[output.Footprint]struct{}, error) {
	addresses, err := documentAliasSet(ctx, aliases)
	if err != nil {
		return nil, err
	}
	result := make(map[output.Footprint]struct{})
	for address := range addresses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for footprint := range index.globalFootprints[address] {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			result[footprint] = struct{}{}
		}
	}
	return result, nil
}

func sameDocumentClaimFootprints(expected map[output.Footprint]struct{}, claims []ownership.Claim) bool {
	if len(expected) != len(claims) {
		return false
	}
	for _, claim := range claims {
		if _, found := expected[claim.Address().Footprint()]; !found {
			return false
		}
	}
	return true
}
