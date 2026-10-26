package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

// BeforeState retains the supplied complete baseline, not a freshness proof.
func (batch NativeDocumentObservationBatch) BeforeState() durable.Snapshot {
	return batch.before
}

// ValidateBefore checks persisted ownership, not source freshness or a write grant.
func (batch NativeDocumentObservationBatch) ValidateBefore(ctx context.Context) error {
	_, err := batch.Ownership(ctx)
	return err
}

func nativeDocumentClaimBaseline(namespace NativeDocumentNamespace, owned map[string][]ownership.Claim, before map[output.Footprint]struct{}) (pathauthority.Exact, []ownership.Claim, error) {
	exact, known := namespace.observation.Exact()
	if !known {
		missing, provisional := namespace.observation.Provisional()
		if !provisional {
			return pathauthority.Exact{}, nil, fmt.Errorf("native history requires a current namespace observation")
		}
		previous := owned[missing.CandidateKey()]
		if len(before) == 0 && len(previous) == 0 {
			return pathauthority.Exact{}, nil, nil
		}
		if len(previous) == 0 || !missing.MatchesMissingExact(previous[0].Address().PathAuthority()) {
			return pathauthority.Exact{}, nil, fmt.Errorf("native history differs from the missing namespace")
		}
		exact = previous[0].Address().PathAuthority()
	}
	claims := owned[exact.Key()]
	if !sameDocumentClaimFootprints(before, claims) {
		return pathauthority.Exact{}, nil, fmt.Errorf("native capture before membership differs from current owned claims")
	}
	for _, claim := range claims {
		if !claim.Address().PathAuthority().Equal(exact) || claim.State() != ownership.ClaimActive {
			return pathauthority.Exact{}, nil, fmt.Errorf("native capture requires active claims at the captured namespace")
		}
	}
	return exact, claims, nil
}
