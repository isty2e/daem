package journal

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
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
	"github.com/isty2e/daem/internal/topology"
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
	var inputs []documentStateGroupInput
	result := nativeDocumentRecords{}
	for _, record := range journal.record.DocumentGroups {
		if err := ctx.Err(); err != nil {
			return nativeDocumentRecords{}, err
		}
		aliases := make([]aggregate.DocumentAddress, 0, len(record.Aliases))
		for _, alias := range record.Aliases {
			address, err := alias.Address.canonical()
			if err != nil {
				return nativeDocumentRecords{}, err
			}
			aliases = append(aliases, address)
		}
		subjects := make([]topology.SubjectID, 0, len(record.SelectedSubjects))
		for _, ref := range record.SelectedSubjects {
			subject, err := ref.canonical()
			if err != nil {
				return nativeDocumentRecords{}, err
			}
			subjects = append(subjects, subject)
		}
		selections, err := indexes.selectedContracts(ctx, subjects, aliases)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		bindings, err := journal.documentBindings(record)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		claims, provisional, err := journal.documentClaims(ctx, indexes, record, aliases)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		images, err := loadDocumentImages(ctx, record.Original, record.Expected, options.artifacts, options.filesystem, options.budget, recovery.MaximumRecoveryBackupFileBytes)
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		input := DocumentComparisonInput{
			Before: images.original.document, Expected: images.expected.document,
			BeforeMode: images.original.mode, ExpectedMode: images.expected.mode, Limits: options.limits,
		}
		var comparison DocumentComparison
		if record.Extent == "whole" {
			comparison, err = NewWholeDocumentComparison(ctx, input)
		} else {
			participants := make([]compose.Participant, 0, len(selections))
			for _, selection := range selections {
				participant, err := nativeJournalParticipant(selection, options.codecs)
				if err != nil {
					return nativeDocumentRecords{}, err
				}
				participants = append(participants, participant)
			}
			var order *pipackage.OrderObservation
			if record.PiOrder != nil {
				observed, err := record.PiOrder.Evidence.canonical(ctx, images.original.document)
				if err != nil {
					return nativeDocumentRecords{}, err
				}
				order = &observed
			}
			candidate, candidateErr := ReconstructSelectedDocument(ctx, input, participants, order)
			err = candidateErr
			comparison = candidate.Comparison()
		}
		if err != nil {
			return nativeDocumentRecords{}, err
		}
		inputs = append(inputs, documentStateGroupInput{aliases: aliases, selectedSubjects: subjects, comparison: comparison, bindingIntents: bindings})
		result.images = append(result.images, images)
		result.claims = append(result.claims, claims)
		result.provisional = append(result.provisional, provisional)
	}
	groups, err := reconstructDocumentStateGroups(ctx, journal.owner, journal.before, journal.after, inputs)
	if err != nil {
		return nativeDocumentRecords{}, err
	}
	if err := journal.validateExpectedBaselines(ctx, indexes.after, groups, options.codecs); err != nil {
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

func (journal nativeJournalSnapshots) documentBindings(group recoveryDocumentGroup) ([]ownership.DocumentBindingIntent, error) {
	intents := make([]ownership.DocumentBindingIntent, 0, len(group.BindingIntents))
	for _, record := range group.BindingIntents {
		intent, err := record.canonical()
		if err != nil {
			return nil, err
		}
		if !intent.Owner().Equal(journal.owner) {
			return nil, fmt.Errorf("document binding has another operation owner")
		}
		aliasIndex := slices.IndexFunc(group.Aliases, func(alias recoveryDocumentAlias) bool {
			return alias.Address.Destination == intent.Destination().String()
		})
		if aliasIndex < 0 {
			return nil, fmt.Errorf("document binding has no group alias")
		}
		before := group.Aliases[aliasIndex].ObservedBefore
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
		changes, err := intent.ChangesBinding()
		if err != nil {
			return nil, err
		}
		if changes && journal.before.Equal(journal.after) {
			return nil, fmt.Errorf("equal Snapshot operation cannot carry nontrivial document binding debt")
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
	for _, row := range index.aggregates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		contract := row.Contribution().Contract()
		if _, found := addresses[contract.Address().Document()]; found && contract.Address().Document().Scope() == target.ScopeGlobal {
			result[contract.Address().Footprint()] = struct{}{}
		}
	}
	for _, row := range index.paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id, native := topologyprojection.EntityID(row.Subject())
		if !native || id.Kind() != entity.KindHostConfig || row.Scope() != target.ScopeGlobal {
			continue
		}
		for address := range addresses {
			if address.Scope() == row.Scope() && address.AggregateRoot() == row.Destination() && slices.Contains(row.ConsumerTargets(), address.Target()) {
				result[output.WholePathFootprint()] = struct{}{}
			}
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
