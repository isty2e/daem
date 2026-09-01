package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
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
	selected []journal.EntrySelection,
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
		paths.journalPaths(),
		currentState,
		journal.PlanLoadOptions{
			Filesystem:        authority.filesystem,
			RootedCapability:  authority.rootedJournalCapability,
			Resolver:          authority.rootedJournalResolver(authority.lexical),
			OwnershipRegistry: authority.rootedOwnershipRegistryOption(),
			Codecs:            codecs,
			StateCodec:        stateCodec,
		},
	)
}

func loadApplyActivePlanForStateEntries(
	ctx context.Context,
	paths Paths,
	currentState durable.Snapshot,
	selected []journal.EntrySelection,
	authority *mutationAuthority,
	stateCodec durable.SnapshotCodec,
	codecs aggregate.CodecCatalog,
) (recovery.Plan, error) {
	if authority == nil {
		return recovery.Plan{}, fmt.Errorf("apply mutation authority is unavailable")
	}
	return journal.LoadActivePlanForStateEntriesWithOptions(
		ctx,
		paths.journalPaths(),
		currentState,
		selected,
		journal.PlanLoadOptions{
			Filesystem:        authority.filesystem,
			RootedCapability:  authority.rootedJournalCapability,
			Resolver:          authority.rootedJournalResolver(authority.lexical),
			OwnershipRegistry: authority.rootedOwnershipRegistryOption(),
			Codecs:            codecs,
			StateCodec:        stateCodec,
		},
	)
}

func applyRecoveryErrorWithEvents(
	ctx context.Context,
	primary error,
	paths Paths,
	currentState durable.Snapshot,
	progress hostActionProgress,
	selections []journal.EntrySelection,
	events applyEventEmitter,
	authority *mutationAuthority,
	stateCodec durable.SnapshotCodec,
	codecs aggregate.CodecCatalog,
	gate visibilityEffectGate,
) error {
	events.emit(EventRollbackRestoreStarted, EventStageRollbackRestore, nil, nil)
	if progress.requiresRecovery() {
		recoveryErr := fmt.Errorf("host effect outcome is indeterminate")
		events.emit(EventRollbackRestoreFailed, EventStageRollbackRestore, nil, recoveryErr)
		return fmt.Errorf("%w; %v; recovery journal retained; run: daem recover --dry-run", primary, recoveryErr)
	}

	recoveryCtx := context.WithoutCancel(ctx)
	rollbackEntries, err := progress.rollbackEntries(selections)
	if err != nil {
		events.emit(EventRollbackRestoreFailed, EventStageRollbackRestore, nil, err)
		return fmt.Errorf("%w; select guarded rollback entries failed: %v; recovery journal retained; run: daem recover --dry-run", primary, err)
	}
	plan, err := loadCapturedJournalPlanForStateEntries(
		recoveryCtx,
		paths,
		currentState,
		rollbackEntries,
		authority,
		stateCodec,
		codecs,
		"before guarded rollback",
	)
	if err != nil {
		events.emit(EventRollbackRestoreFailed, EventStageRollbackRestore, nil, err)
		return fmt.Errorf("%w; load guarded rollback plan failed: %v; recovery journal retained; run: daem recover --dry-run", primary, err)
	}
	if plan.HasErrors() {
		err := fmt.Errorf("guarded rollback is blocked by current evidence")
		events.emit(EventRollbackRestoreFailed, EventStageRollbackRestore, nil, err)
		return fmt.Errorf("%w; %v; recovery journal retained; run: daem recover --dry-run", primary, err)
	}
	execution, err := newActiveRecoveryExecutionForPlan(
		plan,
		activeRecoveryCallerApplySettlement,
		false,
	)
	if err != nil {
		events.emit(EventRollbackRestoreFailed, EventStageRollbackRestore, nil, err)
		return fmt.Errorf("%w; compile guarded rollback settlement failed: %v; recovery journal retained; run: daem recover --dry-run", primary, err)
	}
	if err := executeRecoveryPlanEffects(recoveryCtx, plan, paths, RecoveryOptions{
		reloadPlan: func(
			ctx context.Context,
			loadOptions journal.PlanLoadOptions,
		) (recovery.Plan, error) {
			return journal.LoadActivePlanForStateEntriesWithOptions(
				ctx,
				paths.journalPaths(),
				currentState,
				rollbackEntries,
				loadOptions,
			)
		},
		mutationAuthority:           authority,
		ActiveJournalAuthority:      authority.journalBasis.activeAuthority,
		ValidateVisibilityAuthority: gate.before,
		AcceptVisibilityChanges:     gate.after,
		Resolver:                    authority.lexical,
		Codecs:                      codecs,
		StateCodec:                  stateCodec,
		Filesystem:                  authority.filesystem,
	}, execution); err != nil {
		err = execution.finish(err, nil)
		events.emit(EventRollbackRestoreFailed, EventStageRollbackRestore, nil, err)
		return fmt.Errorf("%w; guarded rollback failed: %v; recovery journal retained; run: daem recover --dry-run", primary, err)
	}
	events.emit(EventRollbackRestored, EventStageRollbackRestore, nil, nil)
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
						paths.journalPaths(),
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
		return newApplyRollbackError(primary, retirementFailureWithRemediation(err))
	}
	if err := execution.finish(nil, nil); err != nil {
		return newApplyRollbackError(primary, retirementFailureWithRemediation(err))
	}
	return newApplyRollbackError(primary, nil)
}

