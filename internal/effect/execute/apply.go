package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization/aggregate"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
)

// Paths identifies host mutation and statefile locations used by execution.
type Paths struct {
	RecoveryDir           string
	StateDir              string
	StatefilePath         string
	ManifestRoot          string
	DataDir               string
	OwnershipRegistryPath string
}

func (paths Paths) journalPaths() journal.Paths {
	return journal.Paths{
		RecoveryDir:   paths.RecoveryDir,
		StatefilePath: paths.StatefilePath,
		ManifestRoot:  paths.ManifestRoot,
		DataDir:       paths.DataDir,
	}
}

// ApplyInput contains typed executable effects and prepared payloads for one apply mutation.
type ApplyInput struct {
	Paths                       Paths
	Resolver                    DestinationResolver
	ManagedPathEffects          []ManagedPathEffect
	AggregateEffects            []AggregateEffect
	ManagedPathEvidence         []observe.ManagedPathEvidence
	CurrentState                durable.Snapshot
	GlobalCarrierClaims         durablecarrier.GlobalCarrierClaims
	RetiredProjectCarrierClaims []durablecarrier.ManagedCarrierClaim
	AdoptedProjectCarrierClaims []durablecarrier.ManagedCarrierClaim
	Payloads                    payload.PayloadSet
	ConfirmedRelationActions    []reconciliation.RelationAction
	Owner                       stateauthority.Authority
	Ownership                   []observe.OwnershipObservation
	ProjectRoot                 *rootedpath.CapturedRoot
	Codecs                      aggregate.CodecCatalog
	OwnershipRegistryBinder     ownershipmutation.RootedRegistryBinder
	StateCodec                  durable.SnapshotCodec
	Filesystem                  mutationfs.Store
}

// ApplyOptions configures the final apply boundary and observational execution events.
type ApplyOptions struct {
	Events EventSink
	// ValidateBeforeEffects runs after all rooted effect authority is bound and
	// immediately before each forward visibility-changing effect.
	ValidateBeforeEffects func(context.Context, mutation.PhysicalAuthoritySet) error
	// AcceptVisibilityChanges re-observes and accepts path identity transitions
	// after one successful visibility-changing effect and its postcondition.
	AcceptVisibilityChanges func(context.Context) error
	// ValidateCompensationAuthority verifies that held namespace authority still
	// permits rollback after a forward visibility change was rejected.
	ValidateCompensationAuthority func(context.Context) error
	// AcceptCompensationVisibilityChanges accepts path identity transitions after
	// one successful compensating effect and its postcondition.
	AcceptCompensationVisibilityChanges func(context.Context) error
	commitStatefile                     statefileCommitter
}

// ApplyResult reports the committed mutation count and statefile path.
type ApplyResult struct {
	ActionCount int
	StatePath   string
	State       durable.Snapshot
	// ExecutionAttempted reports whether apply reached journal capture or a
	// later mutation boundary.
	ExecutionAttempted bool
}

