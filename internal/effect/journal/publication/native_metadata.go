package publication

import (
	"context"
	"fmt"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func (captured CapturedNativeJournal) OrdinaryClaimTransitions() ([]ownershipmutation.ClaimTransition, error) {
	if err := captured.Validate(); err != nil {
		return nil, err
	}
	return captured.journal.ClaimTransitions()
}

func (captured CapturedNativeJournal) OrdinaryProvisionalAcquires() ([]ownership.ProvisionalAcquireIntent, error) {
	if err := captured.Validate(); err != nil {
		return nil, err
	}
	return captured.journal.ProvisionalAcquires()
}

func (captured CapturedNativeJournal) PreparationClaims(ctx context.Context) (ownership.ClaimConvergence, error) {
	if ctx == nil {
		return ownership.ClaimConvergence{}, fmt.Errorf("native preparation context is required")
	}
	transitions, err := captured.OrdinaryClaimTransitions()
	if err != nil {
		return ownership.ClaimConvergence{}, err
	}
	return journalqualification.PrepareClaims(ctx, captured.qualified.Records(), transitions)
}
