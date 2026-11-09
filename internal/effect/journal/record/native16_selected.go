package record

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

// PrepareNative16SelectedImage retains native16's private Pi checks while lowering
// its order to the shared positional editor. Context qualification stays separate.
func PrepareNative16SelectedImage(ctx context.Context, before aggregate.Document, limits configdocument.Limits, edits []compose.ProjectionEdit, order *pipackage.OrderObservation) (aggregate.Document, []compose.Participant, error) {
	if err := compose.ValidateImage(ctx, before, limits); err != nil {
		return aggregate.Document{}, nil, err
	}
	var sequences []configdocument.JSONArrayEdit
	preparationLimits := limits
	if order != nil {
		preparationLimits.MaximumBytes = min(preparationLimits.MaximumBytes, pipackage.MaximumSettingsBytes)
		if err := validateNative16OrderBaseline(before, *order); err != nil {
			return aggregate.Document{}, nil, err
		}
		candidate, exists := order.Candidate()
		expected := aggregate.AbsentDocument()
		if exists {
			expected = aggregate.ExistingDocument(candidate)
		}
		var err error
		sequences, err = native16SequenceEdits(ctx, before, expected, limits, *order)
		if err != nil {
			return aggregate.Document{}, nil, err
		}
	}
	expected, participants, err := compose.PrepareSelectedImage(ctx, before, preparationLimits, edits, sequences)
	if err != nil {
		return aggregate.Document{}, nil, err
	}
	if order != nil {
		if err := order.VerifyPackageContent(expected.Content(), expected.Exists()); err != nil {
			return aggregate.Document{}, nil, err
		}
	}
	return expected, participants, nil
}

// ReconstructNative16Selected validates legacy semantic evidence before lowering
// it to the same positional comparison used by other selected arrays.
func ReconstructNative16Selected(ctx context.Context, input journaldocument.ComparisonInput, participants []compose.Participant, order *pipackage.OrderObservation) (journaldocument.SelectedCandidate, error) {
	for _, image := range []aggregate.Document{input.Before, input.Expected} {
		if err := compose.ValidateImage(ctx, image, input.Limits); err != nil {
			return journaldocument.SelectedCandidate{}, err
		}
	}
	var sequences []configdocument.JSONArrayEdit
	if order != nil {
		if input.Expected.ContentSize() > pipackage.MaximumSettingsBytes {
			return journaldocument.SelectedCandidate{}, fmt.Errorf("Pi expected image exceeds the settings byte limit")
		}
		if err := validateNative16OrderBaseline(input.Before, *order); err != nil {
			return journaldocument.SelectedCandidate{}, err
		}
		if err := order.VerifyPackageContent(input.Expected.Content(), input.Expected.Exists()); err != nil {
			return journaldocument.SelectedCandidate{}, err
		}
		var err error
		sequences, err = native16SequenceEdits(ctx, input.Before, input.Expected, input.Limits, *order)
		if err != nil {
			return journaldocument.SelectedCandidate{}, err
		}
	}
	return journaldocument.ReconstructSelected(ctx, input, participants, sequences)
}

func validateNative16OrderBaseline(before aggregate.Document, order pipackage.OrderObservation) error {
	captured, err := order.CaptureInput()
	if err != nil {
		return err
	}
	if !captured.Document.Equal(before) {
		return fmt.Errorf("Pi order does not describe the original document")
	}
	capability, err := native16PiOrderCapability(captured.Scope)
	if err != nil {
		return err
	}
	if !captured.Capability.Equal(capability) {
		return fmt.Errorf("Pi order capability differs from native16's interpretation")
	}
	return nil
}

func native16SequenceEdits(ctx context.Context, before, expected aggregate.Document, limits configdocument.Limits, order pipackage.OrderObservation) ([]configdocument.JSONArrayEdit, error) {
	if !order.Changed() {
		return nil, nil
	}
	selected, err := order.SelectedSequenceEdit()
	if err != nil {
		return nil, err
	}
	projection := selected.Projection()
	syntax, err := configdocument.ParseSyntax(string(projection.Grammar()))
	if err != nil {
		return nil, err
	}
	if !before.Exists() || !expected.Exists() {
		return nil, fmt.Errorf("selected native array requires an existing document")
	}
	left, err := configdocument.Parse(ctx, before.Content(), syntax, limits)
	if err != nil {
		return nil, err
	}
	right, err := configdocument.Parse(ctx, expected.Content(), syntax, limits)
	if err != nil {
		return nil, err
	}
	sequence, err := configdocument.NewJSONArrayEdit(ctx, left, right, projection.ArrayPath(), selected.RemovedSlots(), selected.SurvivorOrder())
	if err != nil {
		return nil, err
	}
	return []configdocument.JSONArrayEdit{sequence}, nil
}
