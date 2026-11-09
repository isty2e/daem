package publication

import (
	"context"
	"fmt"
	"slices"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/topology"
)

type selectedDocumentCaptureInput struct {
	journalobservation.CaptureInput
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

func (index documentCaptureSubjects) selectedSubjects(ctx context.Context, edits []compose.ProjectionEdit, aliases []aggregate.DocumentAddress) ([]topology.SubjectID, error) {
	addresses, err := journaldocument.AliasSet(ctx, aliases)
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return subjects, nil
}

func prepareSelectedDocumentCandidate(ctx context.Context, namespace journalnamespace.DocumentNamespace, input selectedDocumentCaptureInput, subjects documentCaptureSubjects) (nativeDocumentCaptureCandidate, error) {
	if err := input.CaptureInput.Validate(ctx); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	refs, err := subjects.selectedSubjects(ctx, input.edits, namespace.Addresses())
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	draft, manifest, err := nativeDocumentGroupMetadata(ctx, namespace, input.CaptureInput)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	var orderAlias *aggregate.DocumentAddress
	if input.order != nil && input.order.Changed() {
		var facts journalrecord.PiQualification
		draft, facts, err = draft.WithPiOrder(*input.order)
		if err != nil {
			return nativeDocumentCaptureCandidate{}, err
		}
		alias := facts.Alias()
		orderAlias = &alias
		if err := journalqualification.QualifyPiContext(ctx, facts, manifest, namespace, input.Budget()); err != nil {
			return nativeDocumentCaptureCandidate{}, err
		}
	}

	original, mode, _, err := namespace.ReadReferent(ctx, min(input.Limits().MaximumBytes, recovery.MaximumRecoveryBackupFileBytes), input.Budget())
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	expected, participants, err := journalrecord.PrepareNative16SelectedImage(ctx, original, input.Limits(), input.edits, input.order)
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
	candidate, err := journalrecord.ReconstructNative16Selected(ctx, journaldocument.ComparisonInput{Before: original, Expected: expected, BeforeMode: mode, ExpectedMode: expectedMode, Limits: input.Limits()}, participants, input.order)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	comparison := candidate.Comparison()
	record, err := draft.WithComparison(comparison, input.GroupIndex(), refs)
	if err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}

	relations := make([]aggregate.DocumentAddress, 0, len(input.edits)+1)
	for _, edit := range input.edits {
		for _, contract := range edit.Participant().Selection().Contracts() {
			relations = append(relations, contract.Address().Document())
		}
	}
	if orderAlias != nil {
		relations = append(relations, *orderAlias)
	}
	if err := ctx.Err(); err != nil {
		return nativeDocumentCaptureCandidate{}, err
	}
	return nativeDocumentCaptureCandidate{record: record, namespace: namespace, relations: relations, budget: input.Budget()}, nil
}
