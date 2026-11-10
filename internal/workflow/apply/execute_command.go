package apply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/effect/execute/executionevent"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/execute/nativeoperation"
	"github.com/isty2e/daem/internal/effect/execute/registrysettlement"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type ExecuteOptions struct {
	NativeFilesystem               mutationfs.Store
	ExecuteEvents                  executionevent.Sink
	RelationObservations           *relationobserve.Batch
	HostRouteExecutor              subprocess.CommandExecutor
	HostRouteObserver              HostRouteObserver
	CarrierRemovalAdapter          executehostroute.RemovalAdapter
	CarrierRemovalObserver         CarrierRemovalObserver
	CarrierRemovalBaselineObserver CarrierRemovalBaselineObserver
	DelegateExecutor               delegate.Executor
	RelationOrderRiskAuthorizer    RelationOrderRiskAuthorizer
	PlanWasDisclosed               bool
}

type executeDependencies struct {
	recoveryProvenancePreflight recoveryProvenancePreflight
	reserveForwardEffects       func(
		recoverygate.EffectAuthority,
		operationplan.EffectStructure,
		operationplan.Demand,
	) (*recoverygate.ForwardEffectAuthority, error)
}

func ExecuteWithOptions(
	ctx context.Context,
	prepared *PreparedWrite,
	options ExecuteOptions,
) (CommandResult, error) {
	return executeWithDependencies(ctx, prepared, options, executeDependencies{})
}

