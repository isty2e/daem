package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
)

type SelectedDocumentPreparationInput struct {
	Before       aggregate.Document
	BeforeMode   *recovery.PermissionMode
	ExpectedMode *recovery.PermissionMode
	Limits       configdocument.Limits
}

// SelectedDocumentCandidate owns a composed semantic image and its comparison,
// not membership, namespace, artifact-custody or operation-phase authority.
type SelectedDocumentCandidate struct {
	comparison DocumentComparison
	order      *pipackage.OrderObservation
}

func (candidate SelectedDocumentCandidate) Document() aggregate.Document {
	return candidate.comparison.expected
}

func (candidate SelectedDocumentCandidate) Comparison() DocumentComparison {
	return candidate.comparison
}

// OrderInput returns replay facts only for a changed, independently selected order.
func (candidate SelectedDocumentCandidate) OrderInput() (pipackage.LoadedOrderInput, bool, error) {
	if err := candidate.comparison.Validate(); err != nil {
		return pipackage.LoadedOrderInput{}, false, err
	}
	if candidate.order == nil {
		return pipackage.LoadedOrderInput{}, false, nil
	}
	input, err := candidate.order.CaptureInput()
	return input, err == nil, err
}

// PrepareSelectedDocument accepts real semantic plans, not claimed expected bytes.
func PrepareSelectedDocument(ctx context.Context, input SelectedDocumentPreparationInput, edits []compose.ProjectionEdit, order *pipackage.OrderObservation) (SelectedDocumentCandidate, error) {
	expected, participants, err := prepareSelectedDocumentImage(ctx, input.Before, input.Limits, edits, order)
	if err != nil {
		return SelectedDocumentCandidate{}, err
	}
	return ReconstructSelectedDocument(ctx, DocumentComparisonInput{
		Before: input.Before, Expected: expected,
		BeforeMode: input.BeforeMode, ExpectedMode: input.ExpectedMode, Limits: input.Limits,
	}, participants, order)
}

func prepareSelectedDocumentImage(ctx context.Context, before aggregate.Document, limits configdocument.Limits, edits []compose.ProjectionEdit, order *pipackage.OrderObservation) (aggregate.Document, []compose.Participant, error) {
	if err := validateComparisonDocument(ctx, before, limits); err != nil {
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

// ReconstructSelectedDocument verifies actual loaded images through the same
// semantic owners as preparation before compiling any recovery comparison.
func ReconstructSelectedDocument(ctx context.Context, input DocumentComparisonInput, participants []compose.Participant, order *pipackage.OrderObservation) (SelectedDocumentCandidate, error) {
	for _, document := range []aggregate.Document{input.Before, input.Expected} {
		if err := validateComparisonDocument(ctx, document, input.Limits); err != nil {
			return SelectedDocumentCandidate{}, err
		}
	}
	nativeExpected := input.Expected
	var permutation *piconfig.PackagePermutation
	if order != nil {
		if input.Expected.ContentSize() > pipackage.MaximumSettingsBytes {
			return SelectedDocumentCandidate{}, fmt.Errorf("Pi expected image exceeds the settings byte limit")
		}
		captured, err := order.CaptureInput()
		if err != nil {
			return SelectedDocumentCandidate{}, err
		}
		if !captured.Document.Equal(input.Before) {
			return SelectedDocumentCandidate{}, fmt.Errorf("Pi order does not describe the original document")
		}
		if err := order.VerifyPackageContent(input.Expected.Content(), input.Expected.Exists()); err != nil {
			return SelectedDocumentCandidate{}, err
		}
		if order.Changed() {
			before, err := readPiComparisonDocument(ctx, input.Before, input.Limits)
			if err != nil {
				return SelectedDocumentCandidate{}, err
			}
			expected, err := readPiComparisonDocument(ctx, input.Expected, input.Limits)
			if err != nil {
				return SelectedDocumentCandidate{}, err
			}
			inverse, err := piconfig.NewPackagePermutation(before, expected)
			if err != nil {
				return SelectedDocumentCandidate{}, err
			}
			content, _, err := inverse.Restore(expected)
			if err != nil {
				return SelectedDocumentCandidate{}, err
			}
			nativeExpected = aggregate.ExistingDocument(content)
			permutation = &inverse
		}
	}
	if len(participants) == 0 {
		if !input.Before.Equal(nativeExpected) {
			return SelectedDocumentCandidate{}, fmt.Errorf("unselected document content changed")
		}
	} else if err := compose.VerifyCandidate(ctx, input.Before, nativeExpected, input.Limits, participants); err != nil {
		return SelectedDocumentCandidate{}, err
	}
	comparison, err := NewSelectedDocumentComparison(ctx, input, participants, permutation)
	if err != nil {
		return SelectedDocumentCandidate{}, err
	}
	candidate := SelectedDocumentCandidate{comparison: comparison}
	if permutation != nil {
		retained := *order
		candidate.order = &retained
	}
	return candidate, nil
}
