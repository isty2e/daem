package selecteddoc

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

type CompletedSequence struct {
	document         aggregate.DocumentAddress
	edit             configdocument.JSONArrayEdit
	identityContract string
}

func (sequence CompletedSequence) DocumentAddress() aggregate.DocumentAddress {
	return sequence.document
}
func (sequence CompletedSequence) ArrayEdit() configdocument.JSONArrayEdit { return sequence.edit }

// VerifiedSelectedImage is complete pure evidence, not a write capability. Its
// expected image and positional witnesses exist only after every normalizer passes.
type VerifiedSelectedImage struct {
	original     aggregate.Document
	expected     aggregate.Document
	participants []compose.Participant
	sequences    []CompletedSequence
	limits       configdocument.Limits
	initialized  bool
}

func (completed VerifiedSelectedImage) Validate() error {
	if !completed.initialized {
		return fmt.Errorf("complete verified selected image is required")
	}
	return nil
}

func (completed VerifiedSelectedImage) Original() aggregate.Document  { return completed.original }
func (completed VerifiedSelectedImage) Expected() aggregate.Document  { return completed.expected }
func (completed VerifiedSelectedImage) Limits() configdocument.Limits { return completed.limits }
func (completed VerifiedSelectedImage) Participants() []compose.Participant {
	return slices.Clone(completed.participants)
}

func (completed VerifiedSelectedImage) Sequences() []CompletedSequence {
	return slices.Clone(completed.sequences)
}

func CompleteSelectedImage(ctx context.Context, before aggregate.Document, limits configdocument.Limits, projections []compose.ProjectionEdit, selected []SelectedDocumentEdit) (VerifiedSelectedImage, error) {
	for _, edit := range selected {
		if err := edit.Validate(); err != nil {
			return VerifiedSelectedImage{}, err
		}
		if before.ContentSize() > edit.maximumBytes {
			return VerifiedSelectedImage{}, fmt.Errorf("selected sequence original exceeds its host document byte limit")
		}
		if !before.Equal(edit.original) {
			return VerifiedSelectedImage{}, fmt.Errorf("selected sequence does not describe the original document")
		}
	}
	if err := compose.ValidateImage(ctx, before, limits); err != nil {
		return VerifiedSelectedImage{}, err
	}
	var arrays []configdocument.JSONArrayEdit
	var sequences []CompletedSequence
	for _, edit := range selected {
		if !edit.Changed() {
			continue
		}
		syntax, err := configdocument.ParseSyntax(string(edit.selected.Projection().Grammar()))
		if err != nil {
			return VerifiedSelectedImage{}, err
		}
		loaded, err := configdocument.Parse(ctx, before.Content(), syntax, limits)
		if err != nil {
			return VerifiedSelectedImage{}, err
		}
		array, err := configdocument.PrepareJSONArrayEdit(ctx, loaded, edit.selected.Projection().ArrayPath(), edit.selected.RemovedSlots(), edit.selected.SurvivorOrder())
		if err != nil {
			return VerifiedSelectedImage{}, err
		}
		duplicate := false
		for index, previous := range arrays {
			if previous.ArrayPath() != array.ArrayPath() {
				continue
			}
			if !previous.Equal(array) || sequences[index].identityContract != edit.selected.MemberIdentityContract() {
				return VerifiedSelectedImage{}, fmt.Errorf("selected classes have incompatible mutations of one sequence")
			}
			duplicate = true
		}
		if !duplicate {
			arrays = append(arrays, array)
			sequences = append(sequences, CompletedSequence{document: edit.document, edit: array, identityContract: edit.selected.MemberIdentityContract()})
		}
	}
	expected, participants, err := compose.PrepareSelectedImage(ctx, before, limits, projections, arrays)
	if err != nil {
		return VerifiedSelectedImage{}, err
	}
	for _, edit := range selected {
		if err := ctx.Err(); err != nil {
			return VerifiedSelectedImage{}, err
		}
		if err := edit.origin.verify(edit.selected, expected); err != nil {
			return VerifiedSelectedImage{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return VerifiedSelectedImage{}, err
	}
	return VerifiedSelectedImage{original: before, expected: expected, participants: participants, sequences: sequences, limits: limits, initialized: true}, nil
}