func executeWithDependencies(
	ctx context.Context,
	prepared *PreparedWrite,
	options ExecuteOptions,
	dependencies executeDependencies,
) (result CommandResult, returnErr error) {
	execution, err := prepared.beginExecution()
	if err != nil {
		return CommandResult{}, err
	}
	defer func() {
		if err := closeCommandPlan(&execution.planned); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()

	planned := execution.planned
	executionAttempted := false
	uncompensatedEffectsAttempted := false
	disclose := func(planned commandPlan) CommandResult {
		result := cloneCommandResult(planned.result)
		result.ExecutionAttempted = executionAttempted
		result.UncompensatedEffectsAttempted = uncompensatedEffectsAttempted
		return result
	}
	markExecutionAttempted := func() { executionAttempted = true }
	if err := rejectBlockedRelationActions(planned.assessment.Reconciliation); err != nil {
		return disclose(planned), err
	}
	if err := rejectBlockedCarrierAdoptions(planned.assessment.Reconciliation); err != nil {
		return disclose(planned), err
	}
	if err := rejectBlockedCarrierAbsences(planned.assessment.Reconciliation.CarrierAbsences()); err != nil {
		return disclose(planned), err
	}
	if ctx == nil {
		return disclose(planned), fmt.Errorf("apply context is required")
	}
	if err := ctx.Err(); err != nil {
		return disclose(planned), err
	}
	visibleFingerprint, err := applyOperationFingerprint(planned, execution.operationContext)
	if err != nil || !execution.operationEvidence.Equal(visibleFingerprint) {
		return disclose(planned), staleApplyError(options.PlanWasDisclosed, err)
	}
	visibleAuthority, err := buildApplyAuthorityEvidence(ctx, planned)
	if err != nil || !execution.authorityEvidence.authorityFingerprint.Equal(visibleAuthority.authorityFingerprint) {
		return disclose(planned), staleApplyError(options.PlanWasDisclosed, err)
	}
	executionGuard := newApplyExecutionGuard(
		execution.declarationRevisions,
		options.PlanWasDisclosed,
	)
	if err := executionGuard.requireDeclarationsCurrent(
		ctx,
		"prepared apply plan",
	); err != nil {
		return disclose(planned), err
	}
	if err := preflightMCPEnvironmentSources(
		ctx,
		planned.context.RuntimeEnvironment,
		planned.context.Selection,
		execution.request.environmentPresent,
	); err != nil {
		return disclose(planned), err
	}

	store, err := mutation.NewStore(planned.context.Paths.DataDir)
	if err != nil {
		return disclose(planned), err
	}
	effectPaths, err := planned.context.Paths.WithDataDir(store.DataDir())
	if err != nil {
		return disclose(planned), err
	}
	leases, err := store.Acquire(ctx, execution.authorityEvidence.domains...)
	if err != nil {
		return disclose(planned), err
	}
	defer func() {
		if err := leases.Release(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return disclose(planned), err
	} else if !matches {
		return disclose(planned), staleApplyError(options.PlanWasDisclosed, nil)
	}
	if err := planned.barrier.Validate(ctx); err != nil {
		return disclose(planned), staleApplyError(
			options.PlanWasDisclosed,
			fmt.Errorf("validate planned recovery barrier: %w", err),
		)
	}
	if _, err := projectRootFingerprint(planned); err != nil {
		return disclose(planned), staleApplyError(options.PlanWasDisclosed, err)
	}
	firstEffectRevisions, err := mutation.CaptureRevisionSet(
		ctx,
		execution.authorityEvidence.firstEffectRevisions...,
	)
	if err != nil {
		return disclose(planned), err
	}

	currentInput := cloneCommandInput(execution.request)
	if options.RelationObservations != nil {
		currentInput.RelationObservations = options.RelationObservations
	}
	if err := executionGuard.requireDeclarationsCurrent(
		ctx,
		"initial apply replan",
	); err != nil {
		return disclose(planned), err
	}
	assessor := readinessAssessor(previewReadiness)
	if planned.nativePlan != nil {
		assessor = retainedReadiness
	}
	current, err := planReadinessAtPathsWithAssessor(
		ctx,
		currentInput,
		execution.operationContext,
		planned.context.Paths,
		&planned.barrier,
		nil, assessor,
	)
	defer func() { current.projectRoot = nil; returnErr = errors.Join(returnErr, closeCommandPlan(&current)) }()
	if err != nil {
		return disclose(planned), staleApplyError(options.PlanWasDisclosed, err)
	}
	if err := rejectLocalSourceMutationOverlap(current); err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	if err := rejectUnsupportedCommandPlan(current); err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	if err := rejectBlockedRelationActions(current.assessment.Reconciliation); err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	if err := rejectBlockedCarrierAdoptions(current.assessment.Reconciliation); err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	if err := rejectBlockedCarrierAbsences(current.assessment.Reconciliation.CarrierAbsences()); err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	current.projectRoot = planned.projectRoot
	currentOperation, err := applyOperationFingerprint(current, execution.operationContext)
	if err != nil {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, err)
	}
	currentAuthority, err := buildApplyAuthorityEvidence(ctx, current)
	if err != nil {
		return disclose(current), fmt.Errorf("derive current apply mutation authority: %w", err)
	}
	if !execution.operationEvidence.Equal(currentOperation) ||
		!execution.authorityEvidence.authorityFingerprint.Equal(currentAuthority.authorityFingerprint) {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, nil)
	}
	if matches, err := firstEffectRevisions.MatchesCurrent(ctx); err != nil {
		return disclose(current), err
	} else if !matches {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, nil)
	}
	if err := executionGuard.requireDeclarationsCurrent(
		ctx,
		"initial apply revalidation",
	); err != nil {
		return disclose(current), err
	}
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return disclose(current), err
	} else if !matches {
		return disclose(current), staleApplyError(options.PlanWasDisclosed, nil)
	}
	if err := preflightMCPEnvironmentSources(
		ctx,
		current.context.RuntimeEnvironment,
		current.context.Selection,
		execution.request.environmentPresent,
	); err != nil {
		return disclose(current), err
	}
	providerActions, err := prepareMCPProviderPrerequisiteActions(
		current,
		dependencies.recoveryProvenancePreflight,
	)
	if err != nil {
		return disclose(current), err
	}
	carrierRemovalAdapter := options.CarrierRemovalAdapter
	if carrierRemovalAdapter == nil {
		carrierRemovalAdapter = executehostroute.BuildDelegatedRemovalAttempt
	}
	nativeInvocations, err := prepareNativeInvocations(ctx, current, leases, currentAuthority.metadataRequests, carrierRemovalAdapter, options.DelegateExecutor)
	if err != nil {
		return disclose(current), err
	}
	stateDirPlan, err := stateDirEffectPlanFor(current, providerActions)
	if err != nil {
		return disclose(current), err
	}
	replanInput := currentInput
	if stateDirPlan.schedule.managedSuffix != nil {
		replanInput.persistenceBudget = physicalwork.NewCaptureBudget(nil)
	}
	delegateReplanInput := currentInput
	if stateDirPlan.schedule.delegateSuffix != nil {
		delegateReplanInput.persistenceBudget = physicalwork.NewCaptureBudget(nil)
	}
	reserveForwardEffects := dependencies.reserveForwardEffects
	if reserveForwardEffects == nil {
		reserveForwardEffects = func(
			authority recoverygate.EffectAuthority,
			structure operationplan.EffectStructure,
			demand operationplan.Demand,
		) (*recoverygate.ForwardEffectAuthority, error) {
			return authority.ReserveForwardEffectStructure(structure, demand)
		}
	}
	forwardAuthority, err := reserveForwardEffects(
		current.barrier,
		stateDirPlan.schedule.full,
		stateDirPlan.demand,
	)
	if err != nil {
		return disclose(current), err
	}
	var statefileAuthority *statefileEffectAuthority
	if !stateDirPlan.statefile.empty() {
		descendant, takeErr := forwardAuthority.TakeDescendant()
		if takeErr != nil {
			return disclose(current), takeErr
		}
		statefileAuthority, err = newStatefileEffectAuthorityFromReservation(
			stateDirPlan.statefile,
			transactionStatefileEffectReservation{reservation: descendant},
		)
		if err != nil {
			return disclose(current), err
		}
		defer func() {
			if closeErr := statefileAuthority.Close(); closeErr != nil {
				returnErr = errors.Join(returnErr, closeErr)
			}
		}()
	}

	// The captured revisions describe the pre-execution world. StateDir creation
	// is the first authorized effect; every peer authority is checked before and
	// after it. Later phases retain lease and project-root checks, while direct
	// file mutations add effect-local CAS.
	stateDirBoundaryValidated := false
	revisionsValidated := false
	validatePeerAuthority := func(
		ctx context.Context,
		validateLeases func(context.Context) (bool, error),
	) error {
		if err := executionGuard.requireDeclarationsCurrent(
			ctx,
			"apply effect validation",
		); err != nil {
			return err
		}
		if current.nativePlan != nil {
			for _, config := range current.context.RuntimeEnvironment.HostConfigs() {
				if !current.context.Selection.Includes(config.Target()) {
					continue
				}
				if _, err := current.context.SourceEpoch.NativeSource(ctx, config); err != nil {
					return err
				}
			}
		}
		if !revisionsValidated {
			matches, err := firstEffectRevisions.MatchesCurrent(ctx)
			if err != nil {
				return err
			}
			if !matches {
				return staleApplyError(options.PlanWasDisclosed, nil)
			}
		}
		matches, err := validateLeases(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return staleApplyError(options.PlanWasDisclosed, nil)
		}
		if _, err := projectRootFingerprint(current); err != nil {
			return staleApplyError(options.PlanWasDisclosed, err)
		}
		return nil
	}
	validateBeforeEffectsWith := func(ctx context.Context, validateLeases func(context.Context) (bool, error)) error {
		if stateDirBoundaryValidated {
			if err := validatePeerAuthority(ctx, validateLeases); err != nil {
				return err
			}
			if err := forwardAuthority.ValidateStateDir(ctx); err != nil {
				return err
			}
			revisionsValidated = true
			return nil
		}
		created, err := forwardAuthority.EnsureStateDirForEffect(
			ctx,
			func(ctx context.Context) error {
				return validatePeerAuthority(ctx, validateLeases)
			},
		)
		if created {
			markExecutionAttempted()
		}
		if err != nil {
			return fmt.Errorf("establish recovery barrier before apply effect: %w", err)
		}
		stateDirBoundaryValidated, revisionsValidated = true, true
		return nil
	}
	validateBeforeEffects := func(ctx context.Context, authority mutation.PhysicalAuthoritySet) error {
		return validateBeforeEffectsWith(ctx, func(ctx context.Context) (bool, error) {
			matches, err := leases.DomainsMatchCurrent(ctx)
			if err != nil || !matches {
				return matches, err
			}
			covered, err := leases.CoversPhysicalAuthority(authority)
			if err != nil || !covered {
				return covered, err
			}
			covered, err = leases.CoversPhysicalAuthority(currentAuthority.documentWrites)
			if err != nil || !covered {
				return covered, err
			}
			return true, currentAuthority.documentView.RequireProtectionClear()
		})
	}
	validatePhysicalAuthorityRequests := func(ctx context.Context, requests []mutation.PhysicalAuthorityRequest) error {
		return validateBeforeEffectsWith(ctx, func(ctx context.Context) (bool, error) {
			matches, err := leases.MatchCurrentPhysicalRequests(ctx, requests...)
			if err != nil || !matches {
				return matches, err
			}
			covered, err := leases.CoversPhysicalAuthority(currentAuthority.documentWrites)
			if err != nil || !covered {
				return covered, err
			}
			return true, currentAuthority.documentView.RequireProtectionClear()
		})
	}
	validateCompensationAuthority := func(ctx context.Context) error {
		matches, err := leases.VisibilityAuthorityMatchesCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return staleApplyError(options.PlanWasDisclosed, nil)
		}
		if _, err := projectRootFingerprint(current); err != nil {
			return staleApplyError(options.PlanWasDisclosed, err)
		}
		return nil
	}
	acceptCompensationChanges := func(ctx context.Context) error {
		accepted, err := leases.AcceptVisibilityChanges(ctx)
		if err != nil {
			return err
		}
		if !accepted {
			return staleApplyError(options.PlanWasDisclosed, nil)
		}
		if _, err := projectRootFingerprint(current); err != nil {
			return staleApplyError(options.PlanWasDisclosed, err)
		}
		return nil
	}
	acceptVisibilityChanges := func(ctx context.Context) error {
		if err := acceptCompensationChanges(ctx); err != nil {
			return err
		}
		return executionGuard.requireDeclarationsCurrent(
			ctx,
			"apply visibility acceptance",
		)
	}

	hostRouteObserver := options.HostRouteObserver
	if hostRouteObserver == nil && !current.context.RelationObservationsExplicit {
		hostRouteObserver = passiveHostRouteObserver(
			current.context.Paths,
			current.context.Lockfile,
			current.context.Selection,
		)
	}
	carrierRemovalObserver := options.CarrierRemovalObserver
	if carrierRemovalObserver == nil {
		carrierRemovalObserver = passiveCarrierRemovalObserver(current.context.Paths)
	}
	carrierRemovalBaselineObserver := options.CarrierRemovalBaselineObserver
	if carrierRemovalBaselineObserver == nil {
		carrierRemovalBaselineObserver = passiveCarrierRemovalBaselineObserver(current.context.Paths)
	}
	executionOptions := runOptions{
		ExecuteEvents:                  options.ExecuteEvents,
		HostRouteExecutor:              options.HostRouteExecutor,
		HostRouteObserver:              hostRouteObserver,
		CarrierRemovalAdapter:          carrierRemovalAdapter,
		CarrierRemovalObserver:         carrierRemovalObserver,
		CarrierRemovalBaselineObserver: carrierRemovalBaselineObserver,
		DelegateExecutor:               options.DelegateExecutor,
		RelationOrderRiskAuthorizer:    options.RelationOrderRiskAuthorizer,
		orderRiskBaseline: newRelationOrderRiskBaseline(
			planned.assessment.Reconciliation.RelationOrders(),
		),
		executionGuard:                    executionGuard,
		validateBeforeEffects:             validateBeforeEffects,
		validatePhysicalAuthorityRequests: validatePhysicalAuthorityRequests,
		validateRecoveryBarrier:           forwardAuthority.Validate,
		validateStateDir: func(ctx context.Context) error {
			return forwardAuthority.ValidateStateDir(ctx)
		},
		statefileAuthority:            statefileAuthority,
		applyEffectPlan:               &stateDirPlan.schedule.effectPlan,
		preparedContinuation:          stateDirPlan.schedule.continuation,
		currentContinuation:           stateDirPlan.schedule.continuation,
		requireContinuation:           true,
		acceptVisibilityChanges:       acceptVisibilityChanges,
		validateCompensationAuthority: validateCompensationAuthority,
		acceptCompensationChanges:     acceptCompensationChanges,
		projectRoot:                   planned.projectRoot,
		markExecutionAttempted:        markExecutionAttempted,
		recoveryProvenancePreflight:   dependencies.recoveryProvenancePreflight,
	}

	var providerPhase providerPhaseExecution
	if current.nativePlan != nil {
		native, err := nativePrefixApplyInputFor(current)
		if err != nil {
			return disclose(current), err
		}
		if options.NativeFilesystem != nil {
			native.ApplyInput.Filesystem = options.NativeFilesystem
		}
		executionOptions.nativeInput = &native
		executionOptions.persistenceCodec = current.nativePlan.stateCodec
		executionOptions.nativeInvocations = nativeInvocations
		if !current.nativePlan.opaque {
			executionOptions.nativeOrderChecks = current.nativePlan.orders
		}
		if current.nativePlan.opaque {
			originalOwnership := current.assessment.Ownership
			requalificationInput := func() applyRequalificationInput {
				return applyRequalificationInput{
					request: replanInput, operationContext: execution.operationContext,
					current: current, currentAuthority: currentAuthority,
					originalOwnership: originalOwnership, schedule: stateDirPlan.schedule,
					guard: executionGuard, leases: leases, validateBarrier: forwardAuthority.Validate,
					stateDirBoundaryValidated: stateDirBoundaryValidated,
				}
			}
			prefixRegistry := native.Registry
			var prerequisiteSettlement *registrysettlement.PersistenceSnapshot
			prefixRegistryScope, err := native.Documents.Evidence().RegistryScope()
			if err != nil {
				return disclose(current), err
			}
			executionOptions.afterNativeCore = func(ctx context.Context, applied nativeoperation.ApplyResult) (_ readiness.Assessment, _ applyContinuationPlan, returnErr error) {
				if !applied.ExecutionAttempted {
					return current.assessment, stateDirPlan.schedule.continuation, nil
				}
				updated, remainder, err := requalifyApplyPlan(ctx, requalificationInput(), requalificationAfterNativePrefix, func(ctx context.Context, candidate *commandPlan) (requalifiedNativePrefix, error) {
					refreshed := *candidate
					fresh, err := nativeBeforeDelegateInput(refreshed)
					if err != nil {
						return requalifiedNativePrefix{}, err
					}
					settled, present := applied.NativeSettlement()
					if !present {
						return requalifiedNativePrefix{}, fmt.Errorf("native prefix has no observed settlement")
					}
					if matches, err := settled.Matches(refreshed.assessment.CurrentState, fresh.Registry); err != nil || !matches {
						return requalifiedNativePrefix{}, staleApplyError(options.PlanWasDisclosed, err)
					}

					if stateDirPlan.schedule.managedSuffix != nil {
						if _, err := stateDirPlan.schedule.managedSuffix.Bind(fresh); err != nil {
							return requalifiedNativePrefix{}, staleApplyError(options.PlanWasDisclosed, err)
						}
					} else {
						gates, err := nativeoperation.MaximumNativeForwardEffectValidationCount(fresh)
						if err != nil || gates != 0 {
							return requalifiedNativePrefix{}, staleApplyError(options.PlanWasDisclosed, err)
						}
					}
					schedule, err := compileApplyForwardEffectSchedule(refreshed, providerActions, fresh.ApplyInput)
					if err != nil {
						return requalifiedNativePrefix{}, err
					}
					if err := bindApplyContinuationPlans(refreshed.assessment.Reconciliation, stateDirPlan.schedule.continuation, schedule.continuation); err != nil {
						return requalifiedNativePrefix{}, err
					}
					scope, err := fresh.Documents.Evidence().RegistryScope()
					if err != nil {
						return requalifiedNativePrefix{}, err
					}
					return requalifiedNativePrefix{continuation: schedule.continuation, registry: fresh.Registry, scope: scope}, nil
				})
				if err != nil {
					return readiness.Assessment{}, applyContinuationPlan{}, err
				}
				current, currentAuthority, firstEffectRevisions, stateDirBoundaryValidated, revisionsValidated = updated.plan, updated.authority, updated.revisions, updated.stateDirBoundaryValidated, false
				prefixRegistry, prefixRegistryScope = remainder.registry, remainder.scope
				return current.assessment, remainder.continuation, nil
			}
			if stateDirPlan.schedule.managedSuffix != nil {
				executionOptions.afterHostRoutes = func(ctx context.Context, state durable.Snapshot, claims durablecarrier.GlobalCarrierClaims) (_ nativeSegmentResult, returnErr error) {
					updated, remainder, err := requalifyApplyPlan(ctx, requalificationInput(), requalificationAfterHostRoutes, func(ctx context.Context, candidate *commandPlan) (requalifiedNativeSuffix, error) {
						refreshed := *candidate
						if !refreshed.assessment.CurrentState.Equal(state) || !refreshed.assessment.GlobalCarrierClaims.Equal(claims) {
							return requalifiedNativeSuffix{}, staleApplyError(options.PlanWasDisclosed, nil)
						}
						input, err := nativeBeforeDelegateInput(refreshed)
						if err != nil {
							return requalifiedNativeSuffix{}, err
						}
						if matches, err := prefixRegistryScope.Matches(prefixRegistry, input.Registry); err != nil || !matches {
							return requalifiedNativeSuffix{}, staleApplyError(options.PlanWasDisclosed, err)
						}
						plan, err := stateDirPlan.schedule.managedSuffix.Bind(input)
						if err != nil {
							return requalifiedNativeSuffix{}, staleApplyError(options.PlanWasDisclosed, err)
						}
						return requalifiedNativeSuffix{input: input, plan: plan}, nil
					})
					if err != nil {
						return nativeSegmentResult{}, err
					}
					current, currentAuthority, firstEffectRevisions, stateDirBoundaryValidated, revisionsValidated = updated.plan, updated.authority, updated.revisions, updated.stateDirBoundaryValidated, false
					reconciliation := current.assessment.Reconciliation
					input, plan := remainder.input, remainder.plan
					if options.NativeFilesystem != nil {
						input.Filesystem = options.NativeFilesystem
					}
					// Exact classes also need fresh qualification; journal mutations
					// alone do not cover the final order checks.
					orders := current.nativePlan.orders
					if nativeDelegatesScheduled(reconciliation.Delegates()) {
						orders = nil
					}
					segment, err := executeNativeOrderedSegment(ctx, current.context.Paths, current.context.RuntimeEnvironment, current.context.Lockfile, current.context.Selection, reconciliation, input, plan, orders, executionOptions)
					if err == nil && segment.applied.ExecutionAttempted {
						settled, present := segment.applied.NativeSettlement()
						if !present {
							return segment, fmt.Errorf("native prerequisites have no observed settlement")
						}
						prerequisiteSettlement = &settled
					}
					return segment, err
				}
			}
			if stateDirPlan.schedule.delegateSuffix != nil {
				for _, order := range current.nativePlan.orders {
					executionOptions.deferredNativeOrders = append(executionOptions.deferredNativeOrders, selectedOrderSequenceKey(order))
				}
				executionOptions.afterDelegates = func(ctx context.Context, state durable.Snapshot, claims durablecarrier.GlobalCarrierClaims) (nativeSegmentResult, error) {
					qualification := requalificationInput()
					qualification.request = delegateReplanInput
					updated, remainder, err := requalifyApplyPlan(ctx, qualification, requalificationAfterDelegates, func(ctx context.Context, candidate *commandPlan) (requalifiedNativeSuffix, error) {
						if !candidate.assessment.CurrentState.Equal(state) || !candidate.assessment.GlobalCarrierClaims.Equal(claims) {
							return requalifiedNativeSuffix{}, staleApplyError(options.PlanWasDisclosed, nil)
						}
						input, err := nativeApplyInputFor(*candidate)
						if err != nil {
							return requalifiedNativeSuffix{}, err
						}
						matches, err := prefixRegistryScope.Matches(prefixRegistry, input.Registry)
						if prerequisiteSettlement != nil {
							matches, err = prerequisiteSettlement.MatchesRegistry(input.Registry)
						}
						if err != nil || !matches {
							return requalifiedNativeSuffix{}, staleApplyError(options.PlanWasDisclosed, err)
						}
						plan, err := stateDirPlan.schedule.delegateSuffix.Bind(input)
						return requalifiedNativeSuffix{input: input, plan: plan}, err
					})
					if err != nil {
						return nativeSegmentResult{}, err
					}
					current, currentAuthority, firstEffectRevisions, stateDirBoundaryValidated, revisionsValidated = updated.plan, updated.authority, updated.revisions, updated.stateDirBoundaryValidated, false
					reconciliation := current.assessment.Reconciliation
					input := remainder.input
					if options.NativeFilesystem != nil {
						input.Filesystem = options.NativeFilesystem
					}
					phaseOptions := executionOptions
					phaseOptions.deferredNativeOrders = nil
					return executeNativeOrderedSegment(ctx, current.context.Paths, current.context.RuntimeEnvironment, current.context.Lockfile, current.context.Selection, reconciliation, input, remainder.plan, current.nativePlan.orders, phaseOptions)
				}
			}
			executionOptions.afterNativeProviders = func(ctx context.Context, state durable.Snapshot, claims durablecarrier.GlobalCarrierClaims, stable mutation.OperationFingerprint) (_ applyContinuationPlan, returnErr error) {
				updated, continuation, err := requalifyApplyPlan(ctx, requalificationInput(), requalificationAfterProviders, func(ctx context.Context, refreshed *commandPlan) (applyContinuationPlan, error) {
					if !refreshed.assessment.CurrentState.Equal(state) || !refreshed.assessment.GlobalCarrierClaims.Equal(claims) {
						return applyContinuationPlan{}, staleApplyError(options.PlanWasDisclosed, nil)
					}
					if err := validatePostProviderPlan(*refreshed); err != nil {
						return applyContinuationPlan{}, err
					}
					if err := requireCurrentMCPProviders(refreshed.assessment.MCPProviders); err != nil {
						return applyContinuationPlan{}, err
					}
					observedStable, err := providerStableFingerprint(*refreshed, execution.operationContext)
					if err != nil || !observedStable.Equal(stable) {
						return applyContinuationPlan{}, staleApplyError(options.PlanWasDisclosed, err)
					}
					if err := prepareNativeInvocationRemainder(ctx, refreshed); err != nil {
						return applyContinuationPlan{}, err
					}
					prefix, err := nativePrefixApplyInputFor(*refreshed)
					if err != nil {
						return applyContinuationPlan{}, err
					}
					if gates, err := nativeoperation.MaximumNativeForwardEffectValidationCount(prefix); err != nil || gates != 0 {
						return applyContinuationPlan{}, staleApplyError(options.PlanWasDisclosed, err)
					}
					fresh, err := nativeBeforeDelegateInput(*refreshed)
					if err != nil {
						return applyContinuationPlan{}, err
					}
					if matches, err := prefixRegistryScope.Matches(prefixRegistry, fresh.Registry); err != nil || !matches {
						return applyContinuationPlan{}, staleApplyError(options.PlanWasDisclosed, err)
					}
					if stateDirPlan.schedule.managedSuffix != nil {
						if _, err := stateDirPlan.schedule.managedSuffix.Bind(fresh); err != nil {
							return applyContinuationPlan{}, staleApplyError(options.PlanWasDisclosed, err)
						}
					} else if gates, err := nativeoperation.MaximumNativeForwardEffectValidationCount(fresh); err != nil || gates != 0 {
						return applyContinuationPlan{}, staleApplyError(options.PlanWasDisclosed, err)
					}
					schedule, err := equivalentProviderFinalSchedule(stateDirPlan.schedule.finalBinding(), *refreshed, providerActions)
					if err != nil {
						return applyContinuationPlan{}, staleApplyError(options.PlanWasDisclosed, err)
					}
					return schedule.continuation, nil
				})
				if err != nil {
					return applyContinuationPlan{}, err
				}
				current, currentAuthority, firstEffectRevisions, stateDirBoundaryValidated, revisionsValidated = updated.plan, updated.authority, updated.revisions, updated.stateDirBoundaryValidated, false
				return continuation, nil
			}
		}
	}
	runProviders := func(ctx context.Context, phaseOptions runOptions) (providerPhaseExecution, error) {
		return runMCPProviderPrerequisitePhase(
			ctx,
			&current,
			providerActions,
			stateDirPlan.schedule.finalBinding(),
			currentInput,
			execution,
			execution.authorityEvidence,
			effectPaths,
			store,
			leases,
			firstEffectRevisions,
			executionGuard,
			phaseOptions,
			options.PlanWasDisclosed,
		)
	}
	if executionOptions.afterNativeProviders != nil && len(providerActions) != 0 {
		afterPrefix := executionOptions.afterNativeCore
		executionOptions.afterNativeCore = func(ctx context.Context, applied nativeoperation.ApplyResult) (readiness.Assessment, applyContinuationPlan, error) {
			_, continuation, prefixErr := afterPrefix(ctx, applied)
			if prefixErr != nil {
				return readiness.Assessment{}, applyContinuationPlan{}, prefixErr
			}
			phaseOptions := executionOptions
			phaseOptions.currentContinuation = continuation
			phase, phaseErr := runProviders(ctx, phaseOptions)
			providerPhase = phase
			uncompensatedEffectsAttempted = phase.uncompensatedEffectsAttempted
			return current.assessment, phase.currentContinuation, phaseErr
		}
	} else {
		providerPhase, err = runProviders(ctx, executionOptions)
	}
	uncompensatedEffectsAttempted = providerPhase.uncompensatedEffectsAttempted
	if err != nil {
		current.result.HostRouteAttempts = providerPhase.attempts
		return disclose(current), err
	}
	if providerPhase.rebound {
		leases = providerPhase.leases
		firstEffectRevisions = providerPhase.firstEffectRevisions
		stateDirBoundaryValidated, revisionsValidated = false, false
		executionOptions.currentContinuation = providerPhase.currentContinuation
	}

	runResult, err := runWithOptions(
		ctx,
		effectPaths,
		current.context.RuntimeEnvironment,
		current.context.Lockfile,
		current.context.Selection,
		current.assessment,
		executionOptions,
	)
	if nativeInvocations != nil {
		current.result.PermissionProtection = ownershipobserve.ObservePendingProtections(context.WithoutCancel(ctx), current.context.Paths)
	}
	if err != nil {
		current.result.DelegateAttempts = runResult.DelegateAttempts
		current.result.RelationOrderResults = runResult.RelationOrderResults
		if runResult.ReconciliationUpdated {
			current.result.Reconciliation = runResult.Reconciliation
		}
		current.result.HostRouteAttempts = append(providerPhase.attempts, runResult.HostRouteAttempts...)
		committedAdoptions, _, adoptionErr := committedCarrierAdoptionClaims(
			current.result.Reconciliation.CarrierAdoptions(),
			runResult.State,
			runResult.GlobalCarrierClaims,
		)
		current.result.CarrierAdoptionResults = committedAdoptions
		if runResult.ActionCount != 0 {
			current.result.ActionCount = runResult.ActionCount
		}
		if runResult.StatePath != "" {
			current.result.StatefilePath = runResult.StatePath
		}
		return disclose(current), errors.Join(err, adoptionErr)
	}

	carrierAdoptionResults, err := finalizedCarrierAdoptionClaims(
		current.result.Reconciliation.CarrierAdoptions(),
		runResult.State,
		runResult.GlobalCarrierClaims,
	)
	if err != nil {
		return disclose(current), fmt.Errorf("resolve finalized carrier adoption claims: %w", err)
	}
	current.result.ActionCount = runResult.ActionCount
	current.result.StatefilePath = runResult.StatePath
	current.result.DelegateAttempts = runResult.DelegateAttempts
	current.result.RelationOrderResults = runResult.RelationOrderResults
	if runResult.ReconciliationUpdated {
		current.result.Reconciliation = runResult.Reconciliation
	}
	current.result.HostRouteAttempts = append(providerPhase.attempts, runResult.HostRouteAttempts...)
	current.result.CarrierAdoptionResults = carrierAdoptionResults
	return disclose(current), nil
}

