package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// RecoveryOptions configures workflow-owned validation before and after each
// visibility-changing recovery effect.
type RecoveryOptions struct {
	ValidateBeforeEffects       func(context.Context, mutation.PhysicalAuthoritySet) error
	ValidateVisibilityAuthority func(context.Context) error
	AcceptVisibilityChanges     func(context.Context) error
	ActiveJournalAuthority      journal.ActiveJournalAuthority
	Resolver                    DestinationResolver
	Codecs                      aggregate.CodecCatalog
	OwnershipRegistryBinder     ownershipmutation.RootedRegistryBinder
	StateCodec                  durable.SnapshotCodec
	StateReader                 durable.SnapshotReader
	Filesystem                  mutationfs.Store
	reloadPlan                  func(context.Context, journal.PlanLoadOptions) (recovery.Plan, error)
	mutationAuthority           *mutationAuthority
	beforeHostAction            func(int) error
	beforeRetirement            func() error
}

// ExecuteRecoveryPlanWithOptions applies a journal-derived recovery plan after
// invoking the workflow's final authority validation.
func ExecuteRecoveryPlanWithOptions(
	ctx context.Context,
	plan recovery.Plan,
	paths Paths,
	options RecoveryOptions,
) error {
	if ctx == nil {
		return fmt.Errorf("recovery context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.Classification() == recovery.ClassificationBlocked {
		return fmt.Errorf("recovery is blocked")
	}
	if options.Resolver == nil {
		return fmt.Errorf("recovery destination resolver is required")
	}
	if options.StateCodec == nil {
		return fmt.Errorf("recovery state codec is required")
	}
	if options.reloadPlan == nil && options.StateReader == nil {
		return fmt.Errorf("recovery state reader is required")
	}
	if options.Filesystem == nil {
		return fmt.Errorf("recovery filesystem is required")
	}
	if err := options.ActiveJournalAuthority.Validate(); err != nil {
		return err
	}
	if options.reloadPlan == nil {
		options.reloadPlan = func(
			ctx context.Context,
			loadOptions journal.PlanLoadOptions,
		) (recovery.Plan, error) {
			return journal.LoadActivePlanWithOptions(
				ctx,
				paths.journalPaths(),
				loadOptions,
			)
		}
	}
	authority := options.mutationAuthority
	ownsAuthority := false
	if authority == nil {
		var err error
		authority, err = newRecoveryMutationAuthority(
			paths,
			plan,
			options.Resolver,
			options.Filesystem,
			options.OwnershipRegistryBinder,
		)
		if err != nil {
			return err
		}
		ownsAuthority = true
		options.mutationAuthority = authority
	}
	if ownsAuthority {
		defer authority.close()
	}
	if err := authority.bindRecoveryJournal(paths.ManifestRoot, plan.OperationDir()); err != nil {
		return err
	}
	if err := authority.bindRecoveryStatefileSemanticEntry(paths.StatefilePath); err != nil {
		return err
	}
	journalFingerprint, err := plan.JournalAuthorityFingerprint()
	if err != nil {
		return err
	}
	if err := authority.setJournalExecutionBasis(
		journalFingerprint,
		options.ActiveJournalAuthority,
	); err != nil {
		return err
	}
	if err := authority.validateJournalExecutionBasis(
		ctx,
		plan,
		"before recovery execution",
	); err != nil {
		return err
	}
	if err := authority.bindRemovalIntents(plan); err != nil {
		return fmt.Errorf("bind recovery removal authority: %w", err)
	}
	if err := executeRecoveryPlanEffects(ctx, plan, paths, options); err != nil {
		return err
	}
	if err := authority.validateProjectSelection(paths.ManifestRoot); err != nil {
		return fmt.Errorf("%w; recovery effects committed; recovery journal retained", err)
	}
	visibilityGate := recoveryVisibilityGate(options)
	if err := visibilityGate.validateBefore(ctx); err != nil {
		return fmt.Errorf("%w; recovery effects committed; recovery journal retained", err)
	}
	retirementPlan, err := reloadRecoveryPlanAfterEffects(ctx, plan, options, authority)
	if err != nil {
		return fmt.Errorf("%w; recovery effects committed; recovery journal retained", err)
	}
	if options.beforeRetirement != nil {
		if err := options.beforeRetirement(); err != nil {
			return fmt.Errorf("before recovery journal retirement: %w", err)
		}
	}
	if err := authority.retireActiveJournal(
		ctx,
		retirementPlan,
	); err != nil {
		return fmt.Errorf("retire recovery journal: %w", err)
	}
	if err := visibilityGate.acceptAfter(ctx); err != nil {
		return fmt.Errorf("%w; recovery effects committed; recovery journal retired", err)
	}
	if err := authority.validateProjectSelection(paths.ManifestRoot); err != nil {
		return fmt.Errorf("%w; recovery effects committed; recovery journal retired", err)
	}
	return nil
}

func executeRecoveryPlanEffects(ctx context.Context, plan recovery.Plan, paths Paths, options RecoveryOptions) error {
	authority := options.mutationAuthority
	ownsAuthority := false
	requiresOwnershipRegistry := len(plan.ClaimTransitions()) != 0 || len(plan.ProvisionalAcquireIntents()) != 0
	var err error
	if authority == nil {
		if requiresOwnershipRegistry && options.OwnershipRegistryBinder == nil {
			return fmt.Errorf("recovery ownership registry binder is required")
		}
		authority, err = newRecoveryMutationAuthority(
			paths,
			plan,
			options.Resolver,
			options.Filesystem,
			options.OwnershipRegistryBinder,
		)
		ownsAuthority = true
	} else {
		if authority.ownershipRegistryBinder == nil {
			authority.ownershipRegistryBinder = options.OwnershipRegistryBinder
		}
		if requiresOwnershipRegistry &&
			!authority.hasOwnershipRegistry &&
			authority.ownershipRegistryBinder == nil {
			return fmt.Errorf("recovery ownership registry binder is required")
		}
		err = requireRecoveryGlobalBindings(authority, plan.GuardedActions())
	}
	if err != nil {
		return err
	}
	if ownsAuthority {
		defer authority.close()
	}
	if !authority.statefileSemanticEntry.valid() {
		if err := authority.bindRecoveryStatefileSemanticEntry(paths.StatefilePath); err != nil {
			return err
		}
	}
	var registryStore ownershipmutation.RegistryStore
	if requiresOwnershipRegistry {
		if err := authority.bindOwnershipRegistry(paths.OwnershipRegistryPath); err != nil {
			return err
		}
		registryStore, err = authority.rootedOwnershipRegistry()
		if err != nil {
			return err
		}
	}
	physicalAuthority, err := authority.physicalAuthority()
	if err != nil {
		return err
	}

	current, err := recoveryPlanBeforeEffects(
		ctx,
		plan,
		options,
		physicalAuthority,
		recoveryPlanLoadOptions(options, authority),
	)
	if err != nil {
		return err
	}
	plan = current
	if err := authority.bindRemovalIntents(plan); err != nil {
		return fmt.Errorf("bind reloaded recovery removal authority: %w", err)
	}
	if err := authority.prepareActiveJournalRetirement(
		ctx,
		paths,
		plan,
		options.StateCodec,
	); err != nil {
		return fmt.Errorf("prepare bounded journal retirement: %w", err)
	}
	visibilityGate := recoveryVisibilityGate(options)

	switch plan.Classification() {
	case recovery.ClassificationCleanBefore, recovery.ClassificationCleanAfter:
		if err := authority.reserveRecoverySemanticValidations(
			recoverySemanticValidationCount(plan, 0),
		); err != nil {
			return fmt.Errorf("reserve recovery semantic validation: %w", err)
		}
		if err := authority.physicalWorkBudget.ConcludeScratchCleanupNotApplicable(); err != nil {
			return fmt.Errorf("conclude absent recovery rollback stage: %w", err)
		}
		if err := authority.prepareRecoveryRemovalCleanup(plan); err != nil {
			return fmt.Errorf("prepare bounded removal cleanup: %w", err)
		}
		return authority.beginGeneralRecoveryExecution()
	case recovery.ClassificationNeedsRollback, recovery.ClassificationNeedsFinalize:
	default:
		return fmt.Errorf("unsupported recovery classification %q", plan.Classification())
	}
	switch plan.Classification() {
	case recovery.ClassificationNeedsRollback:
		return executeRecoveryRollbackEffects(
			ctx,
			plan,
			paths,
			authority,
			registryStore,
			options.beforeHostAction,
			options.Codecs,
			visibilityGate,
		)
	case recovery.ClassificationNeedsFinalize:
		if err := authority.reserveRecoverySemanticValidations(
			recoverySemanticValidationCount(plan, 0),
		); err != nil {
			return fmt.Errorf("reserve recovery semantic validation: %w", err)
		}
		if err := authority.physicalWorkBudget.ConcludeScratchCleanupNotApplicable(); err != nil {
			return fmt.Errorf("conclude absent recovery rollback stage: %w", err)
		}
		if err := authority.prepareRecoveryRemovalCleanup(plan); err != nil {
			return fmt.Errorf("prepare bounded removal cleanup: %w", err)
		}
		if err := authority.beginGeneralRecoveryExecution(); err != nil {
			return fmt.Errorf("prepare bounded recovery execution: %w", err)
		}
		if requiresOwnershipRegistry {
			registryStore, err = authority.rootedOwnershipRegistry()
			if err != nil {
				return err
			}
		}
		if err := finalizeClaimTransitionsWithAcceptance(
			ctx,
			registryStore,
			plan.ClaimTransitions(),
			recoveryClaimEffectGate(authority, visibilityGate),
			authority.acceptRecoveryOwnershipSuccessor,
		); err != nil {
			return fmt.Errorf("finalize recovery ownership claims: %w", err)
		}
		return nil
	}
	return fmt.Errorf("unsupported recovery classification %q", plan.Classification())
}

func recoverySemanticValidationCount(plan recovery.Plan, hostActionCount int) int {
	count := hostActionCount * 2 // forward attempt plus immediate compensation
	if len(plan.ClaimTransitions()) != 0 {
		count += 2 // pre-convergence validation plus typed successor acceptance
	}
	count += 2                              // stable post-effect reclassification sandwich
	count += len(plan.RemovalIntents()) * 2 // residue promotion plus cleanup
	count++                                 // active-journal retirement commit point
	return count
}

func recoveryClaimEffectGate(
	authority *mutationAuthority,
	gate visibilityEffectGate,
) visibilityEffectGate {
	return visibilityEffectGate{
		before: func(ctx context.Context) error {
			if err := gate.validateBefore(ctx); err != nil {
				return err
			}
			return authority.validateRecoverySemanticWitness(ctx)
		},
		after: gate.after,
	}
}

func recoveryPlanLoadOptions(
	options RecoveryOptions,
	authority *mutationAuthority,
) journal.PlanLoadOptions {
	return journal.PlanLoadOptions{
		Filesystem:        options.Filesystem,
		RootedCapability:  authority.rootedJournalCapability,
		Resolver:          authority.rootedJournalResolver(options.Resolver),
		OwnershipRegistry: authority.rootedOwnershipRegistryOption(),
		Codecs:            options.Codecs,
		StateCodec:        options.StateCodec,
		StateReader:       options.StateReader,
	}
}

func reloadRecoveryPlanAfterEffects(
	ctx context.Context,
	expected recovery.Plan,
	options RecoveryOptions,
	authority *mutationAuthority,
) (recovery.Plan, error) {
	before, err := authority.observeRecoverySemanticWitness(
		ctx,
		authority.semanticExecutionWorkBudget,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	current, err := options.reloadPlan(ctx, recoveryPlanLoadOptions(options, authority))
	if err != nil {
		return recovery.Plan{}, fmt.Errorf("reload recovery plan after effects: %w", err)
	}
	after, err := authority.observeRecoverySemanticWitness(
		ctx,
		authority.semanticExecutionWorkBudget,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	if err := authority.validateRecoverySemanticWitnessPair(
		before,
		after,
		"before post-effect reclassification",
	); err != nil {
		return recovery.Plan{}, err
	}
	if err := authority.validateExpectedRecoveryOwnership(ctx); err != nil {
		return recovery.Plan{}, err
	}
	if err := authority.validateJournalExecutionBasis(
		ctx,
		current,
		"after recovery effects",
	); err != nil {
		return recovery.Plan{}, err
	}
	if err := requireSameActiveJournal(expected, current, "after effects"); err != nil {
		return recovery.Plan{}, err
	}
	if current.Blocked() || current.HasErrors() {
		return recovery.Plan{}, fmt.Errorf("recovery is blocked by current evidence")
	}
	switch current.Classification() {
	case recovery.ClassificationCleanBefore, recovery.ClassificationCleanAfter:
		return current, nil
	default:
		return recovery.Plan{}, fmt.Errorf(
			"recovery effects left journal classified as %q",
			current.Classification(),
		)
	}
}

func recoveryPlanBeforeEffects(
	ctx context.Context,
	plan recovery.Plan,
	options RecoveryOptions,
	physicalAuthority mutation.PhysicalAuthoritySet,
	loadOptions journal.PlanLoadOptions,
) (recovery.Plan, error) {
	if err := validateRecoveryBeforeEffects(ctx, options, physicalAuthority); err != nil {
		return recovery.Plan{}, err
	}
	if options.reloadPlan == nil {
		return plan, nil
	}
	if options.mutationAuthority == nil {
		return recovery.Plan{}, fmt.Errorf("recovery mutation authority is unavailable")
	}
	before, err := options.mutationAuthority.observeRecoverySemanticWitness(
		ctx,
		options.mutationAuthority.generalTraversalPhase,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	current, err := options.reloadPlan(ctx, loadOptions)
	if err != nil {
		return recovery.Plan{}, fmt.Errorf("reload recovery plan before effects: %w", err)
	}
	after, err := options.mutationAuthority.observeRecoverySemanticWitness(
		ctx,
		options.mutationAuthority.generalTraversalPhase,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	if err := options.mutationAuthority.establishRecoverySemanticWitness(before, after); err != nil {
		return recovery.Plan{}, err
	}
	if err := options.mutationAuthority.validateJournalExecutionBasis(
		ctx,
		current,
		"before recovery effects",
	); err != nil {
		return recovery.Plan{}, err
	}
	if err := requireSameActiveJournal(plan, current, "before effects"); err != nil {
		return recovery.Plan{}, err
	}
	if current.Blocked() || current.HasErrors() {
		return recovery.Plan{}, fmt.Errorf("recovery is blocked by current evidence")
	}
	if !plan.SameExecutionAuthority(current) {
		return recovery.Plan{}, fmt.Errorf("recovery execution authority changed before effects")
	}
	return current, nil
}

func requireSameActiveJournal(
	expected recovery.Plan,
	current recovery.Plan,
	phase string,
) error {
	if current.OperationID() != expected.OperationID() ||
		current.OperationDir() != expected.OperationDir() {
		return fmt.Errorf("active recovery operation changed %s", phase)
	}
	expectedJournal, err := expected.JournalAuthorityFingerprint()
	if err != nil {
		return fmt.Errorf("fingerprint expected recovery journal: %w", err)
	}
	currentJournal, err := current.JournalAuthorityFingerprint()
	if err != nil {
		return fmt.Errorf("fingerprint current recovery journal: %w", err)
	}
	if currentJournal != expectedJournal {
		return fmt.Errorf("durable recovery journal changed %s", phase)
	}
	return nil
}

func executeRecoveryRollbackEffects(
	ctx context.Context,
	plan recovery.Plan,
	paths Paths,
	authority *mutationAuthority,
	registryStore ownershipmutation.RegistryStore,
	beforeHostAction func(int) error,
	codecs aggregate.CodecCatalog,
	gate visibilityEffectGate,
) error {
	actions := plan.Actions()
	hostActions := make([]recoveryHostAction, 0, len(actions))
	for _, action := range actions {
		switch action.Kind {
		case recovery.ActionKindNoOp:
			continue
		case recovery.ActionKindRestoreWrite:
			hostActions = append(hostActions, recoveryHostAction{
				Kind:                recovery.ActionKindRestoreWrite,
				Scope:               action.Scope,
				Destination:         action.Destination,
				ContentPath:         action.ContentPath,
				BackupPath:          action.BackupPath,
				BackupHash:          action.BackupHash,
				BackupKind:          action.BackupKind,
				BackupWork:          action.BackupWork,
				BeforePathMode:      action.BeforePathMode,
				BeforePathExisted:   action.BeforePathExisted,
				BeforeParentExisted: action.BeforeParentExisted,
				ExpectedAfter:       action.ExpectedAfter.Clone(),
				AggregateContract:   action.AggregateContract,
			})
		case recovery.ActionKindRestoreDelete:
			hostActions = append(hostActions, recoveryHostAction{
				Kind:                recovery.ActionKindRestoreDelete,
				Scope:               action.Scope,
				Destination:         action.Destination,
				ContentPath:         action.ContentPath,
				BeforePathMode:      action.BeforePathMode,
				BeforePathExisted:   action.BeforePathExisted,
				BeforeParentExisted: action.BeforeParentExisted,
				ExpectedAfter:       action.ExpectedAfter.Clone(),
				AggregateContract:   action.AggregateContract,
			})
		case recovery.ActionKindError:
			return fmt.Errorf("recovery plan contains error action: %s", action.Reason)
		default:
			return fmt.Errorf("unsupported recovery action %q for %q", action.Kind, action.Destination)
		}
	}
	hostActions = orderRecoveryHostActions(hostActions)
	if err := authority.reserveRecoverySemanticValidations(
		recoverySemanticValidationCount(plan, len(hostActions)),
	); err != nil {
		return fmt.Errorf("reserve recovery semantic validation: %w", err)
	}

	if authority.capturedRoot == nil {
		if err := authority.captureProjectRoot(paths, nil); err != nil {
			return err
		}
	}
	provenance, err := authority.projectAuthority.Provenance()
	if err != nil {
		return fmt.Errorf("derive recovery manifest root provenance: %w", err)
	}
	canonicalProvenance, err := recovery.NewRootProvenance(
		provenance.PhysicalRoot(),
		provenance.ObjectFingerprint(),
		provenance.MountFingerprint(),
	)
	if err != nil {
		return fmt.Errorf("canonicalize recovery manifest root provenance: %w", err)
	}
	if err := plan.MatchManifestRootProvenance(canonicalProvenance); err != nil {
		return fmt.Errorf("match recovery manifest root authority: %w", err)
	}

	if err := authority.prepareRecoveryBackups(ctx, plan.OperationDir(), hostActions); err != nil {
		return fmt.Errorf("prepare bounded recovery backups: %w", err)
	}
	if err := authority.prepareRecoveryForwardRemovals(
		ctx,
		hostActions,
		codecs,
	); err != nil {
		return fmt.Errorf("prepare bounded recovery removals: %w", err)
	}
	rollback, err := stageRecoveryRollback(ctx, authority, hostActions, codecs)
	if err != nil {
		return err
	}
	if err := authority.prepareRecoveryRemovalCleanup(plan); err != nil {
		return errors.Join(
			fmt.Errorf("prepare bounded removal cleanup: %w", err),
			rollback.cleanup(context.WithoutCancel(ctx), authority),
		)
	}
	if err := authority.beginGeneralRecoveryExecution(); err != nil {
		return errors.Join(
			fmt.Errorf("prepare bounded recovery execution: %w", err),
			rollback.cleanup(context.WithoutCancel(ctx), authority),
		)
	}
	if registryStore != nil {
		registryStore, err = authority.rootedOwnershipRegistry()
		if err != nil {
			return errors.Join(err, rollback.cleanup(context.WithoutCancel(ctx), authority))
		}
	}

	if err := executeRecoveryHostActions(
		ctx,
		authority,
		hostActions,
		rollback.entries,
		beforeHostAction,
		recoveryIntentClaimGuard(plan.ProvisionalAcquireIntents(), registryStore, authority),
		codecs,
		gate,
	); err != nil {
		rollbackErr := rollback.restore(context.WithoutCancel(ctx), authority, gate)
		cleanupErr := rollback.cleanup(context.WithoutCancel(ctx), authority)
		return recoveryRollbackFailure(err, rollbackErr, cleanupErr)
	}
	if err := ctx.Err(); err != nil {
		rollbackErr := rollback.restore(context.WithoutCancel(ctx), authority, gate)
		cleanupErr := rollback.cleanup(context.WithoutCancel(ctx), authority)
		return recoveryRollbackFailure(err, rollbackErr, cleanupErr)
	}
	if err := rollbackClaimsToBeforeWithAcceptance(
		ctx,
		registryStore,
		plan.ClaimTransitions(),
		recoveryClaimEffectGate(authority, gate),
		authority.acceptRecoveryOwnershipSuccessor,
	); err != nil {
		return errors.Join(
			fmt.Errorf("rollback recovery ownership claims: %w; recovery journal retained", err),
			rollback.cleanup(context.WithoutCancel(ctx), authority),
		)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(
			fmt.Errorf("%w; recovery writes committed; recovery journal retained", err),
			rollback.cleanup(context.WithoutCancel(ctx), authority),
		)
	}
	if err := rollback.cleanup(context.WithoutCancel(ctx), authority); err != nil {
		return fmt.Errorf("cleanup recovery rollback stage: %w; recovery journal retained", err)
	}
	return nil
}

func recoveryIntentClaimGuard(
	intents []ownership.ProvisionalAcquireIntent,
	registryStore ownershipmutation.RegistryStore,
	authority *mutationAuthority,
) recoveryHostOwnershipGuard {
	if len(intents) == 0 {
		return nil
	}
	byOutput := make(map[string]ownership.ProvisionalAcquireIntent, len(intents))
	for _, intent := range intents {
		byOutput[provisionalRecoveryOutputKey(intent.Destination().String(), string(intent.ContentPath()))] = intent
	}
	return func(ctx context.Context, action recoveryHostAction, destination mutationDestination) error {
		intent, present := byOutput[provisionalRecoveryOutputKey(action.Destination, action.ContentPath)]
		if !present {
			return nil
		}
		if registryStore == nil {
			return fmt.Errorf("ownership registry is required for provisional recovery rollback")
		}
		authorityObservation, err := mutation.ObserveDirectoryEntryAuthorityBounded(
			destination.hostPath,
			recovery.MaximumPhysicalPathDepth,
			authority.generalTraversalPhase,
		)
		if err != nil {
			return err
		}
		registry, err := registryStore.Load(ctx)
		if err != nil {
			return err
		}
		if exact, ok := authorityObservation.Exact(); ok {
			address, err := ownership.NewManagedAddress(exact, action.ContentPath)
			if err != nil {
				return err
			}
			if err := intent.AdmitAddress(address); err != nil {
				return fmt.Errorf("provisional recovery path authority changed: %w", err)
			}
			if claim, conflict := registry.Conflict(address); conflict {
				actual, _ := ownership.PresentClaim(claim)
				return &ownership.StaleClaimError{
					Address:  address,
					Expected: ownership.NoClaim(),
					Actual:   actual,
				}
			}
			return nil
		}
		provisional, ok := authorityObservation.Provisional()
		if !ok || !provisional.Equal(intent.Path()) {
			return fmt.Errorf("provisional recovery path authority changed before rollback")
		}
		if claim, conflict := registry.ProvisionalAncestorConflict(provisional); conflict {
			return fmt.Errorf(
				"provisional recovery output overlaps a durable claim owned by manifest %q",
				claim.Owner().ManifestPath(),
			)
		}
		return fmt.Errorf("provisional recovery output is no longer exactly visible before rollback")
	}
}

func provisionalRecoveryOutputKey(destination string, contentPath string) string {
	return destination + "\x00" + contentPath
}

func recoveryRollbackFailure(primary error, rollbackErr error, cleanupErr error) error {
	result := primary
	if rollbackErr != nil {
		result = rollbackError(result, rollbackErr)
	}
	if cleanupErr != nil {
		result = errors.Join(
			result,
			fmt.Errorf("cleanup recovery rollback stage: %w; recovery journal retained", cleanupErr),
		)
	}
	return result
}

func validateRecoveryBeforeEffects(
	ctx context.Context,
	options RecoveryOptions,
	authority mutation.PhysicalAuthoritySet,
) error {
	if options.ValidateBeforeEffects == nil {
		return nil
	}
	return options.ValidateBeforeEffects(ctx, authority)
}

func recoveryVisibilityGate(options RecoveryOptions) visibilityEffectGate {
	return visibilityEffectGate{
		before: options.ValidateVisibilityAuthority,
		after:  options.AcceptVisibilityChanges,
	}
}
