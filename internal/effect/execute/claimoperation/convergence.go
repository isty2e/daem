package claimoperation

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func Prepare(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
	gate applyprogram.VisibilityGate,
) error {
	if len(transitions) == 0 {
		return nil
	}
	var convergence ownership.ClaimConvergence
	if err := gate.Observe("transition-plan", func() error {
		set, err := ownershipmutation.NewClaimTransitionSet(transitions)
		if err != nil {
			return err
		}
		convergence, err = set.Preparation()
		return err
	}); err != nil {
		return err
	}
	return executeClaimConvergence(ctx, registryStore, convergence, gate,
		"validate ownership claim preparation", "prepare ownership claim", "accept prepared ownership claim", nil)
}

func Finalize(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
	gate applyprogram.VisibilityGate,
) error {
	return FinalizeWithAcceptance(ctx, registryStore, transitions, gate, nil)
}

func FinalizeWithAcceptance(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
	gate applyprogram.VisibilityGate,
	acceptSuccessor func(context.Context, ownership.Registry) error,
) error {
	if len(transitions) == 0 {
		return nil
	}
	var convergence ownership.ClaimConvergence
	if err := gate.Observe("transition-plan", func() error {
		set, err := ownershipmutation.NewClaimTransitionSet(transitions)
		if err != nil {
			return err
		}
		convergence, err = set.Finalization()
		return err
	}); err != nil {
		return err
	}
	return executeClaimConvergence(ctx, registryStore, convergence, gate,
		"validate ownership claim finalization", "finalize ownership claim", "accept finalized ownership claim", acceptSuccessor)
}

func RollbackToBefore(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
	gate applyprogram.VisibilityGate,
) error {
	return RollbackToBeforeWithAcceptance(ctx, registryStore, transitions, gate, nil)
}

func RollbackToBeforeWithAcceptance(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
	gate applyprogram.VisibilityGate,
	acceptSuccessor func(context.Context, ownership.Registry) error,
) error {
	if len(transitions) == 0 {
		return nil
	}
	var convergence ownership.ClaimConvergence
	if err := gate.Observe("transition-plan", func() error {
		set, err := ownershipmutation.NewClaimTransitionSet(transitions)
		if err != nil {
			return err
		}
		convergence, err = set.Rollback()
		return err
	}); err != nil {
		return err
	}
	return executeClaimConvergence(ctx, registryStore, convergence, gate,
		"validate ownership claim rollback", "rollback ownership claim", "accept rolled back ownership claim", acceptSuccessor)
}

func executeClaimConvergence(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	convergence ownership.ClaimConvergence,
	gate applyprogram.VisibilityGate,
	validateDetail string,
	commitDetail string,
	acceptDetail string,
	acceptSuccessor func(context.Context, ownership.Registry) error,
) error {
	if registryStore == nil {
		return fmt.Errorf("ownership registry is unavailable")
	}
	if err := gate.ValidateBefore(ctx); err != nil {
		return fmt.Errorf("%s: %w", validateDetail, err)
	}
	var next ownership.Registry
	if err := gate.ApplyEffect(func() error {
		var convergeErr error
		next, convergeErr = registryStore.Converge(ctx, convergence)
		return convergeErr
	}); err != nil {
		return fmt.Errorf("%s: %w", commitDetail, err)
	}
	if acceptSuccessor != nil {
		if err := acceptSuccessor(ctx, next); err != nil {
			return fmt.Errorf("%s: %w", acceptDetail, err)
		}
	}
	if err := gate.AcceptAfter(ctx); err != nil {
		return fmt.Errorf("%s: %w", acceptDetail, err)
	}
	return nil
}