func staleApplyError(disclosed bool, cause error) error {
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return cause
	}
	state := recoverygate.StateOf(cause)
	if state.Observed() {
		return cause
	}
	var stale error = mutation.StaleSnapshotError{}
	if disclosed {
		stale = mutation.StalePlanError{}
	}
	return errors.Join(stale, cause)
}

type applyExecutionGuard struct {
	declarationRevisions mutation.RevisionSet
	planWasDisclosed     bool
	valid                bool
}

func newApplyExecutionGuard(
	revisions mutation.RevisionSet,
	planWasDisclosed bool,
) applyExecutionGuard {
	return applyExecutionGuard{
		declarationRevisions: revisions,
		planWasDisclosed:     planWasDisclosed,
		valid:                true,
	}
}

func captureDeclarationRevisions(
	ctx context.Context,
	manifestPath string,
	lockfilePath string,
) (mutation.RevisionSet, error) {
	return mutation.CaptureBoundedFileRevisionSet(
		ctx,
		declarationartifact.MaximumBytes,
		manifestPath,
		lockfilePath,
	)
}

func (guard applyExecutionGuard) requireDeclarationsCurrent(
	ctx context.Context,
	phase string,
) error {
	if !guard.valid {
		return fmt.Errorf("apply execution guard is required")
	}
	matches, err := guard.declarationRevisions.MatchesCurrent(ctx)
	if err != nil {
		return err
	}
	if matches {
		return nil
	}
	return staleApplyError(
		guard.planWasDisclosed,
		errors.New(phase+": manifest or selected lockfile changed"),
	)
}