// ApplyWithOptions commits typed executable effects and emits observational execution events.
func ApplyWithOptions(
	ctx context.Context,
	input ApplyInput,
	options ApplyOptions,
) (result ApplyResult, resultErr error) {
	executionAttempted := false
	defer func() {
		result.ExecutionAttempted = executionAttempted
	}()
	for index, effect := range input.ManagedPathEffects {
		if err := effect.validate(); err != nil {
			return ApplyResult{}, fmt.Errorf("managed path effect[%d]: %w", index, err)
		}
	}
	for index, effect := range input.AggregateEffects {
		if err := effect.validate(); err != nil {
			return ApplyResult{}, fmt.Errorf("aggregate effect[%d]: %w", index, err)
		}
	}
	if ctx == nil {
		return ApplyResult{}, fmt.Errorf("apply context is required")
	}
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	if input.Resolver == nil {
		return ApplyResult{}, fmt.Errorf("apply destination resolver is required")
	}
	resolver := input.Resolver
	transition, err := deriveApplyStateTransition(input)
	if err != nil {
		return ApplyResult{}, err
	}
	nextState := transition.nextState

	createdAt := time.Now().UTC()
	operationID := journal.OperationID(createdAt)
	events := applyEventEmitter{
		sink: options.Events,
		totalActions: len(input.ManagedPathEffects) +
			aggregateSubjectCount(input.AggregateEffects) +
			transition.retiredProjectClaimCount +
			transition.adoptedProjectClaimCount,
	}
	if !transition.changed {
		return ApplyResult{StatePath: input.Paths.StatefilePath, State: input.CurrentState}, nil
	}
	if input.StateCodec == nil {
		return ApplyResult{}, fmt.Errorf("apply state codec is required")
	}
	if input.Filesystem == nil {
		return ApplyResult{}, fmt.Errorf("apply filesystem is required")
	}
	stateContent, err := input.StateCodec.Encode(nextState)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("marshal statefile: %w", err)
	}
	managedMutations, err := managedPathJournalMutations(input.ManagedPathEffects)
	if err != nil {
		return ApplyResult{}, err
	}
	managedSchedule, err := newManagedPathExecutionSchedule(input.ManagedPathEffects)
	if err != nil {
		return ApplyResult{}, err
	}
	aggregateMutations, err := aggregateJournalMutations(input.AggregateEffects)
	if err != nil {
		return ApplyResult{}, err
	}
	removalDemands, err := removalDemandSetForExecution(
		input.ManagedPathEffects,
		input.AggregateEffects,
		input.ManagedPathEvidence,
	)
	if err != nil {
		return ApplyResult{}, err
	}
	entrySelections, err := journal.EntrySelections(managedMutations, aggregateMutations)
	if err != nil {
		return ApplyResult{}, err
	}
	managedOwnership, err := ownershipPlanForManagedPathEffects(
		input.ManagedPathEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("derive managed path ownership claim transitions: %w", err)
	}
	aggregateOwnership, err := ownershipPlanForAggregateEffects(
		input.AggregateEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("derive managed aggregate ownership claim transitions: %w", err)
	}
	ownershipState := newOwnershipMutationState(managedOwnership, aggregateOwnership)
	if ownershipState.hasMutations() && input.OwnershipRegistryBinder == nil {
		return ApplyResult{}, fmt.Errorf("apply ownership registry binder is required")
	}
	mutationAuthority, err := newMutationAuthorityWithProjectionEffects(
		input.Paths,
		input.ManagedPathEffects,
		input.AggregateEffects,
		removalDemands,
		input.ProjectRoot,
		input.Resolver,
		input.Filesystem,
		input.OwnershipRegistryBinder,
	)
	if err != nil {
		return ApplyResult{}, err
	}
	defer mutationAuthority.close()
	if err := mutationAuthority.bindProjectStatefile(
		input.Paths.ManifestRoot,
		input.Paths.StatefilePath,
	); err != nil {
		return ApplyResult{}, err
	}
	journalResolver := mutationAuthority.rootedJournalResolver(resolver)
	var registryStore ownershipmutation.RegistryStore
	if ownershipState.hasMutations() {
		if err := mutationAuthority.bindOwnershipRegistry(input.Paths.OwnershipRegistryPath); err != nil {
			return ApplyResult{}, err
		}
		registryStore, err = mutationAuthority.rootedOwnershipRegistry()
		if err != nil {
			return ApplyResult{}, err
		}
	}
	visibilityGate := visibilityEffectGate{
		before: func(ctx context.Context) error {
			physicalAuthority, err := mutationAuthority.physicalAuthority()
			if err != nil {
				return err
			}
			return options.validateBeforeEffects(ctx, physicalAuthority)
		},
		after: options.acceptVisibilityChanges,
	}
	compensationGate := visibilityEffectGate{
		before: options.validateCompensationAuthority,
		after:  options.acceptCompensationVisibilityChanges,
	}
	if err := mutationAuthority.prepareApplyForwardRemovals(
		ctx,
		input.ManagedPathEffects,
		input.AggregateEffects,
		input.Payloads,
	); err != nil {
		return ApplyResult{}, fmt.Errorf("prepare bounded forward removals: %w", err)
	}
	if err := visibilityGate.validateBefore(ctx); err != nil {
		return ApplyResult{}, err
	}
	if err := mutationAuthority.bindRecoveryJournal(
		input.Paths.ManifestRoot,
		filepath.Join(input.Paths.RecoveryDir, operationID),
	); err != nil {
		return ApplyResult{}, err
	}
	events.emit(EventJournalCaptureStarted, EventStageJournalCapture, nil, nil)
	executionAttempted = true
	captureResult, err := journal.CaptureJournalWithOptions(
		ctx,
		input.Paths.journalPaths(),
		operationID,
		createdAt,
		input.CurrentState,
		nextState,
		journal.CaptureOptions{
			Filesystem:                input.Filesystem,
			ClaimTransitions:          ownershipState.transitions,
			ProvisionalAcquires:       ownershipState.provisional,
			ManagedPathMutations:      managedMutations,
			ManagedAggregateMutations: aggregateMutations,
			ManagedPathEvidence:       input.ManagedPathEvidence,
			RemovalDemands:            removalDemands,
			Resolver:                  journalResolver,
			ManifestRoot:              mutationAuthority.capturedRoot,
			OperationAuthority:        mutationAuthority.recoveryJournal,
			RootedCapability:          mutationAuthority.rootedJournalCapability,
			Codecs:                    input.Codecs,
			StateCodec:                input.StateCodec,
		},
	)
	if err != nil {
		events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, err)
		return ApplyResult{}, fmt.Errorf("capture recovery journal: %w", err)
	}
	if err := mutationAuthority.captureJournalExecutionBasis(
		ctx,
		captureResult.RecordFingerprint,
	); err != nil {
		events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, err)
		return ApplyResult{}, err
	}
	activePlan, err := loadApplyActivePlanForState(
		ctx,
		input.Paths,
		input.CurrentState,
		mutationAuthority,
		input.StateCodec,
		input.Codecs,
	)
	if err != nil {
		events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, err)
		return ApplyResult{}, fmt.Errorf("bind captured removal authority: %w", err)
	}
	if err := mutationAuthority.validateJournalExecutionBasis(
		ctx,
		activePlan,
		"after publication",
	); err != nil {
		events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, err)
		return ApplyResult{}, err
	}
	if err := mutationAuthority.bindRemovalIntents(activePlan); err != nil {
		events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, err)
		return ApplyResult{}, fmt.Errorf("bind captured removal authority: %w", err)
	}
	if err := mutationAuthority.prepareActiveJournalRetirement(
		ctx,
		input.Paths,
		activePlan,
		input.StateCodec,
	); err != nil {
		events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, err)
		return ApplyResult{}, fmt.Errorf("prepare bounded journal retirement: %w", err)
	}
	if err := visibilityGate.acceptAfter(ctx); err != nil {
		events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, err)
		return ApplyResult{}, fmt.Errorf(
			"accept recovery journal visibility: %w; recovery journal retained; run: daem recover --dry-run",
			err,
		)
	}
	if err := validateCapturedJournalFingerprint(
		ctx,
		input.Paths,
		input.CurrentState,
		mutationAuthority,
		input.StateCodec,
		input.Codecs,
		"before prepared effects",
	); err != nil {
		events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, err)
		return ApplyResult{}, err
	}
	events.emit(EventJournalCaptured, EventStageJournalCapture, nil, nil)
	if err := prepareClaimTransitions(
		ctx,
		registryStore,
		ownershipState.transitions,
		visibilityGate,
	); err != nil {
		rollbackErr := rollbackClaimsToBefore(
			context.WithoutCancel(ctx),
			registryStore,
			ownershipState.transitions,
			compensationGate,
		)
		if rollbackErr != nil {
			return ApplyResult{}, fmt.Errorf("%w; ownership rollback failed: %v; recovery journal retained; run: daem recover --dry-run", err, rollbackErr)
		}
		loadRetirementPlan := func(ctx context.Context) (recovery.Plan, error) {
			return loadCapturedJournalPlanForStateEntries(
				ctx,
				input.Paths,
				input.CurrentState,
				[]journal.EntrySelection{},
				mutationAuthority,
				input.StateCodec,
				input.Codecs,
				"before claim-preparation rollback retirement",
			)
		}
		cleanupErr := retireRecoveryJournalWithEvents(
			context.WithoutCancel(ctx),
			input.Paths,
			mutationAuthority,
			events,
			loadRetirementPlan,
			input.StateCodec,
			compensationGate,
			nil,
		)
		if cleanupErr != nil {
			return ApplyResult{}, fmt.Errorf(
				"%w; retire recovery journal failed: %v",
				err,
				retirementFailureWithRemediation(cleanupErr),
			)
		}
		return ApplyResult{}, err
	}

	events.emit(EventRollbackStageStarted, EventStageRollbackStage, nil, nil)
	if err := validateApplyRecoveryPrepared(
		ctx,
		input.Paths,
		input.CurrentState,
		mutationAuthority,
		input.StateCodec,
		input.Codecs,
	); err != nil {
		events.emit(EventRollbackStageFailed, EventStageRollbackStage, nil, err)
		if claimErr := rollbackClaimsToBefore(
			context.WithoutCancel(ctx),
			registryStore,
			ownershipState.transitions,
			compensationGate,
		); claimErr != nil {
			return ApplyResult{}, fmt.Errorf("%w; ownership rollback failed: %v; recovery journal retained; run: daem recover --dry-run", err, claimErr)
		}
		loadRetirementPlan := func(ctx context.Context) (recovery.Plan, error) {
			return loadCapturedJournalPlanForStateEntries(
				ctx,
				input.Paths,
				input.CurrentState,
				[]journal.EntrySelection{},
				mutationAuthority,
				input.StateCodec,
				input.Codecs,
				"before prepared-effect rollback retirement",
			)
		}
		cleanupErr := retireRecoveryJournalWithEvents(
			context.WithoutCancel(ctx),
			input.Paths,
			mutationAuthority,
			events,
			loadRetirementPlan,
			input.StateCodec,
			compensationGate,
			nil,
		)
		if cleanupErr != nil {
			return ApplyResult{}, fmt.Errorf(
				"%w; retire recovery journal failed: %v",
				err,
				retirementFailureWithRemediation(cleanupErr),
			)
		}
		return ApplyResult{}, err
	}
	events.emit(EventRollbackStaged, EventStageRollbackStage, nil, nil)

	managedProgress := newHostActionProgress(managedSchedule.mutationCount)
	promoteManagedOwnership := func(
		ctx context.Context,
		effect ManagedPathEffect,
		phase managedPathPhase,
	) error {
		return ownershipState.promoteVisibleAcquires(
			ctx,
			provisionalAcquireKeysForManagedPath(effect, phase),
			mutationAuthority,
			registryStore,
			input.StateCodec,
			visibilityGate,
		)
	}
	if err := applyManagedPathPhaseWithEvents(
		ctx, managedSchedule, managedPathPublishPhase, input.Payloads, mutationAuthority,
		managedProgress, 0, events, visibilityGate, promoteManagedOwnership,
	); err != nil {
		progress := combineHostActionProgress(
			managedProgress, newHostActionProgress(len(aggregateMutations)),
		)
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
		)
	}

	aggregateProgress, err := applyAggregateEffectsWithEvents(
		ctx,
		input.AggregateEffects,
		mutationAuthority,
		input.Codecs,
		len(input.ManagedPathEffects),
		events,
		visibilityGate,
		func(ctx context.Context, effect AggregateEffect) error {
			return ownershipState.promoteVisibleAcquires(
				ctx,
				provisionalAcquireKeysForAggregate(effect),
				mutationAuthority,
				registryStore,
				input.StateCodec,
				visibilityGate,
			)
		},
	)
	progress := combineHostActionProgress(managedProgress, aggregateProgress)
	if err != nil {
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
		)
	}
	if err := applyManagedPathPhaseWithEvents(
		ctx, managedSchedule, managedPathRetirePhase, input.Payloads, mutationAuthority,
		managedProgress, 0, events, visibilityGate, promoteManagedOwnership,
	); err != nil {
		progress = combineHostActionProgress(managedProgress, aggregateProgress)
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
		)
	}
	progress = combineHostActionProgress(managedProgress, aggregateProgress)
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
		)
	}
	if err := mutationAuthority.validateProjectSelection(input.Paths.ManifestRoot); err != nil {
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
		)
	}
	if err := visibilityGate.validateBefore(ctx); err != nil {
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
		)
	}
	events.emit(EventStatefileWriteStarted, EventStageStatefileWrite, nil, nil)
	commit := options.statefileCommit(
		ctx,
		mutationAuthority,
		input.Paths.StatefilePath,
		stateContent,
		0o600,
	)
	if !commit.committed() {
		primary := fmt.Errorf("write statefile: %w", commit.failure())
		events.emit(EventStatefileWriteFailed, EventStageStatefileWrite, nil, primary)
		if commit.requiresRecovery() {
			return ApplyResult{}, fmt.Errorf("%w; statefile commit is indeterminate; recovery journal retained; run: daem recover --dry-run", primary)
		}
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, primary, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
		)
	}
	committedResult := ApplyResult{
		ActionCount: len(input.ManagedPathEffects) +
			aggregateSubjectCount(input.AggregateEffects) +
			transition.retiredProjectClaimCount +
			transition.adoptedProjectClaimCount,
		StatePath: input.Paths.StatefilePath,
		State:     nextState,
	}
	if err := visibilityGate.acceptAfter(ctx); err != nil {
		return committedResult, fmt.Errorf(
			"%w; apply effects and statefile committed; recovery journal retained; run: daem recover --dry-run",
			err,
		)
	}
	events.emit(EventStatefileWritten, EventStageStatefileWrite, nil, nil)
	if err := mutationAuthority.validateProjectSelection(input.Paths.ManifestRoot); err != nil {
		return committedResult, fmt.Errorf(
			"%w; apply effects and statefile committed; recovery journal retained; run: daem recover --dry-run",
			err,
		)
	}
	if err := ctx.Err(); err != nil {
		return committedResult, fmt.Errorf("%w; apply effects and statefile committed; recovery journal retained; run: daem recover --dry-run", err)
	}
	if err := finalizeClaimTransitions(ctx, registryStore, ownershipState.transitions, visibilityGate); err != nil {
		return committedResult, fmt.Errorf("%w; apply effects and statefile committed; recovery journal retained; run: daem recover --dry-run", err)
	}
	loadRetirementPlan := func(ctx context.Context) (recovery.Plan, error) {
		return loadCapturedJournalPlan(
			ctx,
			input.Paths,
			nextState,
			mutationAuthority,
			input.StateCodec,
			input.Codecs,
			"before successful apply retirement",
		)
	}
	if err := retireRecoveryJournalWithEvents(
		ctx,
		input.Paths,
		mutationAuthority,
		events,
		loadRetirementPlan,
		input.StateCodec,
		visibilityGate,
		nil,
	); err != nil {
		return committedResult, fmt.Errorf(
			"retire recovery journal: %w",
			retirementFailureWithRemediation(err),
		)
	}
	if err := mutationAuthority.validateProjectSelection(input.Paths.ManifestRoot); err != nil {
		return committedResult, fmt.Errorf(
			"%w; apply effects and statefile committed; recovery journal retired",
			err,
		)
	}

	return committedResult, nil
}

