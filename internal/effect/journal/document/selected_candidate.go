package document

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

type SelectedPreparationInput struct {
	Before       aggregate.Document
	BeforeMode   *recovery.PermissionMode
	ExpectedMode *recovery.PermissionMode
	Limits       configdocument.Limits
}

// SelectedCandidate owns a composed image and its comparison,
// not membership, namespace, artifact-custody or operation-phase authority.
type SelectedCandidate struct {
	comparison Comparison
}

func (candidate SelectedCandidate) Document() aggregate.Document {
	return candidate.comparison.expected
}

func (candidate SelectedCandidate) Comparison() Comparison {
	return candidate.comparison
}

// PrepareSelected combines semantic plans with validated positional edits.
// Protocol normalizers must verify the completed image before publication.
func PrepareSelected(ctx context.Context, input SelectedPreparationInput, edits []compose.ProjectionEdit, sequences []configdocument.JSONArrayEdit) (SelectedCandidate, error) {
	expected, participants, err := PrepareSelectedImage(ctx, input.Before, input.Limits, edits, sequences)
	if err != nil {
		return SelectedCandidate{}, err
	}
	return ReconstructSelected(ctx, ComparisonInput{
		Before: input.Before, Expected: expected,
		BeforeMode: input.BeforeMode, ExpectedMode: input.ExpectedMode, Limits: input.Limits,
	}, participants, sequences)
}

func PrepareSelectedImage(ctx context.Context, before aggregate.Document, limits configdocument.Limits, edits []compose.ProjectionEdit, sequences []configdocument.JSONArrayEdit) (aggregate.Document, []compose.Participant, error) {
	if err := ValidateImage(ctx, before, limits); err != nil {
		return aggregate.Document{}, nil, err
	}
	participants := make([]compose.Participant, len(edits))
	for index, edit := range edits {
		participants[index] = edit.Participant()
	}
	retained, err := disjointSelectedSequences(participants, sequences)
	if err != nil {
		return aggregate.Document{}, nil, err
	}
	expected := before
	if len(edits) != 0 {
		candidate, err := compose.Render(ctx, before, limits, edits)
		if err != nil {
			return aggregate.Document{}, nil, err
		}
		expected = candidate.Document()
	}
	for _, sequence := range retained {
		current, err := readArrayComparisonDocument(ctx, expected, limits, sequence.Syntax())
		if err != nil {
			return aggregate.Document{}, nil, err
		}
		applied, _, err := sequence.Apply(ctx, current)
		if err != nil {
			return aggregate.Document{}, nil, err
		}
		expected = aggregate.ExistingDocument(applied.Bytes())
	}
	return expected, participants, nil
}

// ReconstructSelected checks recorded images, selected footprints and positional
// witnesses without reinterpreting protocol identities or order constraints.
func ReconstructSelected(ctx context.Context, input ComparisonInput, participants []compose.Participant, sequences []configdocument.JSONArrayEdit) (SelectedCandidate, error) {
	comparison, err := NewSelectedComparison(ctx, input, participants, sequences)
	if err != nil {
		return SelectedCandidate{}, err
	}
	nativeExpected := input.Expected
	for _, sequence := range comparison.sequences {
		expected, err := readArrayComparisonDocument(ctx, nativeExpected, input.Limits, sequence.Syntax())
		if err != nil {
			return SelectedCandidate{}, err
		}
		restored, _, err := sequence.Restore(ctx, expected)
		if err != nil {
			return SelectedCandidate{}, err
		}
		nativeExpected = aggregate.ExistingDocument(restored.Bytes())
	}
	if len(participants) == 0 {
		if !input.Before.Equal(nativeExpected) {
			return SelectedCandidate{}, fmt.Errorf("unselected document content changed")
		}
	} else if err := compose.VerifyCandidate(ctx, input.Before, nativeExpected, input.Limits, participants); err != nil {
		return SelectedCandidate{}, err
	}
	return SelectedCandidate{comparison: comparison}, nil
}