func (guard applyExecutionGuard) requirePlanCurrent(
	ctx context.Context,
	expected mutation.OperationFingerprint,
	actual mutation.OperationFingerprint,
	phase string,
) error {
	if err := guard.requireDeclarationsCurrent(ctx, phase); err != nil {
		return err
	}
	if expected.Equal(actual) {
		return nil
	}
	return staleApplyError(
		guard.planWasDisclosed,
		errors.New(phase+": remaining execution plan changed"),
	)
}

func remainingExecutionFingerprint(
	reconciliation reconcile.Result,
) (mutation.OperationFingerprint, error) {
	relationOrders, err := marshalRemainingExecutionFingerprintProjection(
		relationOrderFingerprintRows(reconciliation.RelationOrders()),
	)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	delegateActions, err := marshalRemainingExecutionFingerprintProjection(
		delegateFingerprintRows(reconciliation.Delegates()),
	)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	return operationplan.RemainingApplyOperationFingerprint(operationplan.RemainingApplyIdentityInput{
		RelationOrders:  relationOrders,
		DelegateActions: delegateActions,
	})
}

func marshalRemainingExecutionFingerprintProjection[T any](value T) (json.RawMessage, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("fingerprint remaining apply execution: %w", err)
	}
	return json.RawMessage(payload), nil
}
