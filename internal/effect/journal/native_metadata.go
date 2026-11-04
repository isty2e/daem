package journal

import (
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
