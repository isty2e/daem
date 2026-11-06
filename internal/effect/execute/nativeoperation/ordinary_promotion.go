package nativeoperation

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/claimoperation"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/effect/mutation"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

func promoteNativeOrdinaryAcquires(ctx context.Context, state *claimoperation.State, keys []ownership.LogicalKey, captured *journalpublication.CapturedNativeJournal, authority *operation, input NativeApplyInput, store ownershipmutation.RegistryStore, gate applyprogram.VisibilityGate, execution *applyprogram.Execution) error {
	return state.Promote(ctx, keys, func(ctx context.Context, intent ownership.ProvisionalAcquireIntent) (ownershipmutation.ClaimTransition, bool, error) {
		key := ownership.LogicalKey{Destination: intent.Destination(), Footprint: intent.Footprint()}
		prefix, err := execution.PromotionReference(key)
		if err != nil {
			return ownershipmutation.ClaimTransition{}, false, err
		}
		var transition ownershipmutation.ClaimTransition
		if err := execution.Observe(prefix+"/observation", applyprogram.FailureGuardedRecovery, func() error {
			destination, err := authority.destinations.Resolve(target.ScopeGlobal, intent.Destination())
			if err != nil {
				return err
			}
			actual, err := mutation.ObserveDirectoryEntryAuthorityBounded(destination.HostPath(), physicalwork.MaximumPhysicalPathDepth, input.Budget)
			if err != nil {
				return err
			}
			exact, visible := actual.Exact()
			if !visible {
				return fmt.Errorf("published ordinary output has no exact ownership namespace")
			}
			address, err := ownership.NewManagedAddress(exact, intent.Footprint())
			if err != nil {
				return err
			}
			transition, err = ownershipmutation.NewAcquireTransitionFromIntent(intent, address)
			return err
		}); err != nil {
			return ownershipmutation.ClaimTransition{}, false, err
		}
		if err := execution.SelectPromotion(key, true); err != nil {
			return ownershipmutation.ClaimTransition{}, false, err
		}
		journalGate := execution.VisibilityGate(gate, prefix+"/journal", operationplan.EffectStepPersistence, applyprogram.FailureGuardedRecovery)
		if err := journalGate.ValidateBefore(ctx); err != nil {
			return ownershipmutation.ClaimTransition{}, false, err
		}
		if err := journalGate.ApplyEffect(func() error {
			promoted, err := captured.PromoteOrdinary(ctx, intent, transition, journalpublication.NativePromotionOptions{
				NativeRecoveryOptions: journalqualification.NativeRecoveryOptions{
					Filesystem: input.Filesystem, OperationAuthority: authority.recoveryJournal, Registry: store, Codecs: input.Codecs,
					State: func(ctx context.Context) (durable.Snapshot, error) {
						return authority.projectStatefile.Read(ctx, input.StateCodec, input.Budget)
					},
				},
				RecordAuthority: authority.recoveryJournalRecord, StateCodec: input.StateCodec,
			})
			if err != nil {
				return err
			}
			*captured = promoted
			return authority.setJournalExecutionBasis(promoted.CaptureResult().RecordFingerprint, promoted.ActiveJournalAuthority())
		}); err != nil {
			return ownershipmutation.ClaimTransition{}, false, err
		}
		if err := journalGate.AcceptAfter(ctx); err != nil {
			return ownershipmutation.ClaimTransition{}, false, err
		}
		claimGate := execution.VisibilityGate(gate, prefix+"/claim", operationplan.EffectStepPersistence, applyprogram.FailureGuardedRecovery)
		if err := claimGate.ValidateBefore(ctx); err != nil {
			return ownershipmutation.ClaimTransition{}, false, err
		}
		if err := claimGate.ApplyEffect(func() error {
			claims, err := captured.PreparationClaims(ctx)
			if err != nil {
				return err
			}
			bindings, err := ownership.NewDocumentBindingConvergence(nil)
			if err != nil {
				return err
			}
			_, err = store.ConvergeRegistry(ctx, claims, bindings)
			return err
		}); err != nil {
			return ownershipmutation.ClaimTransition{}, false, err
		}
		if err := claimGate.AcceptAfter(ctx); err != nil {
			return ownershipmutation.ClaimTransition{}, false, err
		}
		return transition, true, nil
	})
}
