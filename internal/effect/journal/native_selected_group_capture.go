package journal

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/topology"
)

type selectedDocumentCaptureInput struct {
	nativeDocumentCaptureInput
	edits []compose.ProjectionEdit
	order *pipackage.OrderObservation
}

type documentCaptureContractKey struct {
	address aggregate.ProjectionAddress
	codec   aggregate.CodecContractID
}

type documentCaptureSubjects map[documentCaptureContractKey][]durable.ManagedAggregateState

func indexDocumentCaptureSubjects(ctx context.Context, before, after durable.Snapshot) (documentCaptureSubjects, error) {
	if ctx == nil {
		return nil, fmt.Errorf("document capture subject context is required")
	}
	index := make(documentCaptureSubjects)
	for _, snapshot := range []durable.Snapshot{before, after} {
		for _, row := range snapshot.ManagedAggregates() {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			contract := row.Contribution().Contract()
			key := documentCaptureContractKey{address: contract.Address(), codec: contract.CodecContractID()}
			index[key] = append(index[key], row)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return index, nil
}

func (index documentCaptureSubjects) selectedSubjects(ctx context.Context, edits []compose.ProjectionEdit, aliases []aggregate.DocumentAddress) ([]persistedSubjectRef, error) {
	addresses, err := documentAliasSet(ctx, aliases)
	if err != nil {
		return nil, err
	}
	seen := make(map[topology.SubjectID]struct{})
	selectedContracts := make(map[documentCaptureContractKey]aggregate.ProjectionContract)
	for _, edit := range edits {
		contracts := edit.Participant().Selection().Contracts()
		if len(contracts) == 0 {
			return nil, fmt.Errorf("selected capture edit has no actual contracts")
		}
		for _, contract := range contracts {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if _, present := addresses[contract.Address().Document()]; !present {
				return nil, fmt.Errorf("selected capture edit has no namespace alias")
			}
			key := documentCaptureContractKey{address: contract.Address(), codec: contract.CodecContractID()}
			if previous, duplicate := selectedContracts[key]; duplicate {
				if !previous.Equal(contract) {
					return nil, fmt.Errorf("selected capture edits disagree on their contract")
				}
				continue
			}
			selectedContracts[key] = contract
			rows := index[key]
			if len(rows) == 0 {
				return nil, fmt.Errorf("selected capture contract has no before or after subject")
			}
			for _, row := range rows {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if !row.Contribution().Contract().Equal(contract) {
					return nil, fmt.Errorf("selected capture contract differs across its state-backed subjects")
				}
				seen[row.Subject()] = struct{}{}
			}
		}
	}
	subjects := make([]topology.SubjectID, 0, len(seen))
	for subject := range seen {
		subjects = append(subjects, subject)
	}
	slices.SortFunc(subjects, topology.CompareSubjectID)
	refs := make([]persistedSubjectRef, 0, len(subjects))
	for _, subject := range subjects {
		refs = append(refs, persistedSubjectRef{Kind: string(subject.Kind()), Namespace: subject.Namespace(), Name: subject.Key()})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return refs, nil
}

// prepareSelectedDocumentGroup captures edit/order facts. Complete Snapshot and
// operation-authority admission remain the containing caller's responsibility.
func prepareSelectedDocumentGroup(ctx context.Context, namespace NativeDocumentNamespace, input selectedDocumentCaptureInput, subjects documentCaptureSubjects) (recoveryDocumentGroup, DocumentComparison, error) {
	candidate, err := prepareSelectedDocumentCandidate(ctx, namespace, input, subjects)
	if err != nil {
		return recoveryDocumentGroup{}, DocumentComparison{}, err
	}
	return candidate.captureRemoval(ctx, input.budget)
}

func prepareSelectedDocumentCandidate(ctx context.Context, namespace NativeDocumentNamespace, input selectedDocumentCaptureInput, subjects documentCaptureSubjects) (nativeDocumentCaptureCandidate, error) {
	if err := validateNativeDocumentCaptureInput(ctx, input.nativeDocumentCaptureInput); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	refs, err := subjects.selectedSubjects(ctx, input.edits, namespace.Addresses())
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	group, manifest, err := nativeDocumentGroupMetadata(ctx, namespace, input.nativeDocumentCaptureInput)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	if input.order != nil && input.order.Changed() {
		evidence, err := persistedPiOrderEvidence(*input.order)
		if err != nil {
			return nativeDocumentCaptureCandidate{}, err
		}
		captured, err := input.order.CaptureInput()
		if err != nil {
			return nativeDocumentCaptureCandidate{}, err
		}
		alias, err := piOrderDocumentAlias(captured.Scope)
		if err != nil || !slices.Contains(namespace.Addresses(), alias) {
			return nativeDocumentCaptureCandidate{}, fmt.Errorf("selected Pi order has no captured settings alias")
		}
		group.PiOrder = &recoveryDocumentPiOrder{Alias: persistedDocumentAlias(alias), CommandRoot: recoveryDocumentLogicalRoot{Role: "manifest-root", Provenance: persistedNativeRoot(manifest.provenance)}, Evidence: evidence}
		if err := qualifyNativePiContext(ctx, *group.PiOrder, manifest, namespace, input.budget); err != nil {
			return nativeDocumentCaptureCandidate{}, err
		}
	}
	original, mode, _, err := readNativeRecoveryDocument(ctx, namespace, min(input.limits.MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), input.budget)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	expected, participants, err := prepareSelectedDocumentImage(ctx, original, input.limits, input.edits, input.order)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	expectedMode := mode
	if !expected.Exists() {
		expectedMode = nil
	} else {
		if expectedMode == nil {
			expectedMode = recovery.NewPermissionMode(aggregate.DocumentFileMode)
		}
		for _, participant := range participants {
			if _, native := aggregate.HostConfigSyntaxForCodec(participant.Selection().CodecContractID()); !native {
				expectedMode = recovery.NewPermissionMode(aggregate.DocumentFileMode)
			}
		}
	}
	candidate, err := ReconstructSelectedDocument(ctx, DocumentComparisonInput{Before: original, Expected: expected, BeforeMode: mode, ExpectedMode: expectedMode, Limits: input.limits}, participants, input.order)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	group.Extent, group.SelectedSubjects = "selected", refs
	comparison := candidate.Comparison()
	_, group.Original, group.Expected, err = documentImageReferences(comparison, input.groupIndex)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	relations := make([]aggregate.DocumentAddress, 0, len(input.edits)+1)
	for _, edit := range input.edits {
		for _, contract := range edit.Participant().Selection().Contracts() {
			relations = append(relations, contract.Address().Document())
		}
	}
	if group.PiOrder != nil {
		alias, err := group.PiOrder.Alias.canonical()
		if err != nil {
			return nativeDocumentCaptureCandidate{}, err
		}
		relations = append(relations, alias)
	}
	if err := ctx.Err(); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	return nativeDocumentCaptureCandidate{group: group, comparison: comparison, namespace: namespace, manifest: manifest, relations: relations, budget: input.budget}, nil
}
