package execute

import (
	"context"
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
	// ValidateBeforeEffects runs once after all rooted effect authority is bound
	// and before journal capture or any host/state mutation.
	ValidateBeforeEffects func(context.Context, mutation.PhysicalAuthoritySet) error
	commitStatefile       statefileCommitter
}

// ApplyResult reports the committed mutation count and statefile path.
type ApplyResult struct {
	ActionCount int
	StatePath   string
	State       durable.Snapshot
}

// ApplyWithOptions commits typed executable effects and emits observational execution events.
func ApplyWithOptions(ctx context.Context, input ApplyInput, options ApplyOptions) (ApplyResult, error) {
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
	nextState, err := snapshotAfterManagedPathEffects(input.CurrentState, input.ManagedPathEffects)
	if err != nil {
		return ApplyResult{}, err
	}
	nextState, err = snapshotAfterAggregateEffects(nextState, input.AggregateEffects)
	if err != nil {
		return ApplyResult{}, err
	}
	nextState, globalCarrierStateChanged, err := nextState.WithConvergedGlobalCarrierClaims(
		input.GlobalCarrierClaims,
	)
	if err != nil {
		return ApplyResult{}, err
	}
	nextState, retiredProjectClaimCount, err := snapshotAfterRetiredProjectCarrierClaims(
		nextState,
		input.RetiredProjectCarrierClaims,
	)
	if err != nil {
		return ApplyResult{}, err
	}
	promotedClaims, err := promotedProjectCarrierClaims(
		nextState,
		input.ConfirmedRelationActions,
	)
	if err != nil {
		return ApplyResult{}, err
	}
	nextState, relationStateChanged, err := nextState.WithPromotedCarrierClaims(promotedClaims)
	if err != nil {
		return ApplyResult{}, err
	}
	projectClaimCountBeforeAdoption := len(nextState.ManagedCarrierClaims())
	nextState, adoptionStateChanged, err := nextState.WithAdoptedCarrierClaims(
		input.AdoptedProjectCarrierClaims,
	)
	if err != nil {
		return ApplyResult{}, err
	}
	adoptedProjectClaimCount := len(nextState.ManagedCarrierClaims()) -
		projectClaimCountBeforeAdoption

	createdAt := time.Now().UTC()
	operationID := journal.OperationID(createdAt)
	events := applyEventEmitter{
		sink: options.Events,
		totalActions: len(input.ManagedPathEffects) +
			aggregateSubjectCount(input.AggregateEffects) +
			retiredProjectClaimCount +
			adoptedProjectClaimCount,
	}
	if len(input.ManagedPathEffects) == 0 &&
		len(input.AggregateEffects) == 0 &&
		!globalCarrierStateChanged &&
		retiredProjectClaimCount == 0 &&
		!relationStateChanged &&
		!adoptionStateChanged {
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
	entrySelections, err := journal.EntrySelections(managedMutations, aggregateMutations)
	if err != nil {
		return ApplyResult{}, err
	}
	claimTransitions, err := claimTransitionsForManagedPathEffects(
		input.ManagedPathEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("derive managed path ownership claim transitions: %w", err)
	}
	aggregateClaimTransitions, err := claimTransitionsForAggregateEffects(
		input.AggregateEffects,
		input.Owner,
		input.Ownership,
		operationID,
	)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("derive managed aggregate ownership claim transitions: %w", err)
	}
	claimTransitions = append(claimTransitions, aggregateClaimTransitions...)
	if len(claimTransitions) != 0 && input.OwnershipRegistryBinder == nil {
		return ApplyResult{}, fmt.Errorf("apply ownership registry binder is required")
	}
	mutationAuthority, err := newMutationAuthorityWithProjectionEffects(
		input.Paths,
		input.ManagedPathEffects,
		input.AggregateEffects,
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
	if len(claimTransitions) != 0 {
		if err := mutationAuthority.bindOwnershipRegistry(input.Paths.OwnershipRegistryPath); err != nil {
			return ApplyResult{}, err
		}
		registryStore, err = mutationAuthority.rootedOwnershipRegistry()
		if err != nil {
			return ApplyResult{}, err
		}
	}
	physicalAuthority, err := mutationAuthority.physicalAuthority()
	if err != nil {
		return ApplyResult{}, err
	}
	if err := options.validateBeforeEffects(ctx, physicalAuthority); err != nil {
		return ApplyResult{}, err
	}
	journalProjectRoot := mutationAuthority.capturedRoot
	if !hasProjectJournalEntries(input.ManagedPathEffects, input.AggregateEffects) {
		journalProjectRoot = nil
	}
	if err := mutationAuthority.bindRecoveryJournal(
		input.Paths.ManifestRoot,
		filepath.Join(input.Paths.RecoveryDir, operationID),
	); err != nil {
		return ApplyResult{}, err
	}
	events.emit(EventJournalCaptureStarted, EventStageJournalCapture, nil, nil)
	_, err = journal.CaptureJournalWithOptions(
		ctx,
		input.Paths.journalPaths(),
		operationID,
		createdAt,
		input.CurrentState,
		nextState,
		journal.CaptureOptions{
			Filesystem:                input.Filesystem,
			ClaimTransitions:          claimTransitions,
			ManagedPathMutations:      managedMutations,
			ManagedAggregateMutations: aggregateMutations,
			ManagedPathEvidence:       input.ManagedPathEvidence,
			Resolver:                  journalResolver,
			ProjectRoot:               journalProjectRoot,
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
	events.emit(EventJournalCaptured, EventStageJournalCapture, nil, nil)
	if err := prepareClaimTransitions(ctx, registryStore, claimTransitions); err != nil {
		rollbackErr := rollbackClaimsToBefore(context.WithoutCancel(ctx), registryStore, claimTransitions)
		if rollbackErr != nil {
			return ApplyResult{}, fmt.Errorf("%w; ownership rollback failed: %v; recovery journal retained; run: daem recover --dry-run", err, rollbackErr)
		}
		loadRetirementPlan := func(ctx context.Context) (recovery.Plan, error) {
			return loadApplyActivePlanForStateEntries(
				ctx,
				input.Paths,
				input.CurrentState,
				[]journal.EntrySelection{},
				mutationAuthority,
				input.StateCodec,
				input.Codecs,
			)
		}
		cleanupErr := retireRecoveryJournalWithEvents(
			context.WithoutCancel(ctx),
			input.Paths,
			mutationAuthority,
			events,
			loadRetirementPlan,
		)
		if cleanupErr != nil {
			return ApplyResult{}, fmt.Errorf("%w; retire recovery journal failed: %v; run: daem recover --dry-run", err, cleanupErr)
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
		if claimErr := rollbackClaimsToBefore(context.WithoutCancel(ctx), registryStore, claimTransitions); claimErr != nil {
			return ApplyResult{}, fmt.Errorf("%w; ownership rollback failed: %v; recovery journal retained; run: daem recover --dry-run", err, claimErr)
		}
		loadRetirementPlan := func(ctx context.Context) (recovery.Plan, error) {
			return loadApplyActivePlanForStateEntries(
				ctx,
				input.Paths,
				input.CurrentState,
				[]journal.EntrySelection{},
				mutationAuthority,
				input.StateCodec,
				input.Codecs,
			)
		}
		cleanupErr := retireRecoveryJournalWithEvents(
			context.WithoutCancel(ctx),
			input.Paths,
			mutationAuthority,
			events,
			loadRetirementPlan,
		)
		if cleanupErr != nil {
			return ApplyResult{}, fmt.Errorf("%w; retire recovery journal failed: %v", err, cleanupErr)
		}
		return ApplyResult{}, err
	}
	events.emit(EventRollbackStaged, EventStageRollbackStage, nil, nil)

	managedProgress := newHostActionProgress(managedSchedule.mutationCount)
	if err := applyManagedPathPhaseWithEvents(
		ctx, managedSchedule, managedPathPublishPhase, input.Payloads, mutationAuthority,
		managedProgress, 0, events,
	); err != nil {
		progress := combineHostActionProgress(
			managedProgress, newHostActionProgress(len(aggregateMutations)),
		)
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs,
		)
	}

	aggregateProgress, err := applyAggregateEffectsWithEvents(
		ctx,
		input.AggregateEffects,
		mutationAuthority,
		input.Codecs,
		len(input.ManagedPathEffects),
		events,
	)
	progress := combineHostActionProgress(managedProgress, aggregateProgress)
	if err != nil {
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs,
		)
	}
	if err := applyManagedPathPhaseWithEvents(
		ctx, managedSchedule, managedPathRetirePhase, input.Payloads, mutationAuthority,
		managedProgress, 0, events,
	); err != nil {
		progress = combineHostActionProgress(managedProgress, aggregateProgress)
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs,
		)
	}
	progress = combineHostActionProgress(managedProgress, aggregateProgress)
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs,
		)
	}
	if err := mutationAuthority.validateProjectSelection(input.Paths.ManifestRoot); err != nil {
		return ApplyResult{}, applyRecoveryErrorWithEvents(
			ctx, err, input.Paths, input.CurrentState, progress, entrySelections,
			events, mutationAuthority, input.StateCodec, input.Codecs,
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
			events, mutationAuthority, input.StateCodec, input.Codecs,
		)
	}
	committedResult := ApplyResult{
		ActionCount: len(input.ManagedPathEffects) +
			aggregateSubjectCount(input.AggregateEffects) +
			retiredProjectClaimCount +
			adoptedProjectClaimCount,
		StatePath: input.Paths.StatefilePath,
		State:     nextState,
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
	if err := finalizeClaimTransitions(ctx, registryStore, claimTransitions); err != nil {
		return committedResult, fmt.Errorf("%w; apply effects and statefile committed; recovery journal retained; run: daem recover --dry-run", err)
	}
	loadRetirementPlan := func(ctx context.Context) (recovery.Plan, error) {
		return loadApplyActivePlanForState(
			ctx,
			input.Paths,
			nextState,
			mutationAuthority,
			input.StateCodec,
			input.Codecs,
		)
	}
	if err := retireRecoveryJournalWithEvents(
		ctx,
		input.Paths,
		mutationAuthority,
		events,
		loadRetirementPlan,
	); err != nil {
		return committedResult, fmt.Errorf("retire recovery journal: %w; run: daem recover --dry-run", err)
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

func retireRecoveryJournalWithEvents(
	ctx context.Context,
	paths Paths,
	authority *mutationAuthority,
	events applyEventEmitter,
	loadPlan activeRetirementPlanLoader,
) error {
	events.emit(EventJournalCleanupStarted, EventStageJournalCleanup, nil, nil)
	if loadPlan == nil {
		err := fmt.Errorf("recovery journal retirement plan loader is required")
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, err)
		return err
	}
	if err := authority.validateProjectSelection(paths.ManifestRoot); err != nil {
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, err)
		return err
	}
	plan, err := loadPlan(ctx)
	if err == nil {
		err = authority.retireActiveJournal(ctx, paths, plan)
	}
	if err != nil {
		events.emit(EventJournalCleanupFailed, EventStageJournalCleanup, nil, err)
		return err
	}
	events.emit(EventJournalCleaned, EventStageJournalCleanup, nil, nil)
	return nil
}
