package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/operationplan"
)

func settleNativeApplyFailure(ctx context.Context, primary error, input NativeApplyInput, options ApplyOptions, authority *mutationAuthority, store ownershipmutation.RegistryStore, captured journal.CapturedNativeJournal, retiring *journal.NativeJournalRetirement, execution *applyEffectExecution, result *ApplyResult) (resultErr error) {
	prefix, err := execution.beginFailureSettlement(applyFailureSettlementGuardedRecovery)
	if err != nil {
		return errors.Join(primary, err)
	}
	recoveryCtx := context.WithoutCancel(ctx)
	refuse := func(err error) error {
		return errors.Join(primary, fmt.Errorf("automatic native settlement did not complete; preserve recovery evidence and run daem recover --dry-run: %w", err))
	}
	if err := execution.runObservation(prefix+"/progress-classification", applyForwardFailureUnavailable, func() error {
		if err := captured.Validate(); err != nil {
			return err
		}
		var err error
		input.Codecs, input.StateCodec, err = input.RecoveryCodecs(recoveryCtx)
		return err
	}); err != nil {
		return refuse(err)
	}
	if err := execution.runObservation(prefix+"/rollback-selection", applyForwardFailureUnavailable, func() error {
		if !captured.BeforeState().Equal(input.Preparation.BeforeState()) || !captured.AfterState().Equal(input.Preparation.AfterState()) {
			return fmt.Errorf("native settlement differs from its complete containing operation")
		}
		return nil
	}); err != nil {
		return refuse(err)
	}
	var operation *journal.NativeRecoveryOperation
	if err := execution.runObservation(prefix+"/journal-load", applyForwardFailureUnavailable, func() error {
		var err error
		operation, err = captured.OpenRecovery(recoveryCtx, input.StateCodec, journal.NativeRecoveryOptions{
			Filesystem: input.Filesystem, OperationAuthority: authority.recoveryJournal, Registry: store, Codecs: input.Codecs,
			State: func(ctx context.Context) (durable.Snapshot, error) {
				return authority.readNativeState(ctx, input.StateCodec, input.Budget)
			},
		})
		return err
	}); err != nil {
		return refuse(err)
	}
	defer func() { resultErr = errors.Join(resultErr, operation.Close()) }()
	if err := execution.runObservation(prefix+"/journal-validation", applyForwardFailureUnavailable, func() error {
		assessment := operation.InitialAssessment()
		if !assessment.CleanBefore() && !assessment.CleanAfter() && !assessment.RequiresRollback() && !assessment.RequiresFinalization() {
			return fmt.Errorf("native settlement has no admitted observed phase")
		}
		return nil
	}); err != nil {
		return refuse(err)
	}
	var prepared operationplan.EffectStructure
	if err := execution.runObservation(prefix+"/execution-compile", applyForwardFailureUnavailable, func() error {
		if retiring != nil {
			if err := retiring.BindRecoveryOperation(operation); err != nil {
				return err
			}
		}
		var err error
		prepared, err = compileNativeRecoverySchedule(operation)
		return err
	}); err != nil {
		return refuse(err)
	}
	if err := execution.handoffGuardedRecovery(); err != nil {
		return refuse(err)
	}
	err = executeNativeRecoveryOperation(recoveryCtx, operation, authority, store, RecoveryOptions{
		Filesystem: input.Filesystem, Resolver: input.Resolver, OwnershipRegistryBinder: input.OwnershipRegistryBinder, StateCodec: input.StateCodec, Codecs: input.Codecs,
		ValidateBeforeEffects: func(ctx context.Context, _ mutation.PhysicalAuthoritySet) error {
			return options.validateCompensationAuthority(ctx)
		},
		ValidateVisibilityAuthority: options.validateCompensationAuthority,
		AcceptVisibilityChanges:     options.acceptCompensationVisibilityChanges,
	}, retiring, prepared)
	if err != nil {
		return refuse(err)
	}
	result.State, result.StatePath = operation.ObservedState(), input.Paths.StatefilePath
	if result.State.Equal(input.Preparation.AfterState()) {
		result.ActionCount = aggregateSubjectCount(input.Preparation.AggregateEffects())
		return fmt.Errorf("%w; committed native state finalized without replaying host writes", primary)
	}
	result.ActionCount = 0
	return newApplyRollbackError(primary, nil)
}
