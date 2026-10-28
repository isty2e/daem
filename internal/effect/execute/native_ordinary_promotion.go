package execute

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

func promoteNativeOrdinaryAcquires(ctx context.Context, state *ownershipMutationState, keys []ownershipOutputKey, captured *journal.CapturedNativeJournal, authority *mutationAuthority, input NativeApplyInput, store ownershipmutation.RegistryStore, gate visibilityEffectGate, execution *applyEffectExecution) error {
	requested := make(map[ownershipOutputKey]struct{}, len(keys))
	for _, key := range keys {
		requested[key] = struct{}{}
	}
	for index := 0; index < len(state.provisional); {
		intent := state.provisional[index]
		key := ownershipOutputKey{destination: intent.Destination(), footprint: intent.Footprint()}
		if _, needed := requested[key]; !needed {
			index++
			continue
		}
		prefix, err := execution.promotionReference(key)
		if err != nil {
			return err
		}
		var transition ownershipmutation.ClaimTransition
		if err := execution.runObservation(prefix+"/observation", applyForwardFailureGuardedRecovery, func() error {
			destination, err := authority.resolveBoundDestination(target.ScopeGlobal, intent.Destination())
			if err != nil {
				return err
			}
			actual, err := mutation.ObserveDirectoryEntryAuthorityBounded(destination.hostPath, recovery.MaximumPhysicalPathDepth, input.Budget)
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
			return err
		}
		if err := execution.selectPromotion(key, true); err != nil {
			return err
		}
		journalGate := execution.visibilityGate(gate, prefix+"/journal", operationplan.EffectStepPersistence, applyForwardFailureGuardedRecovery)
		if err := journalGate.validateBefore(ctx); err != nil {
			return err
		}
		if err := journalGate.applyEffect(func() error {
			promoted, err := captured.PromoteOrdinary(ctx, intent, transition, journal.NativePromotionOptions{
				NativeRecoveryOptions: journal.NativeRecoveryOptions{
					Filesystem: input.Filesystem, OperationAuthority: authority.recoveryJournal, Registry: store, Codecs: input.Codecs,
					State: func(ctx context.Context) (durable.Snapshot, error) {
						return readNativeCommittedState(ctx, authority, input)
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
			return err
		}
		if err := journalGate.acceptAfter(ctx); err != nil {
			return err
		}
		claimGate := execution.visibilityGate(gate, prefix+"/claim", operationplan.EffectStepPersistence, applyForwardFailureGuardedRecovery)
		if err := claimGate.validateBefore(ctx); err != nil {
			return err
		}
		if err := claimGate.applyEffect(func() error {
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
			return err
		}
		if err := claimGate.acceptAfter(ctx); err != nil {
			return err
		}
		state.transitions = append(state.transitions, transition)
		state.provisional = append(state.provisional[:index], state.provisional[index+1:]...)
	}
	return nil
}
