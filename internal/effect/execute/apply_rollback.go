package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/claimoperation"
	"github.com/isty2e/daem/internal/effect/execute/executionevent"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func validateApplyRecoveryPrepared(
	ctx context.Context,
	paths Paths,
	currentState durable.Snapshot,
	authority *mutationAuthority,
	stateCodec durable.SnapshotCodec,
	codecs aggregate.CodecCatalog,
) error {
	plan, err := loadCapturedJournalPlan(
		ctx,
		paths,
		currentState,
		authority,
		stateCodec,
		codecs,
		"before host effects",
	)
	if err != nil {
		return err
	}
	if plan.HasErrors() {
		for _, action := range plan.Actions() {
			if action.Kind != recovery.ActionKindError {
				continue
			}
			return fmt.Errorf(
				"captured recovery journal is blocked by current path or state evidence: %s at %q: %s",
				action.Reason,
				action.Destination,
				action.Detail,
			)
		}
		for _, obligation := range plan.RemovalCleanupObligations() {
			if obligation.Readiness() != recovery.RemovalCleanupBlocked &&
				obligation.Readiness() != recovery.RemovalCleanupRetry {
				continue
			}
			return fmt.Errorf(
				"captured recovery journal removal cleanup is %s (%s) for %q: %s",
				obligation.Readiness(),
				obligation.Reason(),
				obligation.Destination(),
				obligation.Detail(),
			)
		}
		return fmt.Errorf("captured recovery journal is blocked by current path or state evidence")
	}
	switch plan.Classification() {
	case recovery.ClassificationCleanBefore, recovery.ClassificationNeedsRollback:
		return nil
	default:
		return fmt.Errorf("captured recovery journal classified as %q before host effects", plan.Classification())
	}
}

func validateCapturedJournalFingerprint(
	ctx context.Context,
	paths Paths,
	currentState durable.Snapshot,
	authority *mutationAuthority,
	stateCodec durable.SnapshotCodec,
	codecs aggregate.CodecCatalog,
	phase string,
) error {
	_, err := loadCapturedJournalPlan(
		ctx,
		paths,
		currentState,
		authority,
		stateCodec,
		codecs,
		phase,
	)
	return err
}

