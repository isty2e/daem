package record

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type documentGroupCaptureFacts struct {
	owner       stateauthority.Authority
	operationID string
	bindings    []ownership.DocumentBindingIntent
	claims      *ownershipmutation.DocumentClaimTransition
	provisional []journaldocument.ProvisionalAcquire
}

type DocumentGroupAlias struct {
	address     aggregate.DocumentAddress
	root        rootedpath.AuthorityProvenance
	observation mutation.DirectoryEntryAuthorityObservation
	binding     configuration.AliasBinding
}

func NewDocumentGroupAlias(address aggregate.DocumentAddress, root rootedpath.AuthorityProvenance, observation mutation.DirectoryEntryAuthorityObservation) DocumentGroupAlias {
	return DocumentGroupAlias{address: address, root: root, observation: observation}
}

type documentGroupDraftPhase uint8

const (
	documentGroupAwaitingNamespace documentGroupDraftPhase = iota + 1
	documentGroupMetadataComplete
	documentGroupOrderRecorded
)

type DocumentGroupDraft struct {
	phase     documentGroupDraftPhase
	facts     documentGroupCaptureFacts
	record    nativeDocumentBody
	legacy    *native16DocumentEnvelope
	current   *native17DocumentEnvelope
	namespace documentNamespaceFacts
	manifest  rootedpath.AuthorityProvenance
}

type DocumentGroupCandidate struct {
	record      nativeDocumentBody
	legacy      *native16DocumentEnvelope
	current     *native17DocumentEnvelope
	namespace   documentNamespaceFacts
	comparison  journaldocument.Comparison
	manifest    rootedpath.AuthorityProvenance
	initialized bool
}

// SealedDocumentGroup holds complete captured group facts, not operation
// admission, a published revision or permission to mutate a host document.
type SealedDocumentGroup struct {
	record      recoveryDocumentGroup
	current     *native17Document
	namespace   documentNamespaceFacts
	initialized bool
}

func (candidate DocumentGroupCandidate) Comparison() journaldocument.Comparison {
	return candidate.comparison
}

// Qualification uses recorded lexical roots. Source-context semantic admission
// remains with evidence reconstruction, after the existing qualification reads.
type PiQualification struct {
	alias             aggregate.DocumentAddress
	commandRoot       rootedpath.AuthorityProvenance
	settingsBaseRoot  string
	relationBaseRoots []string
}

func (facts PiQualification) Alias() aggregate.DocumentAddress { return facts.alias }

func (facts PiQualification) CommandRoot() rootedpath.AuthorityProvenance { return facts.commandRoot }

func (facts PiQualification) SettingsBaseRoot() string { return facts.settingsBaseRoot }

func (facts PiQualification) RelationBaseRoots() []string {
	return slices.Clone(facts.relationBaseRoots)
}

func BeginDocumentGroup(owner stateauthority.Authority, operationID string, bindings []ownership.DocumentBindingIntent, claims *ownershipmutation.DocumentClaimTransition, provisional []journaldocument.ProvisionalAcquire) (DocumentGroupDraft, error) {
	facts := documentGroupCaptureFacts{owner: owner, operationID: operationID, bindings: bindings, claims: claims, provisional: provisional}
	if err := validateRecoveryOwnershipWorkBudget(len(facts.bindings), 0, 0); err != nil {
		return DocumentGroupDraft{}, err
	}

	facts.bindings = slices.Clone(facts.bindings)
	facts.provisional = slices.Clone(facts.provisional)
	if facts.claims != nil {
		claims := *facts.claims
		facts.claims = &claims
	}
	return DocumentGroupDraft{phase: documentGroupAwaitingNamespace, facts: facts}, nil
}

