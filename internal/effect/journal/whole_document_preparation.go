package journal

import (
	"context"
	"fmt"
	"slices"

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
func PrepareWholeDocument(ctx context.Context, input WholeDocumentPreparationInput, mutation ManagedPathMutation, payloads payload.PayloadSet) (DocumentComparison, error) {
	if err := validateComparisonDocument(ctx, input.Before, input.Limits); err != nil {
		return DocumentComparison{}, err
	}
	if err := mutation.validate(); err != nil {
		return DocumentComparison{}, err
	}
	facts := mutation.facts()
	mode := facts.expectedMode
	if mutation.kind() == pathMutationRemove {
		mode = facts.previous.FileMode()
	}
	if err := profile.ValidateHostConfigDocumentOccupancy(facts.subject, facts.consumerTargets, facts.scope, facts.destination, facts.contentKind, realization.PathPermissionsExact, mode); err != nil {
		return DocumentComparison{}, err
	}
	if err := validateWholeDocumentPrevious(facts); err != nil {
		return DocumentComparison{}, err
	}
	if _, err := recovery.NewPartialDocumentState(input.Before.Exists(), input.BeforeMode, nil); err != nil {
		return DocumentComparison{}, err
	}
	if mutation.kind() == pathMutationCreate {
		if input.Before.Exists() {
			return DocumentComparison{}, fmt.Errorf("whole document creation requires an absent original")
		}
	} else {
		if !input.Before.Exists() {
			return DocumentComparison{}, fmt.Errorf("whole document mutation requires an existing original")
		}
		actual := artifact.HashFileContentWithExecutable(input.Before.Content(), input.BeforeMode.FileMode()&0o111 != 0)
		if actual != facts.liveHash {
			return DocumentComparison{}, fmt.Errorf("whole document original does not match the observed live identity")
		}
	}

	expected := aggregate.AbsentDocument()
	var expectedMode *recovery.PermissionMode
	switch mutation.kind() {
	case pathMutationCreate, pathMutationReplace:
		value, found := payloads.LookupSubject(facts.subject)
		file, regular := value.File()
		if !found || !regular {
			return DocumentComparison{}, fmt.Errorf("whole document mutation requires its exact file payload")
		}
		if file.ContentSize() > input.Limits.MaximumBytes {
			return DocumentComparison{}, fmt.Errorf("whole document payload exceeds the selected byte limit")
		}
		if file.Mode() != facts.expectedMode {
			return DocumentComparison{}, fmt.Errorf("whole document payload mode does not match the prepared mutation")
		}
		if err := value.VerifyHash(facts.desiredHash, facts.destination); err != nil {
			return DocumentComparison{}, err
		}
		expected = aggregate.ExistingDocument(file.Bytes())
		expectedMode = recovery.NewPermissionMode(facts.expectedMode)
	case pathMutationRecord:
		if facts.liveHash != facts.desiredHash || input.BeforeMode.FileMode() != facts.expectedMode {
			return DocumentComparison{}, fmt.Errorf("whole document record does not match desired identity and mode")
		}
		expected, expectedMode = input.Before, input.BeforeMode
	case pathMutationRemove:
		if facts.liveHash != facts.previous.ContentHash() || input.BeforeMode.FileMode() != facts.previous.FileMode() {
			return DocumentComparison{}, fmt.Errorf("whole document removal does not match its previous baseline")
		}
	}
	return NewWholeDocumentComparison(ctx, DocumentComparisonInput{
		Before: input.Before, Expected: expected,
		BeforeMode: input.BeforeMode, ExpectedMode: expectedMode, Limits: input.Limits,
	})
}

func validateWholeDocumentPrevious(facts managedPathMutationFacts) error {
	if facts.previous == nil {
		return nil
	}
	previous := *facts.previous
	if previous.Subject() != facts.subject || previous.Scope() != facts.scope || previous.Destination() != facts.destination || !slices.Equal(previous.ConsumerTargets(), facts.consumerTargets) {
		return fmt.Errorf("whole document retargeting requires separate creation and retirement mutations")
	}
	return profile.ValidateHostConfigDocumentOccupancy(previous.Subject(), previous.ConsumerTargets(), previous.Scope(), previous.Destination(), previous.ContentKind(), previous.PermissionPolicy(), previous.FileMode())
}
