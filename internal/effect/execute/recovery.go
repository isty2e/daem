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
	"github.com/isty2e/daem/internal/operationplan"
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
	afterAuthorityClose         func() error
}

// ExecuteRecoveryPlanWithOptions applies a journal-derived recovery plan after
// invoking the workflow's final authority validation.
func ExecuteRecoveryPlanWithOptions(
	ctx context.Context,
	plan recovery.Plan,
	paths Paths,
	options RecoveryOptions,
) (resultErr error) {
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
	if options.mutationAuthority != nil {
		return fmt.Errorf("standalone active recovery cannot borrow mutation authority")
	}
	authority, err := newRecoveryMutationAuthority(
		paths,
		plan,
		options.Resolver,
		options.Filesystem,
		options.OwnershipRegistryBinder,
	)
	if err != nil {
		return err
	}
	options.mutationAuthority = authority
	execution, err := newActiveRecoveryExecutionForPlan(
		plan,
		activeRecoveryCallerStandalone,
		options.beforeRetirement != nil,
	)
	if err != nil {
		return errors.Join(err, authority.close())
	}
	defer func() {
		closeAuthority := authority.close
		if options.afterAuthorityClose != nil {
			closeAuthority = func() error {
				return errors.Join(authority.close(), options.afterAuthorityClose())
			}
		}
		resultErr = execution.finish(resultErr, closeAuthority)
	}()

	prefix := activeRecoveryStandalonePrefixSteps()
	if err := execution.runTerminalStep(prefix[0], func() error {
		return authority.bindRecoveryJournal(paths.ManifestRoot, plan.OperationDir())
	}); err != nil {
		return err
	}
	if err := execution.runTerminalStep(prefix[1], func() error {
		return authority.bindRecoveryStatefileSemanticEntry(paths.StatefilePath)
	}); err != nil {
		return err
	}
	if err := execution.runTerminalStep(prefix[2], func() error {
		journalFingerprint, err := plan.JournalAuthorityFingerprint()
		if err != nil {
			return err
		}
		return authority.setJournalExecutionBasis(
			journalFingerprint,
			options.ActiveJournalAuthority,
		)
	}); err != nil {
		return err
	}
	if err := execution.runTerminalStep(prefix[3], func() error {
		return authority.validateJournalExecutionBasis(
			ctx,
			plan,
			"before recovery execution",
		)
	}); err != nil {
		return err
	}
	if err := execution.runTerminalStep(prefix[4], func() error {
		if err := authority.bindRemovalIntents(plan); err != nil {
			return fmt.Errorf("bind recovery removal authority: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := executeRecoveryPlanEffects(ctx, plan, paths, options, execution); err != nil {
		return err
	}

	if err := execution.runTerminalStep(
		activeRecoveryStep{
			id:   "active-recovery/outer/validate-project-before-retirement",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			if err := authority.validateProjectSelection(paths.ManifestRoot); err != nil {
				return fmt.Errorf("%w; recovery effects committed; recovery journal retained", err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	visibilityGate := recoveryVisibilityGate(options)
	if err := execution.runTerminalStep(
		activeRecoveryStep{
			id:   "active-recovery/outer/validate-visibility-before-retirement",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			if err := visibilityGate.validateBefore(ctx); err != nil {
				return fmt.Errorf("%w; recovery effects committed; recovery journal retained", err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	retirementPlan := recovery.Plan{}
	if err := execution.runTerminalStep(
		activeRecoveryStep{
			id:   "active-recovery/outer/reload-after-effects",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			var err error
			retirementPlan, err = reloadRecoveryPlanAfterEffects(ctx, plan, options, authority)
			if err != nil {
				return fmt.Errorf("%w; recovery effects committed; recovery journal retained", err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	if options.beforeRetirement != nil {
		if err := execution.runTerminalStep(
			activeRecoveryStep{
				id:   "active-recovery/outer/before-retirement",
				kind: operationplan.EffectStepObservation,
			},
			func() error {
				if err := options.beforeRetirement(); err != nil {
					return fmt.Errorf("before recovery journal retirement: %w", err)
				}
				return nil
			},
		); err != nil {
			return err
		}
	}
	if err := authority.retireActiveJournal(ctx, retirementPlan, execution); err != nil {
		return fmt.Errorf("retire recovery journal: %w", err)
	}
	if err := execution.runTerminalStep(
		activeRecoveryStep{
			id:   "active-recovery/outer/accept-visibility",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			if err := visibilityGate.acceptAfter(ctx); err != nil {
				return fmt.Errorf("%w; recovery effects committed; recovery journal retired", err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	return execution.runTerminalStep(
		activeRecoveryStep{
			id:   "active-recovery/outer/validate-project-after-retirement",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			if err := authority.validateProjectSelection(paths.ManifestRoot); err != nil {
				return fmt.Errorf("%w; recovery effects committed; recovery journal retired", err)
			}
			return nil
		},
	)
}

func executeRecoveryPlanEffects(
	ctx context.Context,
	plan recovery.Plan,
	paths Paths,
	options RecoveryOptions,
	execution *activeRecoveryExecution,
) error {
	if execution == nil {
		return fmt.Errorf("active recovery execution is unavailable")
	}
	if err := execution.requirePlan(plan); err != nil {
		return err
	}
	authority := options.mutationAuthority
	if authority == nil {
		return fmt.Errorf("recovery mutation authority is unavailable")
	}
	requiresOwnershipRegistry := len(plan.ClaimTransitions()) != 0 || len(plan.ProvisionalAcquireIntents()) != 0
	var registryStore ownershipmutation.RegistryStore
	var physicalAuthority mutation.PhysicalAuthoritySet
	if err := execution.runTerminalStep(
		activeRecoveryStep{
			id:   "active-recovery/prepare-mutation-authority",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			if authority.ownershipRegistryBinder == nil {
				authority.ownershipRegistryBinder = options.OwnershipRegistryBinder
			}
			if requiresOwnershipRegistry &&
				!authority.hasOwnershipRegistry &&
				authority.ownershipRegistryBinder == nil {
				return fmt.Errorf("recovery ownership registry binder is required")
			}
			if err := requireRecoveryGlobalBindings(authority, plan.GuardedActions()); err != nil {
				return err
			}
			if !authority.statefileSemanticEntry.valid() {
				if err := authority.bindRecoveryStatefileSemanticEntry(paths.StatefilePath); err != nil {
					return err
				}
			}
			if requiresOwnershipRegistry {
				if err := authority.bindOwnershipRegistry(paths.OwnershipRegistryPath); err != nil {
					return err
				}
				var err error
				registryStore, err = authority.rootedOwnershipRegistry()
				if err != nil {
					return err
				}
			}
			var err error
			physicalAuthority, err = authority.physicalAuthority()
			return err
		},
	); err != nil {
		return err
	}

	current := recovery.Plan{}
	if err := execution.runTerminalStep(
		activeRecoveryStep{
			id:   "active-recovery/reload-before-effects",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			var err error
			current, err = recoveryPlanBeforeEffects(
				ctx,
				plan,
				options,
				physicalAuthority,
				recoveryPlanLoadOptions(options, authority),
			)
			return err
		},
	); err != nil {
		return err
	}
	plan = current
	if err := execution.runTerminalStep(
		activeRecoveryStep{
			id:   "active-recovery/bind-reloaded-removal-authority",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			if err := authority.bindRemovalIntents(plan); err != nil {
				return fmt.Errorf("bind reloaded recovery removal authority: %w", err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	if err := execution.runTerminalStep(
		activeRecoveryStep{
			id:   "active-recovery/prepare-journal-retirement",
			kind: operationplan.EffectStepPersistence,
		},
		func() error {
			if err := authority.prepareActiveJournalRetirement(
				ctx,
				paths,
				plan,
				options.StateCodec,
			); err != nil {
				return fmt.Errorf("prepare bounded journal retirement: %w", err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	visibilityGate := recoveryVisibilityGate(options)

	switch plan.Classification() {
	case recovery.ClassificationCleanBefore, recovery.ClassificationCleanAfter:
		steps := activeRecoveryCleanPreparationSteps()
		if err := execution.runTerminalStep(steps[0], func() error {
			if err := authority.reserveRecoverySemanticValidations(
				recoverySemanticValidationCount(plan, 0),
			); err != nil {
				return fmt.Errorf("reserve recovery semantic validation: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}
		if err := execution.runTerminalStep(steps[1], func() error {
			if err := authority.physicalWorkBudget.ConcludeScratchCleanupNotApplicable(); err != nil {
				return fmt.Errorf("conclude absent recovery rollback stage: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}
		if err := execution.runTerminalStep(steps[2], func() error {
			if err := authority.prepareRecoveryRemovalCleanup(plan); err != nil {
				return fmt.Errorf("prepare bounded removal cleanup: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}
		return execution.runTerminalStep(steps[3], authority.beginGeneralRecoveryExecution)
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
			execution,
		)
	case recovery.ClassificationNeedsFinalize:
		steps := activeRecoveryFinalizePreparationSteps()
		if err := execution.runTerminalStep(steps[0], func() error {
			if err := authority.reserveRecoverySemanticValidations(
				recoverySemanticValidationCount(plan, 0),
			); err != nil {
				return fmt.Errorf("reserve recovery semantic validation: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}
		if err := execution.runTerminalStep(steps[1], func() error {
			if err := authority.physicalWorkBudget.ConcludeScratchCleanupNotApplicable(); err != nil {
				return fmt.Errorf("conclude absent recovery rollback stage: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}
		if err := execution.runTerminalStep(steps[2], func() error {
			if err := authority.prepareRecoveryRemovalCleanup(plan); err != nil {
				return fmt.Errorf("prepare bounded removal cleanup: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}
		if err := execution.runTerminalStep(steps[3], func() error {
			if err := authority.beginGeneralRecoveryExecution(); err != nil {
				return fmt.Errorf("prepare bounded recovery execution: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}
		claimKind := operationplan.EffectStepNoOp
		if len(plan.ClaimTransitions()) != 0 {
			claimKind = operationplan.EffectStepPersistence
		}
		return execution.runTerminalStep(
			activeRecoveryStep{id: "active-recovery/finalize/claims", kind: claimKind},
			func() error {
				if requiresOwnershipRegistry {
					var err error
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
			},
		)
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

func recoveryHostActionsForPlan(plan recovery.Plan) ([]recoveryHostAction, error) {
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
			return nil, fmt.Errorf("recovery plan contains error action: %s", action.Reason)
		default:
			return nil, fmt.Errorf("unsupported recovery action %q for %q", action.Kind, action.Destination)
		}
	}
	return orderRecoveryHostActions(hostActions), nil
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
	execution *activeRecoveryExecution,
) error {
	steps := activeRecoveryRollbackPreparationSteps()
	var hostActions []recoveryHostAction
	if err := execution.runTerminalStep(steps[0], func() error {
		var err error
		hostActions, err = recoveryHostActionsForPlan(plan)
		return err
	}); err != nil {
		return err
	}
	if err := execution.runTerminalStep(steps[1], func() error {
		if err := authority.reserveRecoverySemanticValidations(
			recoverySemanticValidationCount(plan, len(hostActions)),
		); err != nil {
			return fmt.Errorf("reserve recovery semantic validation: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := execution.runTerminalStep(steps[2], func() error {
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
		return nil
	}); err != nil {
		return err
	}
	if err := execution.runTerminalStep(steps[3], func() error {
		if err := authority.prepareRecoveryBackups(ctx, plan.OperationDir(), hostActions); err != nil {
			return fmt.Errorf("prepare bounded recovery backups: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := execution.runTerminalStep(steps[4], func() error {
		if err := authority.prepareRecoveryForwardRemovals(ctx, hostActions, codecs); err != nil {
			return fmt.Errorf("prepare bounded recovery removals: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	var rollback hostRollback
	if err := execution.runTerminalStep(steps[5], func() error {
		var err error
		rollback, err = stageRecoveryRollback(ctx, authority, hostActions, codecs)
		return err
	}); err != nil {
		return err
	}

	prepareCleanupStep := activeRecoveryStep{
		id:   "active-recovery/rollback/prepare-removal-cleanup",
		kind: operationplan.EffectStepObservation,
	}
	if err := execution.runBranchingStep(prepareCleanupStep, func() error {
		if err := authority.prepareRecoveryRemovalCleanup(plan); err != nil {
			return fmt.Errorf("prepare bounded removal cleanup: %w", err)
		}
		return nil
	}); err != nil {
		cleanupErr := execution.runFailureCleanup(
			"active-recovery/rollback/prepare-removal-cleanup-failure",
			func() error { return rollback.cleanup(context.WithoutCancel(ctx), authority) },
		)
		return errors.Join(err, cleanupErr)
	}

	beginExecutionStep := activeRecoveryStep{
		id:   "active-recovery/rollback/begin-general-execution",
		kind: operationplan.EffectStepObservation,
	}
	if err := execution.runBranchingStep(
		beginExecutionStep,
		authority.beginGeneralRecoveryExecution,
	); err != nil {
		cleanupErr := execution.runFailureCleanup(
			"active-recovery/rollback/begin-general-execution-failure",
			func() error { return rollback.cleanup(context.WithoutCancel(ctx), authority) },
		)
		return errors.Join(
			fmt.Errorf("prepare bounded recovery execution: %w", err),
			cleanupErr,
		)
	}

	registryKind := operationplan.EffectStepNoOp
	if registryStore != nil {
		registryKind = operationplan.EffectStepObservation
	}
	rebindStep := activeRecoveryStep{
		id:   "active-recovery/rollback/rebind-ownership-registry",
		kind: registryKind,
	}
	if err := execution.runBranchingStep(rebindStep, func() error {
		if registryStore == nil {
			return nil
		}
		var err error
		registryStore, err = authority.rootedOwnershipRegistry()
		return err
	}); err != nil {
		cleanupErr := execution.runFailureCleanup(
			"active-recovery/rollback/rebind-ownership-failure",
			func() error { return rollback.cleanup(context.WithoutCancel(ctx), authority) },
		)
		return errors.Join(err, cleanupErr)
	}

	var hostPlan recoveryHostExecutionPlan
	prepareHostStep := activeRecoveryStep{
		id:   "active-recovery/rollback/prepare-host-execution",
		kind: operationplan.EffectStepObservation,
	}
	if err := execution.runBranchingStep(prepareHostStep, func() error {
		var err error
		hostPlan, err = prepareRecoveryHostExecutionPlan(authority, hostActions)
		return err
	}); err != nil {
		return executeActiveRecoveryRollbackCompensation(
			ctx,
			authority,
			gate,
			execution,
			&rollback,
			"active-recovery/rollback/prepare-host-execution-failure",
			err,
		)
	}
	if err := execution.beginHostBatch(hostPlan.visitOrder); err != nil {
		return errors.Join(err, rollback.cleanup(context.WithoutCancel(ctx), authority))
	}
	guardedBeforeAction := func(index int) error {
		if err := execution.visitHostAction(index); err != nil {
			return err
		}
		if beforeHostAction != nil {
			return beforeHostAction(index)
		}
		return nil
	}
	hostErr := executeRecoveryHostActionPlan(
		ctx,
		authority,
		hostActions,
		rollback.entries,
		hostPlan,
		guardedBeforeAction,
		recoveryIntentClaimGuard(plan.ProvisionalAcquireIntents(), registryStore, authority),
		codecs,
		gate,
	)
	hostErr = execution.settleHostBatch(hostErr)
	if hostErr != nil {
		return executeActiveRecoveryRollbackCompensation(
			ctx,
			authority,
			gate,
			execution,
			&rollback,
			"active-recovery/rollback/host-failure",
			hostErr,
		)
	}

	postHostStep := activeRecoveryStep{
		id:   "active-recovery/rollback/validate-after-host-actions",
		kind: operationplan.EffectStepObservation,
	}
	if err := execution.runBranchingStep(postHostStep, ctx.Err); err != nil {
		return executeActiveRecoveryRollbackCompensation(
			ctx,
			authority,
			gate,
			execution,
			&rollback,
			"active-recovery/rollback/post-host-cancellation",
			err,
		)
	}

	claimKind := operationplan.EffectStepNoOp
	if len(plan.ClaimTransitions()) != 0 {
		claimKind = operationplan.EffectStepPersistence
	}
	claimStep := activeRecoveryStep{
		id:   "active-recovery/rollback/claims",
		kind: claimKind,
	}
	if err := execution.runBranchingStep(claimStep, func() error {
		if err := rollbackClaimsToBeforeWithAcceptance(
			ctx,
			registryStore,
			plan.ClaimTransitions(),
			recoveryClaimEffectGate(authority, gate),
			authority.acceptRecoveryOwnershipSuccessor,
		); err != nil {
			return fmt.Errorf("rollback recovery ownership claims: %w; recovery journal retained", err)
		}
		return nil
	}); err != nil {
		cleanupErr := execution.runFailureCleanup(
			"active-recovery/rollback/claim-failure",
			func() error { return rollback.cleanup(context.WithoutCancel(ctx), authority) },
		)
		return errors.Join(err, cleanupErr)
	}

	postClaimStep := activeRecoveryStep{
		id:   "active-recovery/rollback/validate-after-claims",
		kind: operationplan.EffectStepObservation,
	}
	if err := execution.runBranchingStep(postClaimStep, func() error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w; recovery writes committed; recovery journal retained", err)
		}
		return nil
	}); err != nil {
		cleanupErr := execution.runFailureCleanup(
			"active-recovery/rollback/post-claim-cancellation",
			func() error { return rollback.cleanup(context.WithoutCancel(ctx), authority) },
		)
		return errors.Join(err, cleanupErr)
	}

	return execution.runTerminalStep(
		activeRecoveryStep{
			id:   "active-recovery/rollback/cleanup-after-success",
			kind: operationplan.EffectStepCleanup,
		},
		func() error {
			if err := rollback.cleanup(context.WithoutCancel(ctx), authority); err != nil {
				return fmt.Errorf("cleanup recovery rollback stage: %w; recovery journal retained", err)
			}
			return nil
		},
	)
}

func executeActiveRecoveryRollbackCompensation(
	ctx context.Context,
	authority *mutationAuthority,
	gate visibilityEffectGate,
	execution *activeRecoveryExecution,
	rollback *hostRollback,
	prefix string,
	primary error,
) error {
	rollbackErr := execution.runContinuingOutcomeStep(
		activeRecoveryStep{id: prefix + "/restore", kind: operationplan.EffectStepCompensation},
		prefix+"/restore",
		func() error { return rollback.restore(context.WithoutCancel(ctx), authority, gate) },
	)
	cleanupErr := execution.runFailureCleanup(
		prefix,
		func() error { return rollback.cleanup(context.WithoutCancel(ctx), authority) },
	)
	return recoveryRollbackFailure(primary, rollbackErr, cleanupErr)
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
