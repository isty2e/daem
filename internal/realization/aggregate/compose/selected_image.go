package compose

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// PrepareSelectedImage completes all disjoint projection and array edits from
// one original image. Protocol semantic verification remains with normalizers.
func PrepareSelectedImage(ctx context.Context, before aggregate.Document, limits configdocument.Limits, edits []ProjectionEdit, sequences []configdocument.JSONArrayEdit) (aggregate.Document, []Participant, error) {
	if err := ValidateImage(ctx, before, limits); err != nil {
		return aggregate.Document{}, nil, err
	}
	participants := make([]Participant, len(edits))
	for index, edit := range edits {
		participants[index] = edit.Participant()
	}
	retained, err := DisjointSelectedSequences(participants, sequences)
	if err != nil {
		return aggregate.Document{}, nil, err
	}
	expected := before
	if len(edits) != 0 {
		candidate, err := Render(ctx, before, limits, edits)
		if err != nil {
			return aggregate.Document{}, nil, err
		}
		expected = candidate.Document()
	}
	for _, sequence := range retained {
		if !expected.Exists() {
			return aggregate.Document{}, nil, fmt.Errorf("selected native array requires an existing document")
		}
		current, err := configdocument.Parse(ctx, expected.Content(), sequence.Syntax(), limits)
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

func DisjointSelectedSequences(participants []Participant, sequences []configdocument.JSONArrayEdit) ([]configdocument.JSONArrayEdit, error) {
	retained := slices.Clone(sequences)
	slices.SortFunc(retained, func(left, right configdocument.JSONArrayEdit) int {
		return strings.Compare(left.ArrayPath().String(), right.ArrayPath().String())
	})
	for index, sequence := range retained {
		for _, previous := range retained[:index] {
			if sequence.ArrayPath().Overlaps(previous.ArrayPath()) {
				return nil, fmt.Errorf("selected native array edits overlap")
			}
		}
		for _, participant := range participants {
			for _, area := range participant.RewriteAreas() {
				if area.Overlaps(sequence.ArrayPath()) {
					return nil, fmt.Errorf("selected aggregate area overlaps native array order")
				}
			}
		}
	}
	return retained, nil
}

func ValidateImage(ctx context.Context, document aggregate.Document, limits configdocument.Limits) error {
	if ctx == nil {
		return fmt.Errorf("document comparison context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if err := document.Validate(); err != nil {
		return err
	}
	if document.ContentSize() > limits.MaximumBytes {
		return fmt.Errorf("document comparison exceeds the selected byte limit")
	}
	return nil
}
