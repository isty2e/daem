package document

import (
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// ProvisionalAcquire is a document's candidate alias/intent pair, not a claim
// grant. The containing group still validates its owner, phase and membership.
type ProvisionalAcquire struct {
	alias  aggregate.DocumentAddress
	intent ownership.ProvisionalAcquireIntent
}

func NewProvisionalAcquire(alias aggregate.DocumentAddress, intent ownership.ProvisionalAcquireIntent) ProvisionalAcquire {
	return ProvisionalAcquire{alias: alias, intent: intent}
}

func (acquire ProvisionalAcquire) Alias() aggregate.DocumentAddress { return acquire.alias }

func (acquire ProvisionalAcquire) Intent() ownership.ProvisionalAcquireIntent { return acquire.intent }

func SameClaimFootprints(expected map[output.Footprint]struct{}, claims []ownership.Claim) bool {
	if len(expected) != len(claims) {
		return false
	}
	for _, claim := range claims {
		if _, found := expected[claim.Address().Footprint()]; !found {
			return false
		}
	}
	return true
}
