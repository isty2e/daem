package journal

import (
	"context"
	"fmt"
	"slices"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
)

type WholeDocumentPreparationInput struct {
	Before     aggregate.Document
	BeforeMode *recovery.PermissionMode
	Limits     configdocument.Limits
}

// PrepareWholeDocument derives expected images from capture mutations and exact
// payloads. Loaded originals do not grant namespace or managed-state authority.
func PrepareWholeDocument(ctx context.Context, input WholeDocumentPreparationInput, mutation journalrecord.ManagedPathMutation, payloads payload.PayloadSet) (journaldocument.Comparison, error) {
	if err := journaldocument.ValidateImage(ctx, input.Before, input.Limits); err != nil {
		return journaldocument.Comparison{}, err
	}
	facts, err := wholeDocumentMutationFacts(mutation)
	if err != nil {
		return journaldocument.Comparison{}, err
	}
	if _, err := recovery.NewPartialDocumentState(input.Before.Exists(), input.BeforeMode, nil); err != nil {
		return journaldocument.Comparison{}, err
	}
	if mutation.Kind() == journalrecord.CaptureCreate {
		if input.Before.Exists() {
			return journaldocument.Comparison{}, fmt.Errorf("whole document creation requires an absent original")
		}
	} else {
		if !input.Before.Exists() {
			return journaldocument.Comparison{}, fmt.Errorf("whole document mutation requires an existing original")
		}
		actual := artifact.HashFileContentWithExecutable(input.Before.Content(), input.BeforeMode.FileMode()&0o111 != 0)
		if actual != facts.LiveHash() {
			return journaldocument.Comparison{}, fmt.Errorf("whole document original does not match the observed live identity")
		}
	}

	expected := aggregate.AbsentDocument()
	var expectedMode *recovery.PermissionMode
	switch mutation.Kind() {
	case journalrecord.CaptureCreate, journalrecord.CaptureReplace:
		value, found := payloads.LookupSubject(facts.Subject())
		file, regular := value.File()
		if !found || !regular {
			return journaldocument.Comparison{}, fmt.Errorf("whole document mutation requires its exact file payload")
		}
		if file.ContentSize() > input.Limits.MaximumBytes {
			return journaldocument.Comparison{}, fmt.Errorf("whole document payload exceeds the selected byte limit")
		}
		if file.Mode() != facts.ExpectedMode() {
			return journaldocument.Comparison{}, fmt.Errorf("whole document payload mode does not match the prepared mutation")
		}
		if err := value.VerifyHash(facts.DesiredHash(), facts.Destination()); err != nil {
			return journaldocument.Comparison{}, err
		}
		expected = aggregate.ExistingDocument(file.Bytes())
		expectedMode = recovery.NewPermissionMode(facts.ExpectedMode())
	case journalrecord.CaptureRecord:
		if facts.LiveHash() != facts.DesiredHash() || input.BeforeMode.FileMode() != facts.ExpectedMode() {
			return journaldocument.Comparison{}, fmt.Errorf("whole document record does not match desired identity and mode")
		}
		expected, expectedMode = input.Before, input.BeforeMode
	case journalrecord.CaptureRemove:
		previous, _ := facts.PreviousState()
		if facts.LiveHash() != previous.ContentHash() || input.BeforeMode.FileMode() != previous.FileMode() {
			return journaldocument.Comparison{}, fmt.Errorf("whole document removal does not match its previous baseline")
		}
	}
	return journaldocument.NewWholeComparison(ctx, journaldocument.ComparisonInput{
		Before: input.Before, Expected: expected,
		BeforeMode: input.BeforeMode, ExpectedMode: expectedMode, Limits: input.Limits,
	})
}

func wholeDocumentMutationFacts(mutation journalrecord.ManagedPathMutation) (journalrecord.ManagedPathMutation, error) {
	if err := mutation.Validate(); err != nil {
		return journalrecord.ManagedPathMutation{}, err
	}
	facts := mutation
	mode := facts.ExpectedMode()
	if mutation.Kind() == journalrecord.CaptureRemove {
		previous, _ := facts.PreviousState()
		mode = previous.FileMode()
	}
	if err := profile.ValidateHostConfigDocumentOccupancy(facts.Subject(), facts.ConsumerTargets(), facts.Scope(), facts.Destination(), facts.ContentKind(), realization.PathPermissionsExact, mode); err != nil {
		return journalrecord.ManagedPathMutation{}, err
	}
	if err := validateWholeDocumentPrevious(facts); err != nil {
		return journalrecord.ManagedPathMutation{}, err
	}
	return facts, nil
}

func validateWholeDocumentPrevious(facts journalrecord.ManagedPathMutation) error {
	previous, present := facts.PreviousState()
	if !present {
		return nil
	}
	if previous.Subject() != facts.Subject() || previous.Scope() != facts.Scope() || previous.Destination() != facts.Destination() || !slices.Equal(previous.ConsumerTargets(), facts.ConsumerTargets()) {
		return fmt.Errorf("whole document retargeting requires separate creation and retirement mutations")
	}
	return profile.ValidateHostConfigDocumentOccupancy(previous.Subject(), previous.ConsumerTargets(), previous.Scope(), previous.Destination(), previous.ContentKind(), previous.PermissionPolicy(), previous.FileMode())
}
