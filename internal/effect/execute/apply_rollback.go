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
	plan, err := journal.LoadActivePlanForStateWithOptions(
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
		return fmt.Errorf("captured recovery journal is blocked by current path or state evidence")
	}
	switch plan.Classification() {
	case recovery.ClassificationCleanBefore, recovery.ClassificationNeedsRollback:
		return nil
	default:
		return fmt.Errorf("captured recovery journal classified as %q before host effects", plan.Classification())
	}
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
	plan, err := journal.LoadActivePlanForStateEntriesWithOptions(
		recoveryCtx,
		paths.journalPaths(),
		currentState,
		rollbackEntries,
		journal.PlanLoadOptions{
			Filesystem:        authority.filesystem,
			RootedCapability:  authority.rootedJournalCapability,
			Resolver:          authority.rootedJournalResolver(authority.lexical),
			OwnershipRegistry: authority.rootedOwnershipRegistryOption(),
			Codecs:            codecs,
			StateCodec:        stateCodec,
		},
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
		mutationAuthority: authority,
		Resolver:          authority.lexical,
		Codecs:            codecs,
		StateCodec:        stateCodec,
		Filesystem:        authority.filesystem,
	}); err != nil {
		events.emit(EventRollbackRestoreFailed, EventStageRollbackRestore, nil, err)
		return fmt.Errorf("%w; guarded rollback failed: %v; recovery journal retained; run: daem recover --dry-run", primary, err)
	}
	events.emit(EventRollbackRestored, EventStageRollbackRestore, nil, nil)
	if err := removeRecoveryJournalWithEvents(
		recoveryCtx,
		authority,
		events,
	); err != nil {
		return fmt.Errorf("%w; host changes rolled back; remove recovery journal failed: %v; run: daem recover --dry-run", primary, err)
	}
	return fmt.Errorf("%w; host changes rolled back", primary)
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
