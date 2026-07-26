package execute

import (
	"fmt"

	"github.com/isty2e/daem/internal/reconcile"
)

// RejectUnsupportedActions rejects planner errors and action shapes execution cannot commit.
func RejectUnsupportedActions(planResult reconcile.Result) error {
	if err := rejectPlanErrorActions(planResult); err != nil {
		return err
	}

	return RejectUnsupportedExecutableActions(planResult)
}

func rejectPlanErrorActions(planResult reconcile.Result) error {
	for _, decision := range planResult.ManagedPaths() {
		if !decision.IsBlocked() {
			continue
		}
		if decision.Detail() != "" {
			return fmt.Errorf("plan contains error action for %q: %s: %s", decision.Destination(), decision.Reason(), decision.Detail())
		}
		return fmt.Errorf("plan contains error action for %q: %s", decision.Destination(), decision.Reason())
	}
	for _, decision := range planResult.Aggregates() {
		if !decision.IsBlocked() {
			continue
		}
		if decision.Detail() != "" {
			return fmt.Errorf("plan contains aggregate error for %q: %s: %s", decision.DocumentAddress().AggregateRoot(), decision.Reason(), decision.Detail())
		}
		return fmt.Errorf("plan contains aggregate error for %q: %s", decision.DocumentAddress().AggregateRoot(), decision.Reason())
	}

	return nil
}

// RejectUnsupportedExecutableActions rejects mutating action shapes execution cannot commit.
func RejectUnsupportedExecutableActions(planResult reconcile.Result) error {
	managedPaths := planResult.ManagedPaths()
	executableManagedPaths := make([]reconcile.ManagedPathDecision, 0, len(managedPaths))
	for _, decision := range managedPaths {
		if decision.IsBlocked() || decision.IsNoOp() {
			continue
		}
		executableManagedPaths = append(executableManagedPaths, decision)
	}
	if _, err := ManagedPathEffects(executableManagedPaths); err != nil {
		return err
	}
	aggregates := planResult.Aggregates()
	executableAggregates := make([]reconcile.AggregateDecision, 0, len(aggregates))
	for _, decision := range aggregates {
		if decision.IsBlocked() || decision.IsNoOp() {
			continue
		}
		executableAggregates = append(executableAggregates, decision)
	}
	if _, err := AggregateEffects(executableAggregates); err != nil {
		return err
	}
	return nil
}
