package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/claimoperation"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	"github.com/isty2e/daem/internal/effect/execute/statefileoperation"

	"github.com/isty2e/daem/internal/effect/execute/removaloperation"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	"github.com/isty2e/daem/internal/assurance/durable"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
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

func (paths Paths) journalPaths() journalpublication.Paths {
	return journalpublication.Paths{
		RecoveryDir:   paths.RecoveryDir,
		StatefilePath: paths.StatefilePath,
		ManifestRoot:  paths.ManifestRoot,
		DataDir:       paths.DataDir,
	}
}

// ApplyInput retains typed effects, native transition intents and prepared payloads.
type ApplyInput struct {
	Paths                       Paths
	Resolver                    mutationnamespace.Resolver
	ManagedPathEffects          []effectpreparation.ManagedPathEffect
	AggregateEffects            []effectpreparation.AggregateEffect
	NativeNarrowings            []reconciliation.NativeNarrowingAction
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
	// ValidatePhysicalAuthorityRequests is an alternative to ValidateBeforeEffects
	// for observing bound requests together with current leases. The callbacks are
	// mutually exclusive; each invocation receives its own request slice.
	ValidatePhysicalAuthorityRequests func(context.Context, []mutation.PhysicalAuthorityRequest) error
	// AcceptVisibilityChanges re-observes and accepts path identity transitions
	// after one successful visibility-changing effect and its postcondition.
	AcceptVisibilityChanges func(context.Context) error
	// ValidateCompensationAuthority verifies that held namespace authority still
	// permits rollback after a forward visibility change was rejected.
	ValidateCompensationAuthority func(context.Context) error
	// AcceptCompensationVisibilityChanges accepts path identity transitions after
	// one successful compensating effect and its postcondition.
	AcceptCompensationVisibilityChanges func(context.Context) error
	commitStatefile                     func(context.Context, string, []byte, os.FileMode) error
	closeMutationAuthority              func(*mutationAuthority) error
}

// ApplyResult reports the committed mutation count and statefile path.
type ApplyResult struct {
	ActionCount int
	StatePath   string
	State       durable.Snapshot
	// ExecutionAttempted reports whether apply reached journal capture or a
	// later mutation boundary.
	ExecutionAttempted bool
	nativeSettlement   *NativeApplySettlement
}

// ApplyWithOptions commits typed executable effects and emits observational execution events.
func ApplyWithOptions(
	ctx context.Context,
	input ApplyInput,
	options ApplyOptions,
) (ApplyResult, error) {
	return applyWithOptions(ctx, input, nil, options)
}

// ApplyWithEffectPlan commits typed executable effects through the exact
// operation-prepared structural plan.
func ApplyWithEffectPlan(
	ctx context.Context,
	input ApplyInput,
	plan applyprogram.Plan,
	options ApplyOptions,
) (ApplyResult, error) {
	return applyWithOptions(ctx, input, &plan, options)
}

