package preparation

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
)

// MatchPendingCarrierInstall resolves one exact pending acquisition for an action.
func MatchPendingCarrierInstall(current durable.Snapshot, action reconciliation.RelationAction) (durablecarrier.PendingCarrierInstall, bool) {
	for _, pending := range current.PendingCarrierInstalls() {
		if pending.Identity().ExactEqual(action.CarrierIdentity()) && pending.InstallRequest().Equal(action.RouteRequest()) {
			return pending, true
		}
	}
	return durablecarrier.PendingCarrierInstall{}, false
}

// MatchPendingCarrierInstallCompletion resolves one exact no-invocation completion fact.
func MatchPendingCarrierInstallCompletion(current durable.Snapshot, action reconciliation.RelationAction) (durablecarrier.PendingCarrierInstall, observerelation.CorrelationResult, bool) {
	if action.Kind() != reconciliation.ActionNoOp {
		return durablecarrier.PendingCarrierInstall{}, observerelation.CorrelationResult{}, false
	}
	correlation, present := action.Correlation()
	if !present {
		return durablecarrier.PendingCarrierInstall{}, observerelation.CorrelationResult{}, false
	}
	pending, matched := MatchPendingCarrierInstall(current, action)
	if !matched {
		return durablecarrier.PendingCarrierInstall{}, observerelation.CorrelationResult{}, false
	}
	return pending, correlation, true
}

type ProjectCarrierCompletion struct {
	after   durable.Snapshot
	claims  []durablecarrier.ManagedCarrierClaim
	changed bool
}

func (completion ProjectCarrierCompletion) AfterState() durable.Snapshot { return completion.after }

func (completion ProjectCarrierCompletion) Claims() []durablecarrier.ManagedCarrierClaim {
	return slices.Clone(completion.claims)
}

func (completion ProjectCarrierCompletion) Changed() bool { return completion.changed }

func CompleteProjectCarrierInstalls(current durable.Snapshot, actions []reconciliation.RelationAction) (ProjectCarrierCompletion, error) {
	claims, err := promotedProjectCarrierClaims(current, actions)
	if err != nil {
		return ProjectCarrierCompletion{}, err
	}
	after, changed, err := current.WithPromotedCarrierClaims(claims)
	if err != nil {
		return ProjectCarrierCompletion{}, err
	}
	return ProjectCarrierCompletion{after: after, claims: claims, changed: changed}, nil
}

func promotedProjectCarrierClaims(current durable.Snapshot, actions []reconciliation.RelationAction) ([]durablecarrier.ManagedCarrierClaim, error) {
	claims := make([]durablecarrier.ManagedCarrierClaim, 0, len(actions))
	for _, action := range actions {
		if action.CarrierIdentity().ClaimStore() != durablecarrier.ClaimStoreProjectStatefile {
			continue
		}
		pending, correlation, matched := MatchPendingCarrierInstallCompletion(current, action)
		if !matched {
			continue
		}
		claim, err := durablecarrier.ClaimAfterObservedInstall(pending, correlation, current.ManagedCarrierClaims())
		if err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	return claims, nil
}

func AfterRetiredProjectCarrierClaims(snapshot durable.Snapshot, claims []durablecarrier.ManagedCarrierClaim) (durable.Snapshot, int, error) {
	next := snapshot
	for index, claim := range claims {
		for previous := range index {
			if claims[previous].ExactEqual(claim) {
				return durable.Snapshot{}, 0, fmt.Errorf("retired project carrier claim[%d] duplicates claim[%d]", index, previous)
			}
		}
		updated, changed, err := next.WithoutManagedCarrierClaim(claim)
		if err != nil {
			return durable.Snapshot{}, 0, fmt.Errorf("retire project carrier claim[%d]: %w", index, err)
		}
		if !changed {
			return durable.Snapshot{}, 0, fmt.Errorf("retire project carrier claim[%d]: exact claim is absent", index)
		}
		next = updated
	}
	return next, len(claims), nil
}
