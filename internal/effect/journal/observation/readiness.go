package observation

import (
	"context"
	"fmt"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

// BeforeState retains the supplied complete baseline, not a freshness proof.
func (batch Batch) BeforeState() durable.Snapshot {
	return batch.before
}

// ValidateBefore checks persisted ownership, not source freshness or a write grant.
func (batch Batch) ValidateBefore(ctx context.Context) error {
	_, err := batch.Ownership(ctx)
	return err
}

// ValidatePlanningContext binds shared planner facts to the retained observation;
// equal values do not supply freshness or execution authority.
func (batch Batch) ValidatePlanningContext(ctx context.Context, owner stateauthority.Authority, registry ownership.Registry, budget *physicalwork.Budget) error {
	if !batch.initialized || !batch.owner.Equal(owner) || batch.budget != budget {
		return fmt.Errorf("native planning context differs from its observed owner, registry or allocation")
	}
	scope, err := batch.RegistryScope()
	if err != nil {
		return err
	}
	if same, err := scope.Matches(batch.registry, registry); err != nil {
		return err
	} else if !same {
		return fmt.Errorf("native planning ownership scope differs from its observation")
	}
	return batch.ValidateBefore(ctx)
}

func nativeDocumentClaimBaseline(namespace journalnamespace.DocumentNamespace, owned map[string][]ownership.Claim, before map[output.Footprint]struct{}) (pathauthority.Exact, []ownership.Claim, error) {
	exact, known := namespace.Observation().Exact()
	if !known {
		missing, provisional := namespace.Observation().Provisional()
		if !provisional {
			return pathauthority.Exact{}, nil, fmt.Errorf("native history requires a current namespace observation")
		}
		previous := owned[missing.CandidateKey()]
		if legacy, present := missing.LegacyComparisonKey(); present {
			historical := owned[legacy]
			if len(previous) != 0 && len(historical) != 0 {
				return pathauthority.Exact{}, nil, fmt.Errorf("native history has ambiguous legacy and native namespaces")
			}
			if len(previous) == 0 {
				previous = historical
			}
		}
		if len(before) == 0 && len(previous) == 0 {
			return pathauthority.Exact{}, nil, nil
		}
		if len(previous) == 0 && len(before) != 0 {
			return pathauthority.Exact{}, nil, ownership.ErrClaimMissing
		}
		if len(previous) == 0 || !missing.MatchesMissingExact(previous[0].Address().PathAuthority()) {
			return pathauthority.Exact{}, nil, fmt.Errorf("native history differs from the missing namespace")
		}
		exact = previous[0].Address().PathAuthority()
	}
	claims := owned[exact.Key()]
	if !journaldocument.SameClaimFootprints(before, claims) {
		present := make(map[output.Footprint]bool, len(claims))
		for _, claim := range claims {
			present[claim.Address().Footprint()] = true
		}
		for footprint := range before {
			if !present[footprint] {
				return pathauthority.Exact{}, nil, ownership.ErrClaimMissing
			}
		}
		return pathauthority.Exact{}, nil, fmt.Errorf("native capture before membership differs from current owned claims")
	}
	for _, claim := range claims {
		if claim.State() != ownership.ClaimActive {
			return pathauthority.Exact{}, nil, ownership.ErrClaimMissing
		}
		if !claim.Address().PathAuthority().Equal(exact) {
			return pathauthority.Exact{}, nil, fmt.Errorf("native capture requires active claims at the captured namespace")
		}
	}
	return exact, claims, nil
}