func applyWithOptions(
	ctx context.Context,
	input ApplyInput,
	preparedPlan *applyprogram.Plan,
	options ApplyOptions,
) (result ApplyResult, resultErr error) {
	executionAttempted := false
	defer func() {
		result.ExecutionAttempted = executionAttempted
	}()
	for index, effect := range input.ManagedPathEffects {
		if err := effect.Validate(); err != nil {
			return ApplyResult{}, fmt.Errorf("managed path effect[%d]: %w", index, err)
		}
	}
	for index, effect := range input.AggregateEffects {
		if err := effect.Validate(); err != nil {
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
	operationID := journalpublication.OperationID(createdAt)
	events := applyEventEmitter{
		sink: options.Events,
		totalActions: len(input.ManagedPathEffects) +
			effectpreparation.AggregateSubjectCount(input.AggregateEffects) +
			transition.retiredProjectClaimCount +
			transition.adoptedProjectClaimCount,
	}
	if !transition.changed {
		expectedPlan, planErr := compileApplyEffectPlan(
			transition,
			hostmutation.ManagedPathSchedule{},
			claimoperation.State{},
			nil,
		)
		if planErr != nil {
			return ApplyResult{}, planErr
		}
		execution, planErr := applyprogram.Begin(preparedPlan, expectedPlan)
		if planErr != nil {
			return ApplyResult{}, planErr
		}
		if planErr := execution.Finish(nil); planErr != nil {
			return ApplyResult{}, planErr
		}
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
	managedMutations, err := effectpreparation.ManagedPathJournalMutations(input.ManagedPathEffects)
	if err != nil {
		return ApplyResult{}, err
	}
	managedSchedule, err := hostmutation.PlanManagedPaths(input.ManagedPathEffects)
	if err != nil {
		return ApplyResult{}, err
	}
	aggregateMutations, err := effectpreparation.AggregateJournalMutations(input.AggregateEffects)
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
	entrySelections, err := journalrecord.EntrySelections(managedMutations, aggregateMutations)
	if err != nil {
		return ApplyResult{}, err
	}
	managedOwnership, err := claimoperation.PlanManagedPaths(
		input.ManagedPathEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("derive managed path ownership claim transitions: %w", err)
	}
	aggregateOwnership, err := claimoperation.PlanAggregates(
		input.AggregateEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("derive managed aggregate ownership claim transitions: %w", err)
	}
	ownershipState := claimoperation.NewState(managedOwnership, aggregateOwnership)
	if ownershipState.HasMutations() && input.OwnershipRegistryBinder == nil {
		return ApplyResult{}, fmt.Errorf("apply ownership registry binder is required")
	}
	expectedPlan, err := compileApplyEffectPlan(
		transition,
		managedSchedule,
		ownershipState,
		input.AggregateEffects,
	)
	if err != nil {
		return ApplyResult{}, err
	}
	effectExecution, err := applyprogram.Begin(preparedPlan, expectedPlan)
	if err != nil {
		return ApplyResult{}, err
	}
	defer func() {
		if finishErr := effectExecution.Finish(resultErr); finishErr != nil {
			resultErr = errors.Join(resultErr, finishErr)
		}
	}()
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
	defer func() {
		if closeErr := options.closeAuthority(mutationAuthority); closeErr != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("close apply mutation authority: %w", closeErr),
			)
		}
	}()
	if err := mutationAuthority.bindProjectStatefile(
		input.Paths.ManifestRoot,
		input.Paths.StatefilePath,
	); err != nil {
		return ApplyResult{}, err
	}
	journalResolver := mutationAuthority.destinations.JournalResolver(resolver)
	var registryStore ownershipmutation.RegistryStore
	if ownershipState.HasMutations() {
		if err := mutationAuthority.bindOwnershipRegistry(input.Paths.OwnershipRegistryPath); err != nil {
			return ApplyResult{}, err
		}
		registryStore, err = mutationAuthority.rootedOwnershipRegistry()
		if err != nil {
			return ApplyResult{}, err
		}
	}
	visibilityGate := applyprogram.NewVisibilityGate(
		func(ctx context.Context) error {
			return options.validatePhysicalAuthority(ctx, mutationAuthority)
		},
		options.acceptVisibilityChanges,
	)
	compensationGate := applyprogram.NewVisibilityGate(options.validateCompensationAuthority, options.acceptCompensationVisibilityChanges)
	journalPublicationGate := effectExecution.VisibilityGate(
		visibilityGate,
		"apply/journal-publication",
		operationplan.EffectStepPersistence,
		applyprogram.FailureNoCompensation,
	)
	if err := mutationAuthority.removals.PrepareApplyForward(
		ctx,
		func(demands recovery.RemovalDemandSet) ([]removaloperation.ForwardCandidate, error) {
			return applyForwardRemovalCertificates(input.ManagedPathEffects, input.AggregateEffects, input.Payloads, demands)
		},
	); err != nil {
		return ApplyResult{}, fmt.Errorf("prepare bounded forward removals: %w", err)
	}
	if err := journalPublicationGate.ValidateBefore(ctx); err != nil {
		return ApplyResult{}, effectExecution.SettleFailureWithoutCompensation(err)
	}
	if err := journalPublicationGate.ApplyEffect(func() error {
		if bindErr := mutationAuthority.bindRecoveryJournal(
			input.Paths.ManifestRoot,
			filepath.Join(input.Paths.RecoveryDir, operationID),
		); bindErr != nil {
			return bindErr
		}
		events.emit(EventJournalCaptureStarted, EventStageJournalCapture, nil, nil)
		executionAttempted = true
		captureResult, captureErr := journalpublication.CaptureJournalWithOptions(
			ctx,
			input.Paths.journalPaths(),
			operationID,
			createdAt,
			input.CurrentState,
			nextState,
			journalpublication.CaptureOptions{
				Filesystem:                input.Filesystem,
				ClaimTransitions:          ownershipState.Transitions(),
				ProvisionalAcquires:       ownershipState.Intents(),
				ManagedPathMutations:      managedMutations,
				ManagedAggregateMutations: aggregateMutations,
				ManagedPathEvidence:       input.ManagedPathEvidence,
				RemovalDemands:            removalDemands,
				Resolver:                  journalResolver,
				ManifestRoot:              mutationAuthority.capturedRoot,
				OperationAuthority:        mutationAuthority.recoveryJournal,
				RootedCapability:          mutationAuthority.destinations.JournalCapability,
				Codecs:                    input.Codecs,
				StateCodec:                input.StateCodec,
			},
		)
		if captureErr != nil {
			events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, captureErr)
			return fmt.Errorf("capture recovery journal: %w", captureErr)
		}
		if basisErr := mutationAuthority.captureJournalExecutionBasis(
			ctx,
			captureResult.RecordFingerprint,
		); basisErr != nil {
			events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, basisErr)
			return basisErr
		}
		activePlan, loadErr := loadApplyActivePlanForState(
			ctx,
			input.Paths,
			input.CurrentState,
			mutationAuthority,
			input.StateCodec,
			input.Codecs,
		)
		if loadErr != nil {
			events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, loadErr)
			return fmt.Errorf("bind captured removal authority: %w", loadErr)
		}
		if basisErr := mutationAuthority.validateJournalExecutionBasis(
			ctx,
			activePlan,
			"after publication",
		); basisErr != nil {
			events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, basisErr)
			return basisErr
		}
		if bindErr := mutationAuthority.removals.BindPlan(activePlan); bindErr != nil {
			events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, bindErr)
			return fmt.Errorf("bind captured removal authority: %w", bindErr)
		}
		if prepareErr := mutationAuthority.prepareActiveJournalRetirement(
			ctx,
			input.Paths,
			activePlan,
			input.StateCodec,
		); prepareErr != nil {
			events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, prepareErr)
			return fmt.Errorf("prepare bounded journal retirement: %w", prepareErr)
		}
		return nil
	}); err != nil {
		return ApplyResult{}, effectExecution.SettleFailureWithoutCompensation(err)
	}
	if err := journalPublicationGate.AcceptAfter(ctx); err != nil {
		events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, err)
		primary := fmt.Errorf(
			"accept recovery journal visibility: %w; recovery journal retained; run: daem recover --dry-run",
			err,
		)
		return ApplyResult{}, effectExecution.SettleFailureWithoutCompensation(primary)
	}
	if err := effectExecution.Observe(
		"apply/journal-publication/post-validation",
		applyprogram.FailureNoCompensation,
		func() error {
			return validateCapturedJournalFingerprint(
				ctx,
				input.Paths,
				input.CurrentState,
				mutationAuthority,
				input.StateCodec,
				input.Codecs,
				"before prepared effects",
			)
		},
	); err != nil {
		events.emit(EventJournalCaptureFailed, EventStageJournalCapture, nil, err)
		return ApplyResult{}, effectExecution.SettleFailureWithoutCompensation(err)
	}
	events.emit(EventJournalCaptured, EventStageJournalCapture, nil, nil)
	ownershipPreparationGate := effectExecution.VisibilityGate(
		visibilityGate,
		"apply/ownership-preparation",
		operationplan.EffectStepPersistence,
		applyprogram.FailureClaimPreparation,
	)
	if err := claimoperation.Prepare(
		ctx,
		registryStore,
		ownershipState.Transitions(),
		ownershipPreparationGate,
	); err != nil {
		return ApplyResult{}, settlePreHostApplyFailure(
			ctx,
			err,
			applyprogram.SettlementClaimPreparation,
			"before claim-preparation rollback retirement",
			input.Paths,
			input.CurrentState,
			mutationAuthority,
			registryStore,
			ownershipState.Transitions(),
			events,
			input.StateCodec,
			input.Codecs,
			compensationGate,
			effectExecution,
		)
	}

	events.emit(EventRollbackStageStarted, EventStageRollbackStage, nil, nil)
	if err := effectExecution.Observe(
		"apply/prepared-effects-validation",
		applyprogram.FailurePreparedEffects,
		func() error {
			return validateApplyRecoveryPrepared(
				ctx,
				input.Paths,
				input.CurrentState,
				mutationAuthority,
				input.StateCodec,
				input.Codecs,
			)
		},
	); err != nil {
		events.emit(EventRollbackStageFailed, EventStageRollbackStage, nil, err)
		return ApplyResult{}, settlePreHostApplyFailure(
			ctx,
			err,
			applyprogram.SettlementPreparedEffects,
			"before prepared-effect rollback retirement",
			input.Paths,
			input.CurrentState,
			mutationAuthority,
			registryStore,
			ownershipState.Transitions(),
			events,
			input.StateCodec,
			input.Codecs,
			compensationGate,
			effectExecution,
		)
	}
	events.emit(EventRollbackStaged, EventStageRollbackStage, nil, nil)

	managedProgress := newHostActionProgress(managedSchedule.MutationCount())
	promoteManagedOwnership := func(
		ctx context.Context,
		effect effectpreparation.ManagedPathEffect,
		phase hostmutation.ManagedPathPhase,
	) error {
		return ownershipState.Promote(
			ctx,
			provisionalAcquireKeysForManagedPath(effect, phase),
			ordinaryClaimPromoter(mutationAuthority, registryStore, input.StateCodec, visibilityGate, effectExecution),
		)
	}
	if err := applyManagedPathPhaseWithEvents(
		ctx, managedSchedule, hostmutation.ManagedPathPublishPhase, input.Payloads, mutationAuthority,
		managedProgress, 0, events, visibilityGate, effectExecution, promoteManagedOwnership,
	); err != nil {
		progress := combineHostActionProgress(
			managedProgress, newHostActionProgress(len(aggregateMutations)),
		)
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
			effectExecution,
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
		effectExecution,
		func(ctx context.Context, effect effectpreparation.AggregateEffect) error {
			keys := provisionalAcquireKeysForAggregate(effect)
			return ownershipState.Promote(
				ctx,
				keys,
				ordinaryClaimPromoter(mutationAuthority, registryStore, input.StateCodec, visibilityGate, effectExecution),
			)
		},
	)
	progress := combineHostActionProgress(managedProgress, aggregateProgress)
	if err != nil {
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
			effectExecution,
		)
	}
	if err := applyManagedPathPhaseWithEvents(
		ctx, managedSchedule, hostmutation.ManagedPathRetirePhase, input.Payloads, mutationAuthority,
		managedProgress, 0, events, visibilityGate, effectExecution, promoteManagedOwnership,
	); err != nil {
		progress = combineHostActionProgress(managedProgress, aggregateProgress)
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
			effectExecution,
		)
	}
	progress = combineHostActionProgress(managedProgress, aggregateProgress)
	if err := effectExecution.Observe(
		"apply/statefile-publication/context-validation",
		applyprogram.FailureGuardedRecovery,
		func() error { return ctx.Err() },
	); err != nil {
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
			effectExecution,
		)
	}
	if err := effectExecution.Observe(
		"apply/statefile-publication/project-validation",
		applyprogram.FailureGuardedRecovery,
		func() error {
			return mutationAuthority.validateProjectSelection(input.Paths.ManifestRoot)
		},
	); err != nil {
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
			effectExecution,
		)
	}
	statefileGate := effectExecution.StatefileVisibilityGate(visibilityGate)
	if err := statefileGate.ValidateBefore(ctx); err != nil {
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
			effectExecution,
		)
	}
	var commit statefileoperation.Outcome
	commitAttempted := false
	commitErr := statefileGate.ApplyEffect(func() error {
		events.emit(EventStatefileWriteStarted, EventStageStatefileWrite, nil, nil)
		commitAttempted = true
		commit = options.statefileCommit(
			ctx,
			mutationAuthority,
			input.Paths.StatefilePath,
			stateContent,
			0o600,
		)
		if commit.Committed() {
			return nil
		}
		return fmt.Errorf("write statefile: %w", commit.Failure())
	})
	if commitErr != nil {
		if commitAttempted {
			events.emit(EventStatefileWriteFailed, EventStageStatefileWrite, nil, commitErr)
			if commit.RequiresRecovery() {
				primary := fmt.Errorf(
					"%w; statefile commit is indeterminate; recovery journal retained; run: daem recover --dry-run",
					commitErr,
				)
				return ApplyResult{}, effectExecution.SettleFailureWithoutCompensation(primary)
			}
		}
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, commitErr, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs, compensationGate,
			effectExecution,
		)
	}
	committedResult := ApplyResult{
		ActionCount: len(input.ManagedPathEffects) +
			effectpreparation.AggregateSubjectCount(input.AggregateEffects) +
			transition.retiredProjectClaimCount +
			transition.adoptedProjectClaimCount,
		StatePath: input.Paths.StatefilePath,
		State:     nextState,
	}
	if err := statefileGate.AcceptAfter(ctx); err != nil {
		primary := fmt.Errorf(
			"%w; apply effects and statefile committed; recovery journal retained; run: daem recover --dry-run",
			err,
		)
		return committedResult, effectExecution.SettleFailureWithoutCompensation(primary)
	}
	events.emit(EventStatefileWritten, EventStageStatefileWrite, nil, nil)
	if err := effectExecution.Observe(
		"apply/post-statefile/project-validation",
		applyprogram.FailureNoCompensation,
		func() error {
			return mutationAuthority.validateProjectSelection(input.Paths.ManifestRoot)
		},
	); err != nil {
		primary := fmt.Errorf(
			"%w; apply effects and statefile committed; recovery journal retained; run: daem recover --dry-run",
			err,
		)
		return committedResult, effectExecution.SettleFailureWithoutCompensation(primary)
	}
	if err := effectExecution.Observe(
		"apply/post-statefile/context-validation",
		applyprogram.FailureNoCompensation,
		func() error { return ctx.Err() },
	); err != nil {
		primary := fmt.Errorf("%w; apply effects and statefile committed; recovery journal retained; run: daem recover --dry-run", err)
		return committedResult, effectExecution.SettleFailureWithoutCompensation(primary)
	}
	ownershipFinalizationGate := effectExecution.VisibilityGate(
		visibilityGate,
		"apply/ownership-finalization",
		operationplan.EffectStepPersistence,
		applyprogram.FailureNoCompensation,
	)
	if err := claimoperation.Finalize(
		ctx,
		registryStore,
		ownershipState.Transitions(),
		ownershipFinalizationGate,
	); err != nil {
		primary := fmt.Errorf("%w; apply effects and statefile committed; recovery journal retained; run: daem recover --dry-run", err)
		return committedResult, effectExecution.SettleFailureWithoutCompensation(primary)
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
	journalRetirementGate := effectExecution.VisibilityGate(
		visibilityGate,
		"apply/journal-retirement",
		operationplan.EffectStepRetirement,
		applyprogram.FailureNoCompensation,
	)
	if err := retireRecoveryJournalWithEvents(
		ctx,
		input.Paths,
		mutationAuthority,
		events,
		loadRetirementPlan,
		input.StateCodec,
		journalRetirementGate,
		nil,
	); err != nil {
		primary := fmt.Errorf(
			"retire recovery journal: %w",
			retirementFailureWithRemediation(err),
		)
		return committedResult, effectExecution.SettleFailureWithoutCompensation(primary)
	}
	if err := effectExecution.Observe(
		"apply/journal-retirement/final-project-validation",
		applyprogram.FailureNoCompensation,
		func() error {
			return mutationAuthority.validateProjectSelection(input.Paths.ManifestRoot)
		},
	); err != nil {
		primary := fmt.Errorf(
			"%w; apply effects and statefile committed; recovery journal retired",
			err,
		)
		return committedResult, effectExecution.SettleFailureWithoutCompensation(primary)
	}

	return committedResult, nil
}

