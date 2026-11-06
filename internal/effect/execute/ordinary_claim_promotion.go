package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/claimoperation"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

func ordinaryClaimPromoter(
	authority *mutationAuthority,
	registryStore ownershipmutation.RegistryStore,
	stateCodec durable.SnapshotCodec,
	gate applyprogram.VisibilityGate,
	execution *applyprogram.Execution,
) claimoperation.PromotionStep {
	return func(ctx context.Context, intent ownership.ProvisionalAcquireIntent) (ownershipmutation.ClaimTransition, bool, error) {
		transition, fingerprint, promoted, err := promoteVisibleAcquire(ctx, intent, authority, registryStore, stateCodec, gate, execution)
		if err != nil {
			key := ownership.LogicalKey{Destination: intent.Destination(), Footprint: intent.Footprint()}
			return ownershipmutation.ClaimTransition{}, false, fmt.Errorf("promote ownership for %q footprint %s: %w", intent.Destination(), key.ScopeLabel(), err)
		}
		if promoted && fingerprint == "" {
			return ownershipmutation.ClaimTransition{}, false, fmt.Errorf("promoted recovery journal fingerprint is unavailable")
		}
		return transition, promoted, nil
	}
}

func promoteVisibleAcquire(
	ctx context.Context,
	intent ownership.ProvisionalAcquireIntent,
	authority *mutationAuthority,
	registryStore ownershipmutation.RegistryStore,
	stateCodec durable.SnapshotCodec,
	gate applyprogram.VisibilityGate,
	execution *applyprogram.Execution,
) (ownershipmutation.ClaimTransition, string, bool, error) {
	key := ownership.LogicalKey{Destination: intent.Destination(), Footprint: intent.Footprint()}
	prefix, err := execution.PromotionReference(key)
	if err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	var transition ownershipmutation.ClaimTransition
	var journalFingerprint string
	visible := false
	if err := execution.Observe(prefix+"/observation", applyprogram.FailureGuardedRecovery, func() error {
		if authority == nil || authority.journalBasis.validate() != nil {
			return fmt.Errorf("recovery journal execution basis is unavailable")
		}
		journalFingerprint = authority.journalBasis.recordFingerprint
		if authority.recoveryJournalRecord == nil {
			return fmt.Errorf("recovery journal record authority is unavailable")
		}
		if registryStore == nil {
			return fmt.Errorf("ownership registry is unavailable")
		}
		if stateCodec == nil {
			return fmt.Errorf("state codec is unavailable")
		}
		destination, err := authority.destinations.Resolve(target.ScopeGlobal, intent.Destination())
		if err != nil {
			return err
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(destination.HostPath(), physicalwork.MaximumPhysicalPathDepth, authority.generalTraversalPhase)
		if err != nil {
			return err
		}
		exact, exactVisible := observed.Exact()
		if !exactVisible {
			if _, provisional := observed.Provisional(); provisional {
				return nil
			}
			return fmt.Errorf("visible ownership authority observation is empty")
		}
		visible = true
		address, err := ownership.NewManagedAddress(exact, intent.Footprint())
		if err != nil {
			return err
		}
		transition, err = ownershipmutation.NewAcquireTransitionFromIntent(intent, address)
		if err != nil {
			return err
		}
		registry, err := registryStore.Load(ctx)
		if err != nil {
			return err
		}
		if claim, conflict := registry.Conflict(address); conflict {
			actual, err := ownership.PresentClaim(claim)
			if err != nil {
				return err
			}
			return &ownership.StaleClaimError{Address: address, Expected: ownership.NoClaim(), Actual: actual}
		}
		return nil
	}); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	if err := execution.SelectPromotion(key, visible); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	if !visible {
		return ownershipmutation.ClaimTransition{}, journalFingerprint, false, nil
	}

	journalGate := execution.VisibilityGate(gate, prefix+"/journal", operationplan.EffectStepPersistence, applyprogram.FailureGuardedRecovery)
	if err := journalGate.ValidateBefore(ctx); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	var promotion journalpublication.ProvisionalAcquirePromotion
	if err := journalGate.ApplyEffect(func() error {
		var promoteErr error
		promotion, promoteErr = journalpublication.PromoteProvisionalAcquire(ctx, authority.filesystem,
			authority.recoveryJournalRecord, authority.recoveryJournal, authority.journalBasis.activeAuthority,
			journalFingerprint, intent, transition, stateCodec)
		if refreshed, available := promotion.ActiveJournalAuthority(); available {
			if authority.preparedRetirement != nil {
				if err := authority.preparedRetirement.AdvanceActiveAuthority(authority.journalBasis.activeAuthority, refreshed); err != nil {
					promoteErr = errors.Join(promoteErr, fmt.Errorf("advance prepared journal retirement authority: %w", err))
				}
			}
			if err := authority.setJournalExecutionBasis(promotion.RecordFingerprint(), refreshed); err != nil {
				promoteErr = errors.Join(promoteErr, fmt.Errorf("refresh recovery journal execution basis: %w", err))
			}
		}
		return promoteErr
	}); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	if err := journalGate.AcceptAfter(ctx); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}

	claimGate := execution.VisibilityGate(gate, prefix+"/claim", operationplan.EffectStepPersistence, applyprogram.FailureGuardedRecovery)
	if err := claimGate.ValidateBefore(ctx); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	if err := claimGate.ApplyEffect(func() error {
		return convergeClaim(ctx, registryStore, transition.Address(), transition.Before(), transition.Prepared())
	}); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	if err := claimGate.AcceptAfter(ctx); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}

	return transition, promotion.RecordFingerprint(), true, nil
}

// convergeClaim remains single-address because provisional outputs become
// exactly observable at distinct host-effect boundaries.
func convergeClaim(ctx context.Context, registryStore ownershipmutation.RegistryStore, address ownership.ManagedAddress, from, to ownership.ClaimValue) error {
	change, err := ownership.NewClaimChange(address, from, to)
	if err != nil {
		return err
	}
	convergence, err := ownership.NewClaimConvergence([]ownership.ClaimChange{change})
	if err != nil {
		return err
	}
	_, err = registryStore.Converge(ctx, convergence)
	return err
}