func (draft DocumentGroupDraft) WithNamespace(ctx context.Context, observed mutation.DirectoryEntryAuthorityObservation, writer rootedpath.AuthorityProvenance, coordinate configuration.RootCoordinate, aliases []DocumentGroupAlias, manifest rootedpath.AuthorityProvenance) (DocumentGroupDraft, error) {
	if draft.phase != documentGroupAwaitingNamespace {
		return DocumentGroupDraft{}, fmt.Errorf("document group draft is not awaiting namespace evidence")
	}
	child, directory := coordinate.DirectoryChild()
	if !directory {
		return DocumentGroupDraft{}, fmt.Errorf("native16 writer requires a directory-child coordinate")
	}
	relative, err := rootedpath.NewRelativeDestination(child)
	if err != nil {
		return DocumentGroupDraft{}, err
	}

	captured, err := persistedNativeObservation(observed)
	if err != nil {
		return DocumentGroupDraft{}, err
	}
	group := recoveryDocumentGroup{
		CapturedTarget: captured, Writer: persistedNativeWriter(writer, relative),
		SelectedSubjects: []persistedSubjectRef{}, Aliases: make([]recoveryDocumentAlias, 0, len(aliases)),
		BindingIntents: make([]recoveryDocumentBindingIntent, 0, len(draft.facts.bindings)),
	}

	addresses := make([]aggregate.DocumentAddress, 0, len(aliases))
	for _, alias := range aliases {
		if err := ctx.Err(); err != nil {
			return DocumentGroupDraft{}, err
		}
		role := "global-root"
		if alias.address.Scope() == target.ScopeProject {
			role = "manifest-root"
		}
		selected, err := persistedNativeObservation(alias.observation)
		if err != nil {
			return DocumentGroupDraft{}, err
		}
		group.Aliases = append(group.Aliases, recoveryDocumentAlias{
			Address: persistedDocumentAlias(alias.address), LogicalRoot: recoveryDocumentLogicalRoot{Role: role, Provenance: persistedNativeRoot(alias.root)},
			SelectedObservation: selected, ObservedBefore: captured,
		})
		addresses = append(addresses, alias.address)
	}

	group.ExclusiveClaims, err = draft.facts.documentClaims(observed, addresses, journaldocument.LegacyGlobalClaims)
	if err != nil {
		return DocumentGroupDraft{}, err
	}

	for _, intent := range draft.facts.bindings {
		if err := ctx.Err(); err != nil {
			return DocumentGroupDraft{}, err
		}
		record, err := persistedDocumentBindingIntent(intent)
		if err != nil {
			return DocumentGroupDraft{}, err
		}
		group.BindingIntents = append(group.BindingIntents, record)
	}
	if _, err := group.canonicalBindingIntents(ctx, draft.facts.owner); err != nil {
		return DocumentGroupDraft{}, err
	}

	namespace, err := lowerNative16DocumentNamespace(group, persistedNativeRoot(manifest))
	if err != nil {
		return DocumentGroupDraft{}, err
	}
	draft.record, draft.manifest, draft.phase = native16GroupBody(group), manifest, documentGroupMetadataComplete
	draft.namespace = namespace
	draft.legacy = &native16DocumentEnvelope{writer: group.Writer, aliases: group.Aliases}
	return draft, nil
}

func NewBoundDocumentGroupAlias(address aggregate.DocumentAddress, binding configuration.AliasBinding, root rootedpath.AuthorityProvenance, observation mutation.DirectoryEntryAuthorityObservation) DocumentGroupAlias {
	return DocumentGroupAlias{address: address, binding: binding, root: root, observation: observation}
}