func (options ApplyOptions) closeAuthority(authority *mutationAuthority) error {
	if options.closeMutationAuthority == nil {
		return authority.close()
	}
	return options.closeMutationAuthority(authority)
}

func (options ApplyOptions) statefileCommit(
	ctx context.Context,
	authority *mutationAuthority,
	path string,
	content []byte,
	mode os.FileMode,
) statefileoperation.Outcome {
	if options.commitStatefile == nil {
		var selected *statefileoperation.Selected
		if authority != nil {
			selected = authority.projectStatefile
		}
		return selected.Commit(ctx, content, mode)
	}
	return statefileoperation.FromError(options.commitStatefile(ctx, path, content, mode))
}

func (options ApplyOptions) validatePhysicalAuthority(ctx context.Context, authority *mutationAuthority) error {
	if authority == nil {
		return fmt.Errorf("mutation authority is required")
	}
	if options.ValidatePhysicalAuthorityRequests != nil {
		if options.ValidateBeforeEffects != nil {
			return fmt.Errorf("apply physical authority validators are mutually exclusive")
		}
		requests := append([]mutation.PhysicalAuthorityRequest(nil), authority.physicalAuthorityRequests...)
		return options.ValidatePhysicalAuthorityRequests(ctx, requests)
	}
	physicalAuthority, err := authority.physicalAuthority()
	if err != nil {
		return err
	}
	return options.validateBeforeEffects(ctx, physicalAuthority)
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
	gate applyprogram.VisibilityGate,
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
		func() error {
			return gate.Observe(
				"project-validation",
				func() error { return authority.validateProjectSelection(paths.ManifestRoot) },
			)
		},
	); err != nil {
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, err)
		return err
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/outer/validate-visibility-before-retirement",
			kind: operationplan.EffectStepObservation,
		},
		func() error { return gate.ValidateBefore(ctx) },
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
			return gate.Observe("reload", func() error {
				var err error
				plan, err = loadPlan(ctx)
				return err
			})
		},
	); err != nil {
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, err)
		return err
	}
	if err := gate.ApplyEffect(func() error {
		return authority.retireActiveJournal(ctx, plan, execution)
	}); err != nil {
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, err)
		return err
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/outer/accept-visibility",
			kind: operationplan.EffectStepObservation,
		},
		func() error { return gate.AcceptAfter(ctx) },
	); err != nil {
		retired := &journalRetiredFailure{cause: err}
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, retired)
		return retired
	}
	events.emit(EventJournalCleaned, EventStageJournalCleanup, nil, nil)
	return nil
}