func (options ApplyOptions) statefileCommit(
	ctx context.Context,
	authority *mutationAuthority,
	path string,
	content []byte,
	mode os.FileMode,
) statefileCommitOutcome {
	if options.commitStatefile == nil {
		return authority.commitProjectStatefile(ctx, content, mode)
	}
	return options.commitStatefile(ctx, path, content, mode)
}

func (options ApplyOptions) validateBeforeEffects(
	ctx context.Context,
	authority mutation.PhysicalAuthoritySet,
) error {
	if options.ValidateBeforeEffects == nil {
		return nil
	}
	return options.ValidateBeforeEffects(ctx, authority)
}

func (options ApplyOptions) acceptVisibilityChanges(ctx context.Context) error {
	if options.AcceptVisibilityChanges == nil {
		return nil
	}
	return options.AcceptVisibilityChanges(ctx)
}

func (options ApplyOptions) validateCompensationAuthority(ctx context.Context) error {
	if options.ValidateCompensationAuthority == nil {
		return nil
	}
	return options.ValidateCompensationAuthority(ctx)
}

func (options ApplyOptions) acceptCompensationVisibilityChanges(ctx context.Context) error {
	if options.AcceptCompensationVisibilityChanges == nil {
		return nil
	}
	return options.AcceptCompensationVisibilityChanges(ctx)
}

