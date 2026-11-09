package document

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
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

// SelectedCandidate owns a composed semantic image and its comparison,
// not membership, namespace, artifact-custody or operation-phase authority.
type SelectedCandidate struct {
	comparison Comparison
	order      *pipackage.OrderObservation
}

func (candidate SelectedCandidate) Document() aggregate.Document {
	return candidate.comparison.expected
}

func (candidate SelectedCandidate) Comparison() Comparison {
	return candidate.comparison
}

// OrderInput returns replay facts only for a changed, independently selected order.
func (candidate SelectedCandidate) OrderInput() (pipackage.LoadedOrderInput, bool, error) {
	if err := candidate.comparison.Validate(); err != nil {
		return pipackage.LoadedOrderInput{}, false, err
	}
	if candidate.order == nil {
		return pipackage.LoadedOrderInput{}, false, nil
	}
	input, err := candidate.order.CaptureInput()
	return input, err == nil, err
}

// PrepareSelected accepts real semantic plans, not claimed expected bytes.
func PrepareSelected(ctx context.Context, input SelectedPreparationInput, edits []compose.ProjectionEdit, order *pipackage.OrderObservation) (SelectedCandidate, error) {
	expected, participants, err := PrepareSelectedImage(ctx, input.Before, input.Limits, edits, order)
	if err != nil {
		return SelectedCandidate{}, err
	}
	return ReconstructSelected(ctx, ComparisonInput{
		Before: input.Before, Expected: expected,
		BeforeMode: input.BeforeMode, ExpectedMode: input.ExpectedMode, Limits: input.Limits,
	}, participants, order)
}

func PrepareSelectedImage(ctx context.Context, before aggregate.Document, limits configdocument.Limits, edits []compose.ProjectionEdit, order *pipackage.OrderObservation) (aggregate.Document, []compose.Participant, error) {
	if err := ValidateImage(ctx, before, limits); err != nil {
		return aggregate.Document{}, nil, err
	}
	participants := make([]compose.Participant, len(edits))
	for index, edit := range edits {
		participants[index] = edit.Participant()
	}
	expected := before
	if len(edits) != 0 {
		candidate, err := compose.Render(ctx, before, limits, edits)
		if err != nil {
			return aggregate.Document{}, nil, err
		}
		expected = candidate.Document()
	}
	if order != nil {
		var err error
		expected, err = order.Project(expected)
		if err != nil {
			return aggregate.Document{}, nil, err
		}
	}
	return expected, participants, nil
}

// ReconstructSelected verifies actual loaded images through the same
// semantic owners as preparation before compiling any recovery comparison.
func ReconstructSelected(ctx context.Context, input ComparisonInput, participants []compose.Participant, order *pipackage.OrderObservation) (SelectedCandidate, error) {
	for _, document := range []aggregate.Document{input.Before, input.Expected} {
		if err := ValidateImage(ctx, document, input.Limits); err != nil {
			return SelectedCandidate{}, err
		}
	}
	nativeExpected := input.Expected
	var permutations []configdocument.JSONArrayEdit
	if order != nil {
		if input.Expected.ContentSize() > pipackage.MaximumSettingsBytes {
			return SelectedCandidate{}, fmt.Errorf("Pi expected image exceeds the settings byte limit")
		}
		captured, err := order.CaptureInput()
		if err != nil {
			return SelectedCandidate{}, err
		}
		if !captured.Document.Equal(input.Before) {
			return SelectedCandidate{}, fmt.Errorf("Pi order does not describe the original document")
		}
		if err := order.VerifyPackageContent(input.Expected.Content(), input.Expected.Exists()); err != nil {
			return SelectedCandidate{}, err
		}
		if order.Changed() {
			projection, err := order.NativeProjection()
			if err != nil {
				return SelectedCandidate{}, err
			}
			syntax, err := configdocument.ParseSyntax(string(projection.Grammar()))
			if err != nil {
				return SelectedCandidate{}, err
			}
			before, err := readArrayComparisonDocument(ctx, input.Before, input.Limits, syntax)
			if err != nil {
				return SelectedCandidate{}, err
			}
			expected, err := readArrayComparisonDocument(ctx, input.Expected, input.Limits, syntax)
			if err != nil {
				return SelectedCandidate{}, err
			}
			inverse, err := configdocument.NewJSONArrayPermutation(ctx, before, expected, projection.ArrayPath())
			if err != nil {
				return SelectedCandidate{}, err
			}
			restored, _, err := inverse.Restore(ctx, expected)
			if err != nil {
				return SelectedCandidate{}, err
			}
			nativeExpected = aggregate.ExistingDocument(restored.Bytes())
			permutations = append(permutations, inverse)
		}
	}
	if len(participants) == 0 {
		if !input.Before.Equal(nativeExpected) {
			return SelectedCandidate{}, fmt.Errorf("unselected document content changed")
		}
	} else if err := compose.VerifyCandidate(ctx, input.Before, nativeExpected, input.Limits, participants); err != nil {
		return SelectedCandidate{}, err
	}
	comparison, err := NewSelectedComparison(ctx, input, participants, permutations)
	if err != nil {
		return SelectedCandidate{}, err
	}
	candidate := SelectedCandidate{comparison: comparison}
	if len(permutations) != 0 {
		retained := *order
		candidate.order = &retained
	}
	return candidate, nil
}
