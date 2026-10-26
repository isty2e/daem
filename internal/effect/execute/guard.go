package execute

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/reconcile"
)

// ErrNativeOperationRequired marks work the ordinary Apply pipeline cannot execute.
var ErrNativeOperationRequired = errors.New("native document narrowing requires its containing execution pipeline")

func rejectUnsupportedNativeNarrowings(actions []reconcile.NativeNarrowingAction) error {
	if len(actions) != 0 {
		return ErrNativeOperationRequired
	}
	return nil
}

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
	if err := rejectUnsupportedNativeNarrowings(planResult.NativeNarrowings()); err != nil {
		return err
	}

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
