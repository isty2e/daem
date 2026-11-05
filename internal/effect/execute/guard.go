package execute

import (
	"fmt"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
)

// ErrNativeOperationRequired marks work the ordinary Apply pipeline cannot execute.
var ErrNativeOperationRequired = effectpreparation.ErrNativeOperationRequired

func rejectUnsupportedNativeInput(input ApplyInput) error {
	if len(input.NativeNarrowings) != 0 || input.CurrentState.ContainsHostConfig() {
		return ErrNativeOperationRequired
	}
	for _, effect := range input.ManagedPathEffects {
		if aggregate.IsHostConfigDocumentSubject(effect.Subject()) {
			return ErrNativeOperationRequired
		}
	}
	for _, effect := range input.AggregateEffects {
		if _, native := aggregate.HostConfigSyntaxForCodec(effect.CodecContractID()); native {
			return ErrNativeOperationRequired
		}
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

// RejectUnsupportedExecutableActions rejects work outside the ordinary execution contract.
func RejectUnsupportedExecutableActions(planResult reconcile.Result) error {
	if planResult.ContainsHostConfig() {
		return ErrNativeOperationRequired
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
	if _, err := effectpreparation.AggregateEffects(executableAggregates); err != nil {
		return err
	}
	return nil
}