func (draft DocumentGroupDraft) WithBoundNamespace(ctx context.Context, observed mutation.DirectoryEntryAuthorityObservation, writer journalnamespace.WriterPin, spelling string, aliases []DocumentGroupAlias, manifest rootedpath.AuthorityProvenance) (DocumentGroupDraft, error) {
	if ctx == nil || draft.phase != documentGroupAwaitingNamespace {
		return DocumentGroupDraft{}, fmt.Errorf("bound document namespace requires its awaiting capture draft and context")
	}
	if err := writer.Validate(); err != nil {
		return DocumentGroupDraft{}, err
	}
	writerReference, bound := writer.RootReference()
	if !bound {
		return DocumentGroupDraft{}, fmt.Errorf("native17 capture requires a bound writer")
	}
	if err := manifest.Validate(); err != nil {
		return DocumentGroupDraft{}, err
	}
	captured, err := persistedNativeObservation(observed)
	if err != nil {
		return DocumentGroupDraft{}, err
	}
	body := nativeDocumentBody{CapturedTarget: captured, SelectedSubjects: []persistedSubjectRef{}, BindingIntents: make([]recoveryDocumentBindingIntent, 0, len(draft.facts.bindings))}
	envelope := &native17DocumentEnvelope{Writer: native17Writer{Root: persistedRootReference(writerReference), Coordinate: persistedCoordinate(writer.Coordinate())}, Aliases: make([]native17Alias, 0, len(aliases)), Sequences: []nativeSequenceWitness{}}
	if writer.Coordinate().Kind() == configuration.CoordinatePointSelf {
		if err := validateWriterSpelling(spelling, persistedNativeRoot(writer.Provenance())); err != nil {
			return DocumentGroupDraft{}, err
		}
		envelope.Writer.Spelling = spelling
	}
	namespace := documentNamespaceFacts{writer: writer, writerSpelling: envelope.Writer.Spelling, aliases: make([]documentAliasFacts, 0, len(aliases))}
	addresses := make([]aggregate.DocumentAddress, 0, len(aliases))
	for _, alias := range aliases {
		if err := ctx.Err(); err != nil {
			return DocumentGroupDraft{}, err
		}
		binding, err := configuration.AdmitRecordedAliasBinding(configuration.AliasInput{Target: alias.address.Target(), Scope: alias.address.Scope(), Destination: alias.address.AggregateRoot()}, alias.binding.RootReference(), alias.binding.Coordinate(), alias.binding.PolicyID())
		if err != nil {
			return DocumentGroupDraft{}, err
		}
		if err := alias.root.Validate(); err != nil {
			return DocumentGroupDraft{}, err
		}
		selected, err := persistedNativeObservation(alias.observation)
		if err != nil {
			return DocumentGroupDraft{}, err
		}
		envelope.Aliases = append(envelope.Aliases, native17Alias{Address: persistedDocumentAlias(alias.address), Root: persistedRootReference(binding.RootReference()), Coordinate: persistedCoordinate(binding.Coordinate()), Policy: binding.PolicyID(), SelectedObservation: selected, ObservedBefore: captured})
		namespace.aliases = append(namespace.aliases, documentAliasFacts{address: alias.address, location: binding.Location(), binding: binding, root: alias.root, selected: selected, before: captured})
		addresses = append(addresses, alias.address)
	}
	body.ExclusiveClaims, err = draft.facts.documentClaims(observed, addresses, journaldocument.NativeClaims)
	if err != nil {
		return DocumentGroupDraft{}, err
	}
	for _, intent := range draft.facts.bindings {
		if err := ctx.Err(); err != nil {
			return DocumentGroupDraft{}, err
		}
		record, err := persistedDocumentBindingIntent(intent)
		if err != nil {
			return DocumentGroupDraft{}, err
		}
		body.BindingIntents = append(body.BindingIntents, record)
	}
	if _, err := canonicalDocumentBindingIntents(ctx, body, namespace.aliases, draft.facts.owner); err != nil {
		return DocumentGroupDraft{}, err
	}
	draft.record, draft.namespace, draft.current, draft.manifest, draft.phase = body, namespace, envelope, manifest, documentGroupMetadataComplete
	return draft, nil
}

func (facts documentGroupCaptureFacts) documentClaims(observation mutation.DirectoryEntryAuthorityObservation, aliases []aggregate.DocumentAddress, selection journaldocument.ClaimSelection) (recoveryDocumentClaimSet, error) {
	if facts.claims != nil {
		if len(facts.provisional) != 0 {
			return recoveryDocumentClaimSet{}, fmt.Errorf("native document capture cannot mix exact and provisional claims")
		}
		record, err := persistedDocumentClaimTransition(*facts.claims, facts.operationID)
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		observed, err := persistedNativeObservation(observation)
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		if _, err := record.canonicalObservedExact(facts.owner, facts.operationID, observed); err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		return record, nil
	}
	if len(facts.provisional) != 0 {
		provisional, pending := observation.Provisional()
		if !pending {
			return recoveryDocumentClaimSet{}, fmt.Errorf("provisional document claims require a provisional captured namespace")
		}
		record, err := persistedDocumentProvisionalClaims(facts.provisional, selection)
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		if _, err := record.canonicalProvisional(aliases, facts.owner, facts.operationID, provisional, selection); err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		return record, nil
	}
	return recoveryDocumentClaimSet{Kind: "absent"}, nil
}

func (draft DocumentGroupDraft) WithPiOrder(order pipackage.OrderObservation) (DocumentGroupDraft, PiQualification, error) {
	if draft.phase != documentGroupMetadataComplete || draft.legacy == nil {
		return DocumentGroupDraft{}, PiQualification{}, fmt.Errorf("document group draft cannot record Pi order in its current phase")
	}

	evidence, err := persistedPiOrderEvidence(order)
	if err != nil {
		return DocumentGroupDraft{}, PiQualification{}, err
	}
	captured, err := order.CaptureInput()
	if err != nil {
		return DocumentGroupDraft{}, PiQualification{}, err
	}
	alias, err := PiOrderDocumentAlias(captured.Scope)
	if err != nil {
		return DocumentGroupDraft{}, PiQualification{}, fmt.Errorf("selected Pi order has no captured settings alias")
	}
	aliasRecord := persistedDocumentAlias(alias)
	present := false
	for _, entry := range draft.legacy.aliases {
		if entry.Address == aliasRecord {
			present = true
			break
		}
	}
	if !present {
		return DocumentGroupDraft{}, PiQualification{}, fmt.Errorf("selected Pi order has no captured settings alias")
	}
	orderRecord := recoveryDocumentPiOrder{Alias: aliasRecord, CommandRoot: recoveryDocumentLogicalRoot{Role: "manifest-root", Provenance: persistedNativeRoot(draft.manifest)}, Evidence: evidence}
	qualification, err := orderRecord.qualificationContext()
	if err != nil {
		return DocumentGroupDraft{}, PiQualification{}, err
	}
	envelope := *draft.legacy
	envelope.order, draft.phase = &orderRecord, documentGroupOrderRecorded
	draft.legacy = &envelope
	return draft, qualification, nil
}

