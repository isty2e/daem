package nativeoperation

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/applyprogram"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/operationplan"
)

func settleNativeApplyFailure(ctx context.Context, primary error, input NativeApplyInput, options execute.ApplyOptions, authority *operation, store ownershipmutation.RegistryStore, captured journalpublication.CapturedNativeJournal, retiring *journal.NativeJournalRetirement, execution *applyprogram.Execution, result *ApplyResult) (resultErr error) {
	prefix, err := execution.BeginFailureSettlement(applyprogram.SettlementGuardedRecovery)
	if err != nil {
		return errors.Join(primary, err)
	}
	recoveryCtx := context.WithoutCancel(ctx)
	refuse := func(err error) error {
		return errors.Join(primary, fmt.Errorf("automatic native settlement did not complete; preserve recovery evidence and run daem recover --dry-run: %w", err))
	}
	if err := execution.Observe(prefix+"/progress-classification", applyprogram.FailureUnavailable, func() error {
		if err := captured.Validate(); err != nil {
			return err
		}
		var err error
		input.Codecs, input.StateCodec, input.StatefileCodec, err = input.RecoveryCodecs(recoveryCtx)
		if err == nil && (input.StateCodec == nil || input.StatefileCodec.IsZero()) {
			return fmt.Errorf("native settlement requires journal and statefile codecs")
		}
		return err
	}); err != nil {
		return refuse(err)
	}
	if err := execution.Observe(prefix+"/rollback-selection", applyprogram.FailureUnavailable, func() error {
		if !captured.BeforeState().Equal(input.Preparation.BeforeState()) || !captured.AfterState().Equal(input.Preparation.AfterState()) {
			return fmt.Errorf("native settlement differs from its complete containing operation")
		}
		return nil
	}); err != nil {
		return refuse(err)
	}
	var operation *journal.NativeRecoveryOperation
	if err := execution.Observe(prefix+"/journal-load", applyprogram.FailureUnavailable, func() error {
		var err error
		operation, err = journal.OpenCapturedNativeRecovery(recoveryCtx, captured, input.StateCodec, input.StatefileCodec, authority.capturedRoot, input.Budget, journalqualification.NativeRecoveryOptions{
			Filesystem: input.Filesystem, OperationAuthority: authority.recoveryJournal, Registry: store, Codecs: input.Codecs,
			State: func(ctx context.Context) (durable.Snapshot, error) {
				return authority.projectStatefile.Read(ctx, input.StatefileCodec, input.Budget)
			},
		})
		return err
	}); err != nil {
		return refuse(err)
	}
	defer func() { resultErr = errors.Join(resultErr, operation.Close()) }()
	if err := execution.Observe(prefix+"/journal-validation", applyprogram.FailureUnavailable, func() error {
		assessment := operation.InitialAssessment()
		if !assessment.CleanBefore() && !assessment.CleanAfter() && !assessment.RequiresRollback() && !assessment.RequiresFinalization() {
			return fmt.Errorf("native settlement has no admitted observed phase")
		}
		return nil
	}); err != nil {
		return refuse(err)
	}
	var prepared operationplan.EffectStructure
	if err := execution.Observe(prefix+"/execution-compile", applyprogram.FailureUnavailable, func() error {
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
	if err := execution.HandoffGuardedRecovery(); err != nil {
		return refuse(err)
	}
	err = executeNativeRecoveryOperation(recoveryCtx, operation, authority, store, execute.RecoveryOptions{
		Filesystem: input.Filesystem, Resolver: input.Resolver, OwnershipRegistryBinder: input.OwnershipRegistryBinder, StateCodec: input.StateCodec, Codecs: input.Codecs,
		ValidateBeforeEffects: func(ctx context.Context, _ mutation.PhysicalAuthoritySet) error {
			return options.ValidateCompensation(ctx)
		},
		ValidateVisibilityAuthority: options.ValidateCompensation,
		AcceptVisibilityChanges:     options.AcceptCompensationVisibility,
	}, retiring, prepared)
	if err != nil {
		return refuse(err)
	}
	result.State, result.StatePath = operation.ObservedState(), input.Paths.StatefilePath
	if result.State.Equal(input.Preparation.AfterState()) {
		result.ActionCount = input.Preparation.ActionCount()
		return fmt.Errorf("%w; committed native state finalized without replaying host writes", primary)
	}
	result.ActionCount = 0
	return applyprogram.NewRollbackError(primary, nil)
}