type applyEventEmitter struct {
	sink         EventSink
	totalActions int
}

func (emitter applyEventEmitter) emit(kind EventKind, stage EventStage, action *ActionEventFacts, err error) {
	emitter.sink.Emit(Event{
		Kind:         kind,
		Stage:        stage,
		Action:       action,
		TotalActions: emitter.totalActions,
		Err:          err,
	})
}

type activeRetirementPlanLoader func(context.Context) (recovery.Plan, error)

type journalRetiredFailure struct {
	cause error
}

func (failure *journalRetiredFailure) Error() string {
	return fmt.Sprintf("%v; recovery journal retired; no recovery action remains", failure.cause)
}

func (failure *journalRetiredFailure) Unwrap() error {
	return failure.cause
}

func retirementFailureWithRemediation(err error) error {
	var retired *journalRetiredFailure
	if err == nil || journal.IsRetirementFinalizedWithGCResidue(err) || errors.As(err, &retired) {
		return err
	}
	return fmt.Errorf("%w; run: daem recover --dry-run", err)
}

func retireRecoveryJournalWithEvents(
	ctx context.Context,
	paths Paths,
	authority *mutationAuthority,
	events applyEventEmitter,
	loadPlan activeRetirementPlanLoader,
	stateCodec durable.SnapshotCodec,
	gate visibilityEffectGate,
	execution *activeRecoveryExecution,
) error {
	run := func(step activeRecoveryStep, action func() error) error {
		if execution == nil {
			return action()
		}
		return execution.runTerminalStep(step, action)
	}
	events.emit(EventJournalCleanupStarted, EventStageJournalCleanup, nil, nil)
	if loadPlan == nil {
		err := fmt.Errorf("recovery journal retirement plan loader is required")
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, err)
		return err
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/outer/validate-project-before-retirement",
			kind: operationplan.EffectStepObservation,
		},
		func() error { return authority.validateProjectSelection(paths.ManifestRoot) },
	); err != nil {
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, err)
		return err
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/outer/validate-visibility-before-retirement",
			kind: operationplan.EffectStepObservation,
		},
		func() error { return gate.validateBefore(ctx) },
	); err != nil {
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, err)
		return err
	}
	plan := recovery.Plan{}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/outer/reload-after-effects",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			var err error
			plan, err = loadPlan(ctx)
			return err
		},
	); err != nil {
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, err)
		return err
	}
	if err := authority.retireActiveJournal(ctx, plan, execution); err != nil {
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, err)
		return err
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/outer/accept-visibility",
			kind: operationplan.EffectStepObservation,
		},
		func() error { return gate.acceptAfter(ctx) },
	); err != nil {
		retired := &journalRetiredFailure{cause: err}
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, retired)
		return retired
	}
	events.emit(EventJournalCleaned, EventStageJournalCleanup, nil, nil)
	return nil
}
