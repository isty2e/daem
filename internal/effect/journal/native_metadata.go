package journal

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func (captured CapturedNativeJournal) OrdinaryClaimTransitions() ([]ownershipmutation.ClaimTransition, error) {
	if err := captured.Validate(); err != nil {
		return nil, err
	}
	return canonicalClaimTransitions(captured.journal.record.ClaimTransitions)
}

func (captured CapturedNativeJournal) OrdinaryProvisionalAcquires() ([]ownership.ProvisionalAcquireIntent, error) {
	if err := captured.Validate(); err != nil {
		return nil, err
	}
	return canonicalProvisionalAcquireIntents(captured.journal.record.ProvisionalAcquires)
}

func (captured CapturedNativeJournal) PreparationClaims(ctx context.Context) (ownership.ClaimConvergence, error) {
	if ctx == nil {
		return ownership.ClaimConvergence{}, fmt.Errorf("native preparation context is required")
	}
	transitions, err := captured.OrdinaryClaimTransitions()
	if err != nil {
		return ownership.ClaimConvergence{}, err
	}
	prepared, _, _, err := nativeDocumentClaimConvergences(ctx, captured.qualified.records.claims, transitions)
	return prepared, err
}

func (assessment NativeRecoveryAssessment) ExpectedClaimRemovals() []ownership.Claim {
	return slices.Clone(assessment.assessment.expectedRemovals)
}

func (assessment NativeRecoveryAssessment) MissingDocuments() []pathauthority.Provisional {
	return slices.Clone(assessment.assessment.missingHistory)
}