type applyRollbackError struct {
	primary           error
	retirementFailure error
}

func newApplyRollbackError(primary error, retirementFailure error) error {
	return &applyRollbackError{
		primary:           primary,
		retirementFailure: retirementFailure,
	}
}

func (failure *applyRollbackError) Error() string {
	if failure.retirementFailure != nil {
		return fmt.Sprintf(
			"%v; host changes rolled back; retire recovery journal failed: %v",
			failure.primary,
			failure.retirementFailure,
		)
	}
	return fmt.Sprintf("%v; host changes rolled back", failure.primary)
}

func (failure *applyRollbackError) Unwrap() []error {
	if failure.retirementFailure != nil {
		return []error{failure.primary, failure.retirementFailure}
	}
	return []error{failure.primary}
}

// ApplyHostChangesRolledBack reports whether an unsuccessful apply restored
// every host change before returning.
func ApplyHostChangesRolledBack(err error) bool {
	var failure *applyRollbackError
	return errors.As(err, &failure)
}

// hostEffectProgress classifies what apply can truthfully conclude about one
// host action before deciding whether immediate rollback is safe.
type hostEffectProgress uint8

const (
	hostEffectNotStarted hostEffectProgress = iota
	hostEffectExpectedAfter
	hostEffectIndeterminate
)

func (progress hostEffectProgress) rollbackEligible() bool {
	return progress == hostEffectExpectedAfter
}

func (progress hostActionProgress) rollbackEntries(
	selections []journal.EntrySelection,
) ([]journal.EntrySelection, error) {
	if len(progress.states) != len(selections) {
		return nil, fmt.Errorf("host action progress count %d does not match entry selection count %d", len(progress.states), len(selections))
	}
	selected := make([]journal.EntrySelection, 0, len(selections))
	for index, state := range progress.states {
		if state.rollbackEligible() {
			selected = append(selected, selections[index])
		}
	}
	return selected, nil
}

func (progress hostEffectProgress) requiresRecovery() bool {
	return progress == hostEffectIndeterminate
}

type hostActionProgress struct {
	states []hostEffectProgress
}

type indeterminateHostEffectError struct{ cause error }

func (failure *indeterminateHostEffectError) Error() string { return failure.cause.Error() }
func (failure *indeterminateHostEffectError) Unwrap() error { return failure.cause }

func markHostEffectIndeterminate(err error) error {
	if err == nil {
		return nil
	}
	return &indeterminateHostEffectError{cause: err}
}

func newHostActionProgress(actionCount int) hostActionProgress {
	return hostActionProgress{states: make([]hostEffectProgress, actionCount)}
}

func combineHostActionProgress(parts ...hostActionProgress) hostActionProgress {
	count := 0
	for _, part := range parts {
		count += len(part.states)
	}
	combined := hostActionProgress{states: make([]hostEffectProgress, 0, count)}
	for _, part := range parts {
		combined.states = append(combined.states, part.states...)
	}
	return combined
}

func (progress hostActionProgress) record(index int, state hostEffectProgress) {
	progress.states[index] = state
}

func (progress hostActionProgress) requiresRecovery() bool {
	for _, state := range progress.states {
		if state.requiresRecovery() {
			return true
		}
	}
	return false
}

func progressAfterMutationError(err error) hostEffectProgress {
	var indeterminate *indeterminateHostEffectError
	if errors.As(err, &indeterminate) {
		return hostEffectIndeterminate
	}
	var removalFailure *rootedRemovalCommitError
	if errors.As(err, &removalFailure) {
		return progressAfterCommitOutcome(removalFailure.Outcome(), err)
	}
	kind, classified := mutationfs.FailureKindOf(err)
	if !classified {
		return hostEffectIndeterminate
	}
	switch kind {
	case mutationfs.FailureUncommitted, mutationfs.FailureUnsupportedGuarantee:
		return hostEffectNotStarted
	case mutationfs.FailureIndeterminateCommit, mutationfs.FailureRetainedResidue:
		return hostEffectIndeterminate
	default:
		return hostEffectIndeterminate
	}
}

func progressAfterCommitOutcome(outcome mutationfs.CommitOutcome, err error) hostEffectProgress {
	if err == nil {
		return hostEffectExpectedAfter
	}
	switch outcome.State() {
	case mutationfs.CommitOutcomeUncommitted:
		return hostEffectNotStarted
	case mutationfs.CommitOutcomeIndeterminate, mutationfs.CommitOutcomeRetainedRecoverable,
		mutationfs.CommitOutcomeComplete:
		return hostEffectIndeterminate
	default:
		// A rooted removal must always carry a canonical storage outcome. Treat
		// an unknown outcome as indeterminate rather than recursively trying to
		// classify the same wrapped error again.
		return hostEffectIndeterminate
	}
}