func loadCapturedJournalPlan(
	ctx context.Context,
	paths Paths,
	currentState durable.Snapshot,
	authority *mutationAuthority,
	stateCodec durable.SnapshotCodec,
	codecs aggregate.CodecCatalog,
	phase string,
) (recovery.Plan, error) {
	plan, err := loadApplyActivePlanForState(
		ctx,
		paths,
		currentState,
		authority,
		stateCodec,
		codecs,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	if err := authority.validateJournalExecutionBasis(ctx, plan, phase); err != nil {
		return recovery.Plan{}, err
	}
	return plan, nil
}

func loadCapturedJournalPlanForStateEntries(
	ctx context.Context,
	paths Paths,
	currentState durable.Snapshot,
	selected []journalrecord.EntrySelection,
	authority *mutationAuthority,
	stateCodec durable.SnapshotCodec,
	codecs aggregate.CodecCatalog,
	phase string,
) (recovery.Plan, error) {
	plan, err := loadApplyActivePlanForStateEntries(
		ctx,
		paths,
		currentState,
		selected,
		authority,
		stateCodec,
		codecs,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	if err := authority.validateJournalExecutionBasis(ctx, plan, phase); err != nil {
		return recovery.Plan{}, err
	}
	return plan, nil
}

func requireJournalAuthorityFingerprint(
	expected string,
	plan recovery.Plan,
	phase string,
) error {
	if expected == "" {
		return fmt.Errorf("captured recovery journal fingerprint is unavailable")
	}
	current, err := plan.JournalAuthorityFingerprint()
	if err != nil {
		return fmt.Errorf("fingerprint current recovery journal: %w", err)
	}
	if current != expected {
		return fmt.Errorf("captured recovery journal changed %s", phase)
	}
	return nil
}

func loadApplyActivePlanForState(
	ctx context.Context,
	paths Paths,
	currentState durable.Snapshot,
	authority *mutationAuthority,
	stateCodec durable.SnapshotCodec,
	codecs aggregate.CodecCatalog,
) (recovery.Plan, error) {
	if authority == nil {
		return recovery.Plan{}, fmt.Errorf("apply mutation authority is unavailable")
	}
	return journal.LoadActivePlanForStateWithOptions(
		ctx,
		paths.JournalPaths(),
		currentState,
		journal.PlanLoadOptions{
			Filesystem:        authority.filesystem,
			RootedCapability:  authority.destinations.JournalCapability,
			Resolver:          authority.destinations.JournalResolver(authority.lexical),
			OwnershipRegistry: authority.recoveryOwnershipRegistryOption(paths.OwnershipRegistryPath),
			Codecs:            codecs,
			StateCodec:        stateCodec,
		},
	)
}

func loadApplyActivePlanForStateEntries(
	ctx context.Context,
	paths Paths,
	currentState durable.Snapshot,
	selected []journalrecord.EntrySelection,
	authority *mutationAuthority,
	stateCodec durable.SnapshotCodec,
	codecs aggregate.CodecCatalog,
) (recovery.Plan, error) {
	if authority == nil {
		return recovery.Plan{}, fmt.Errorf("apply mutation authority is unavailable")
	}
	return journal.LoadActivePlanForStateEntriesWithOptions(
		ctx,
		paths.JournalPaths(),
		currentState,
		selected,
		journal.PlanLoadOptions{
			Filesystem:        authority.filesystem,
			RootedCapability:  authority.destinations.JournalCapability,
			Resolver:          authority.destinations.JournalResolver(authority.lexical),
			OwnershipRegistry: authority.recoveryOwnershipRegistryOption(paths.OwnershipRegistryPath),
			Codecs:            codecs,
			StateCodec:        stateCodec,
		},
	)
}

func settlePreHostApplyFailure(
	ctx context.Context,
	primary error,
	kind applyprogram.FailureSettlement,
	retirementPhase string,
	paths Paths,
	currentState durable.Snapshot,
	authority *mutationAuthority,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
	events executionevent.Emitter,
	stateCodec durable.SnapshotCodec,
	codecs aggregate.CodecCatalog,
	gate applyprogram.VisibilityGate,
	effectExecution *applyprogram.Execution,
) error {
	settlementPrefix, settlementErr := effectExecution.BeginFailureSettlement(kind)
	if settlementErr != nil {
		return errors.Join(primary, settlementErr)
	}
	if rollbackErr := claimoperation.RollbackToBefore(
		context.WithoutCancel(ctx),
		registryStore,
		transitions,
		effectExecution.CompensationVisibilityGate(
			gate,
			settlementPrefix+"/ownership-rollback",
			operationplan.EffectStepCompensation,
		),
	); rollbackErr != nil {
		return fmt.Errorf(
			"%w; ownership rollback failed: %w; recovery journal retained; run: daem recover --dry-run",
			primary,
			applyprogram.RollbackFailure{Cause: rollbackErr},
		)
	}
	loadRetirementPlan := func(ctx context.Context) (recovery.Plan, error) {
		return loadCapturedJournalPlanForStateEntries(
			ctx,
			paths,
			currentState,
			[]journalrecord.EntrySelection{},
			authority,
			stateCodec,
			codecs,
			retirementPhase,
		)
	}
	if cleanupErr := retireRecoveryJournalWithEvents(
		context.WithoutCancel(ctx),
		paths,
		authority,
		events,
		loadRetirementPlan,
		stateCodec,
		effectExecution.CompensationVisibilityGate(
			gate,
			settlementPrefix+"/journal-retirement",
			operationplan.EffectStepRetirement,
		),
		nil,
	); cleanupErr != nil {
		return fmt.Errorf(
			"%w; retire recovery journal failed: %v",
			primary,
			retirementFailureWithRemediation(cleanupErr),
		)
	}
	if settlementErr := effectExecution.CompleteFailureSettlement(kind); settlementErr != nil {
		return errors.Join(primary, settlementErr)
	}
	return primary
}

func applyRecoveryErrorWithEvents(
	ctx context.Context,
	primary error,
	paths Paths,
	currentState durable.Snapshot,
	progress hostmutation.ActionProgress,
	selections []journalrecord.EntrySelection,
	events executionevent.Emitter,
	authority *mutationAuthority,
	stateCodec durable.SnapshotCodec,
	codecs aggregate.CodecCatalog,
	gate applyprogram.VisibilityGate,
	effectExecution *applyprogram.Execution,
) error {
	settlementPrefix, settlementErr := effectExecution.BeginFailureSettlement(
		applyprogram.SettlementGuardedRecovery,
	)
	if settlementErr != nil {
		return errors.Join(primary, settlementErr)
	}
	events.Emit(executionevent.RollbackRestoreStarted, executionevent.StageRollbackRestore, nil, nil)
	failRollback := func(stage string, cause error) error {
		events.Emit(executionevent.RollbackRestoreFailed, executionevent.StageRollbackRestore, nil, cause)
		if stage != "" {
			stage += ": "
		}
		return fmt.Errorf("%w; %s%w; recovery journal retained; run: daem recover --dry-run", primary, stage, applyprogram.RollbackFailure{Cause: cause})
	}

	if err := effectExecution.Observe(
		settlementPrefix+"/progress-classification",
		applyprogram.FailureUnavailable,
		func() error {
			if progress.RequiresRecovery() {
				return fmt.Errorf("host effect outcome is indeterminate")
			}
			return nil
		},
	); err != nil {
		return failRollback("", err)
	}

	recoveryCtx := context.WithoutCancel(ctx)
	var rollbackEntries []journalrecord.EntrySelection
	err := effectExecution.Observe(
		settlementPrefix+"/rollback-selection",
		applyprogram.FailureUnavailable,
		func() error {
			var selectErr error
			rollbackEntries, selectErr = progress.RollbackEntries(selections)
			return selectErr
		},
	)
	if err != nil {
		return failRollback("select guarded rollback entries failed", err)
	}
	var plan recovery.Plan
	err = effectExecution.Observe(
		settlementPrefix+"/journal-load",
		applyprogram.FailureUnavailable,
		func() error {
			var loadErr error
			plan, loadErr = loadCapturedJournalPlanForStateEntries(
				recoveryCtx,
				paths,
				currentState,
				rollbackEntries,
				authority,
				stateCodec,
				codecs,
				"before guarded rollback",
			)
			return loadErr
		},
	)
	if err != nil {
		return failRollback("load guarded rollback plan failed", err)
	}
	if err := effectExecution.Observe(
		settlementPrefix+"/journal-validation",
		applyprogram.FailureUnavailable,
		func() error {
			if plan.HasErrors() {
				return fmt.Errorf("guarded rollback is blocked by current evidence")
			}
			return nil
		},
	); err != nil {
		return failRollback("", err)
	}
	var execution *activeRecoveryExecution
	err = effectExecution.Observe(
		settlementPrefix+"/execution-compile",
		applyprogram.FailureUnavailable,
		func() error {
			var compileErr error
			execution, compileErr = newActiveRecoveryExecutionForPlan(
				plan,
				activeRecoveryCallerApplySettlement,
				false,
			)
			return compileErr
		},
	)
	if err != nil {
		return failRollback("compile guarded rollback settlement failed", err)
	}
	if err := effectExecution.HandoffGuardedRecovery(); err != nil {
		return failRollback("handoff guarded rollback settlement failed", err)
	}
	if err := executeRecoveryPlanEffects(recoveryCtx, plan, paths, RecoveryOptions{
		reloadPlan: func(
			ctx context.Context,
			loadOptions journal.PlanLoadOptions,
		) (recovery.Plan, error) {
			return journal.LoadActivePlanForStateEntriesWithOptions(
				ctx,
				paths.JournalPaths(),
				currentState,
				rollbackEntries,
				loadOptions,
			)
		},
		mutationAuthority:           authority,
		ActiveJournalAuthority:      authority.journalBasis.ActiveAuthority(),
		ValidateVisibilityAuthority: gate.ValidateBefore,
		AcceptVisibilityChanges:     gate.AcceptAfter,
		Resolver:                    authority.lexical,
		Codecs:                      codecs,
		StateCodec:                  stateCodec,
		Filesystem:                  authority.filesystem,
	}, execution); err != nil {
		return failRollback("guarded rollback failed", execution.finish(err, nil))
	}
	events.Emit(executionevent.RollbackRestored, executionevent.StageRollbackRestore, nil, nil)
	loadRetirementPlan := func(ctx context.Context) (recovery.Plan, error) {
		return reloadRecoveryPlanAfterEffects(
			ctx,
			plan,
			RecoveryOptions{
				Resolver:   authority.lexical,
				Codecs:     codecs,
				StateCodec: stateCodec,
				Filesystem: authority.filesystem,
				reloadPlan: func(
					ctx context.Context,
					loadOptions journal.PlanLoadOptions,
				) (recovery.Plan, error) {
					return journal.LoadActivePlanForStateEntriesWithOptions(
						ctx,
						paths.JournalPaths(),
						currentState,
						rollbackEntries,
						loadOptions,
					)
				},
			},
			authority,
		)
	}
	if err := retireRecoveryJournalWithEvents(
		recoveryCtx,
		paths,
		authority,
		events,
		loadRetirementPlan,
		stateCodec,
		gate,
		execution,
	); err != nil {
		err = execution.finish(err, nil)
		return applyprogram.NewRollbackError(primary, retirementFailureWithRemediation(err))
	}
	if err := execution.finish(nil, nil); err != nil {
		return applyprogram.NewRollbackError(primary, retirementFailureWithRemediation(err))
	}
	return applyprogram.NewRollbackError(primary, nil)
}