func (draft DocumentGroupDraft) WithComparison(comparison journaldocument.Comparison, index int, subjects []topology.SubjectID) (DocumentGroupCandidate, error) {
	if draft.phase != documentGroupMetadataComplete && draft.phase != documentGroupOrderRecorded {
		return DocumentGroupCandidate{}, fmt.Errorf("document group metadata is incomplete")
	}

	_, original, expected, err := documentImageReferences(comparison, index)
	if err != nil {
		return DocumentGroupCandidate{}, err
	}
	record := draft.record
	record.Original, record.Expected = original, expected
	record.Extent = "whole"
	if !comparison.WholeDocument() {
		record.Extent = "selected"
		record.SelectedSubjects = make([]persistedSubjectRef, 0, len(subjects))
		for _, subject := range subjects {
			record.SelectedSubjects = append(record.SelectedSubjects, persistedSubjectRef{Kind: string(subject.Kind()), Namespace: subject.Namespace(), Name: subject.Key()})
		}
	}
	return DocumentGroupCandidate{record: record, namespace: draft.namespace, legacy: draft.legacy, current: draft.current, comparison: comparison, manifest: draft.manifest, initialized: true}, nil
}

func (candidate DocumentGroupCandidate) Seal(ctx context.Context, removal *recovery.DocumentRemoval) (SealedDocumentGroup, journaldocument.Comparison, error) {
	if !candidate.initialized {
		return SealedDocumentGroup{}, journaldocument.Comparison{}, fmt.Errorf("document group capture candidate is required")
	}

	record := candidate.record
	if removal != nil {
		evidence, err := persistedDocumentRemoval(*removal)
		if err != nil {
			return SealedDocumentGroup{}, journaldocument.Comparison{}, err
		}
		record.Removal = &evidence
	}
	var sealed SealedDocumentGroup
	if candidate.legacy != nil {
		legacy := assembleNative16Document(record, *candidate.legacy)
		if err := legacy.validate(persistedNativeRoot(candidate.manifest)); err != nil {
			return SealedDocumentGroup{}, journaldocument.Comparison{}, err
		}
		sealed = SealedDocumentGroup{record: legacy, initialized: true}
	} else if candidate.current != nil {
		current := native17Document{nativeDocumentBody: record, native17DocumentEnvelope: *candidate.current}
		runtime := nativeDocumentRecord{nativeDocumentBody: record, namespace: candidate.namespace, replay: selectedSequenceReplay{arrays: current.Sequences}}
		if err := runtime.validate(); err != nil {
			return SealedDocumentGroup{}, journaldocument.Comparison{}, err
		}
		sealed = SealedDocumentGroup{current: &current, namespace: candidate.namespace, initialized: true}
	} else {
		return SealedDocumentGroup{}, journaldocument.Comparison{}, fmt.Errorf("document has no admitted capture envelope")
	}
	if err := ctx.Err(); err != nil {
		return SealedDocumentGroup{}, journaldocument.Comparison{}, err
	}
	return sealed, candidate.comparison, nil
}

func (record recoveryDocumentPiOrder) qualificationContext() (PiQualification, error) {
	alias, err := record.Alias.canonical()
	if err != nil {
		return PiQualification{}, err
	}
	root, err := record.CommandRoot.Provenance.canonical()
	if err != nil {
		return PiQualification{}, err
	}
	result := PiQualification{alias: alias, commandRoot: root, settingsBaseRoot: record.Evidence.SettingsContext.BaseRoot}
	for _, relation := range record.Evidence.Relations {
		result.relationBaseRoots = append(result.relationBaseRoots, relation.SourceContext.BaseRoot)
	}
	return result, nil
}
