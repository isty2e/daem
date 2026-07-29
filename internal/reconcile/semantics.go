package reconcile

import (
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
)

// IsOwnershipBlock reports whether the reason denies global managed-address authority.
func (reason ActionReason) IsOwnershipBlock() bool {
	switch reason {
	case ReasonOwnershipObservationMissing,
		ReasonOwnershipClaimMissing,
		ReasonOwnershipConflict,
		ReasonOwnershipReserved,
		ReasonOwnershipStateConflict:
		return true
	default:
		return false
	}
}

func (reason ActionReason) isLockReadinessError() bool {
	return reason == ReasonMissingLock ||
		reason == ReasonStaleLock ||
		reason == ReasonUnexpectedLockSubject ||
		reason == ReasonAggregateLockBlocked
}

// HasErrors reports whether any decision in the result is blocked.
func (result Result) HasErrors() bool {
	for _, decision := range result.managedPaths {
		if decision.IsBlocked() {
			return true
		}
	}
	for _, decision := range result.aggregates {
		if decision.IsBlocked() {
			return true
		}
	}
	if result.HasBlockedRelations() {
		return true
	}
	if result.HasBlockedRelationOrders() {
		return true
	}
	if result.HasBlockedCarrierAdoptions() {
		return true
	}
	if result.HasBlockedCarrierAbsences() {
		return true
	}
	for _, action := range result.delegates {
		if action.Disposition() == DelegateBlocked {
			return true
		}
	}
	return false
}

// HasBlockedRelationOrders reports whether any physical sequence lacks a safe
// current planning interpretation.
func (result Result) HasBlockedRelationOrders() bool {
	for _, decision := range result.relationOrders {
		if decision.BlocksOrdinaryApply() {
			return true
		}
	}
	return false
}

// FirstBlockedRelationOrder returns the first canonical blocked sequence.
func (result Result) FirstBlockedRelationOrder() (RelationOrderDecision, bool) {
	for _, decision := range result.relationOrders {
		if decision.BlocksOrdinaryApply() {
			return decision, true
		}
	}
	return RelationOrderDecision{}, false
}

// HasBlockedCarrierAdoptions reports whether adoption authority adds a block
// after the matching relation decision has applied host-observation policy.
func (result Result) HasBlockedCarrierAdoptions() bool {
	_, blocked := result.FirstBlockedCarrierAdoption()
	return blocked
}

// FirstBlockedCarrierAdoption returns the first adoption-specific authority
// block not already governed by a non-blocking relation decision.
func (result Result) FirstBlockedCarrierAdoption() (carrieradoption.Action, bool) {
	for _, action := range result.adoptions {
		if !action.BlocksOrdinaryApply() {
			continue
		}
		if action.Result() != carrieradoption.ResultClaimConflict &&
			result.hasNonBlockingRelation(action.CarrierIdentity()) {
			continue
		}
		return action, true
	}
	return carrieradoption.Action{}, false
}

// HasBlockedCarrierAbsences reports whether desired carrier absence refuses convergence.
func (result Result) HasBlockedCarrierAbsences() bool {
	for _, action := range result.absences {
		if action.BlocksOrdinaryApply() {
			return true
		}
	}
	return false
}

// HasBlockedRelations reports whether a relation decision prevents ordinary apply.
func (result Result) HasBlockedRelations() bool {
	_, blocked := result.FirstBlockedRelation()
	return blocked
}

// FirstBlockedRelation returns the first relation block not satisfied by an
// exact state-only adoption decision for the same carrier relation.
func (result Result) FirstBlockedRelation() (RelationAction, bool) {
	for _, action := range result.relations {
		if !action.BlocksOrdinaryApply() {
			continue
		}
		if action.Reason() == ReasonPresentUnclaimed &&
			result.hasEligibleCarrierAdoption(action.CarrierIdentity()) {
			continue
		}
		return action, true
	}
	return RelationAction{}, false
}

func (result Result) hasEligibleCarrierAdoption(identity durablecarrier.ManagedCarrierIdentity) bool {
	for _, action := range result.adoptions {
		if action.StateOnly() && action.CarrierIdentity().ExactEqual(identity) {
			return true
		}
	}
	return false
}

func (result Result) hasNonBlockingRelation(identity durablecarrier.ManagedCarrierIdentity) bool {
	for _, action := range result.relations {
		if action.CarrierIdentity().ExactEqual(identity) {
			return !action.BlocksOrdinaryApply()
		}
	}
	return false
}

// HasLockReadinessErrors reports whether any decision blocks on lock readiness.
func (result Result) HasLockReadinessErrors() bool {
	for _, decision := range result.managedPaths {
		if decision.IsBlocked() && decision.Reason().isLockReadinessError() {
			return true
		}
	}
	for _, decision := range result.aggregates {
		if decision.IsBlocked() && decision.Reason().isLockReadinessError() {
			return true
		}
	}
	return false
}

// PendingManagedPaths returns typed path decisions that prevent a clean result.
func (result Result) PendingManagedPaths() []ManagedPathDecision {
	decisions := make([]ManagedPathDecision, 0, len(result.managedPaths))
	for _, decision := range result.managedPaths {
		if decision.IsPending() {
			decisions = append(decisions, decision)
		}
	}
	return decisions
}

// MutatingManagedPaths returns typed path decisions that change host or state.
func (result Result) MutatingManagedPaths() []ManagedPathDecision {
	decisions := make([]ManagedPathDecision, 0, len(result.managedPaths))
	for _, decision := range result.managedPaths {
		if decision.MutatesHost() || decision.MutatesState() {
			decisions = append(decisions, decision)
		}
	}
	return decisions
}

// PendingAggregates returns aggregate decisions that prevent a clean result.
func (result Result) PendingAggregates() []AggregateDecision {
	decisions := make([]AggregateDecision, 0, len(result.aggregates))
	for _, decision := range result.aggregates {
		if !decision.IsNoOp() {
			decisions = append(decisions, decision)
		}
	}
	return decisions
}

// MutatingAggregates returns aggregate decisions that change host or state.
func (result Result) MutatingAggregates() []AggregateDecision {
	decisions := make([]AggregateDecision, 0, len(result.aggregates))
	for _, decision := range result.aggregates {
		if decision.MutatesHost() || decision.MutatesState() {
			decisions = append(decisions, decision)
		}
	}
	return decisions
}

// ProjectionDecisionCount returns managed-path and aggregate-subject decisions.
func (result Result) ProjectionDecisionCount() int {
	count := len(result.managedPaths)
	for _, decision := range result.aggregates {
		count += len(decision.Subjects())
	}
	return count
}

// DecisionCount returns every typed decision in this complete result.
func (result Result) DecisionCount() int {
	return result.ProjectionDecisionCount() +
		len(result.relations) +
		len(result.relationOrders) +
		len(result.adoptions) +
		len(result.absences) +
		len(result.delegates)
}
