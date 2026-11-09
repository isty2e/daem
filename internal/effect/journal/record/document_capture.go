package record

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
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
	phase    documentGroupDraftPhase
	facts    documentGroupCaptureFacts
	record   recoveryDocumentGroup
	manifest rootedpath.AuthorityProvenance
}

type DocumentGroupCandidate struct {
	record      recoveryDocumentGroup
	comparison  journaldocument.Comparison
	manifest    rootedpath.AuthorityProvenance
	initialized bool
}

// SealedDocumentGroup holds complete captured group facts, not operation
// admission, a published revision or permission to mutate a host document.
type SealedDocumentGroup struct {
	record      recoveryDocumentGroup
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

	group.ExclusiveClaims, err = draft.facts.documentClaims(observed, addresses)
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

	draft.record, draft.manifest, draft.phase = group, manifest, documentGroupMetadataComplete
	return draft, nil
}

func (facts documentGroupCaptureFacts) documentClaims(observation mutation.DirectoryEntryAuthorityObservation, aliases []aggregate.DocumentAddress) (recoveryDocumentClaimSet, error) {
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
		record, err := persistedDocumentProvisionalClaims(facts.provisional)
		if err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		if _, err := record.canonicalProvisional(aliases, facts.owner, facts.operationID, provisional); err != nil {
			return recoveryDocumentClaimSet{}, err
		}
		return record, nil
	}
	return recoveryDocumentClaimSet{Kind: "absent"}, nil
}

func (draft DocumentGroupDraft) WithPiOrder(order pipackage.OrderObservation) (DocumentGroupDraft, PiQualification, error) {
	if draft.phase != documentGroupMetadataComplete {
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
	for _, entry := range draft.record.Aliases {
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
	draft.record.PiOrder, draft.phase = &orderRecord, documentGroupOrderRecorded
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
	return DocumentGroupCandidate{record: record, comparison: comparison, manifest: draft.manifest, initialized: true}, nil
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
	if err := record.validate(persistedNativeRoot(candidate.manifest)); err != nil {
		return SealedDocumentGroup{}, journaldocument.Comparison{}, err
	}
	if err := ctx.Err(); err != nil {
		return SealedDocumentGroup{}, journaldocument.Comparison{}, err
	}
	return SealedDocumentGroup{record: record, initialized: true}, candidate.comparison, nil
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
