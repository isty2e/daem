package apply

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func isGlobalCarrierPromotionCandidate(
	current durable.Snapshot,
	action reconciliation.RelationAction,
) bool {
	_, _, matched := execute.MatchPendingCarrierInstallCompletion(
		current,
		action,
		target.ScopeGlobal,
	)
	return matched
}

func commitPreparedGlobalCarrierPromotions(
	ctx context.Context,
	paths daempaths.Paths,
	stateAuthority *statefileEffectAuthority,
	current durable.Snapshot,
	registry durablecarrier.GlobalCarrierClaims,
	promotions []preparedGlobalCarrierPromotion,
	options runOptions,
	execution *applyContinuationExecution,
) (durable.Snapshot, durablecarrier.GlobalCarrierClaims, error) {
	nextState := current
	nextRegistry := registry
	for _, promotion := range promotions {
		action := promotion.action
		correlation, present := action.Correlation()
		if !present {
			return nextState, nextRegistry, fmt.Errorf(
				"exact correlation is required",
			)
		}
		plan, err := prepareGlobalCarrierPromotionSettlementPlan(
			paths.CarrierClaimRegistryPath,
			nextState,
			nextRegistry,
			action,
			correlation,
		)
		if err != nil {
			return nextState, nextRegistry, err
		}
		nextState, nextRegistry, err = commitObservedGlobalCarrierClaimWithContinuation(
			ctx,
			paths,
			stateAuthority,
			nextState,
			nextRegistry,
			action,
			correlation,
			plan,
			options,
			execution,
			promotion.ref,
		)
		if err != nil {
			return nextState, nextRegistry, err
		}
	}
	return nextState, nextRegistry, nil
}

func commitObservedGlobalCarrierClaim(
	ctx context.Context,
	paths daempaths.Paths,
	stateAuthority *statefileEffectAuthority,
	current durable.Snapshot,
	registry durablecarrier.GlobalCarrierClaims,
	action reconciliation.RelationAction,
	observation observerelation.CorrelationResult,
	plan globalCarrierSettlementPlan,
	options runOptions,
) (durable.Snapshot, durablecarrier.GlobalCarrierClaims, error) {
	return commitObservedGlobalCarrierClaimWithContinuation(
		ctx,
		paths,
		stateAuthority,
		current,
		registry,
		action,
		observation,
		plan,
		options,
		nil,
		"",
	)
}

func commitObservedGlobalCarrierClaimWithContinuation(
	ctx context.Context,
	paths daempaths.Paths,
	stateAuthority *statefileEffectAuthority,
	current durable.Snapshot,
	registry durablecarrier.GlobalCarrierClaims,
	action reconciliation.RelationAction,
	observation observerelation.CorrelationResult,
	plan globalCarrierSettlementPlan,
	options runOptions,
	execution *applyContinuationExecution,
	ref string,
) (durable.Snapshot, durablecarrier.GlobalCarrierClaims, error) {
	claim, matched, err := globalCarrierPromotionClaim(current, registry, action, observation)
	if err != nil {
		return current, registry, fmt.Errorf("promote observed global carrier claim: %w", err)
	}
	if !matched {
		return current, registry, nil
	}
	return executeGlobalCarrierPromotionSettlement(
		ctx,
		plan,
		paths.CarrierClaimRegistryPath,
		action,
		claim,
		current,
		registry,
		globalCarrierPromotionSettlementCallbacks{
			validateDeclarationsBefore: func() error {
				return scheduledContinuationCall(
					execution,
					ref+"/declarations-before-registry",
					operationplan.EffectStepObservation,
					func() error {
						return options.executionGuard.requireDeclarationsCurrent(
							ctx,
							"global carrier promotion before registry persistence",
						)
					},
				)
			},
			validateProjectRootBefore: func() error {
				return scheduledContinuationCall(
					execution,
					ref+"/project-root-before-registry",
					operationplan.EffectStepObservation,
					func() error { return validateHostRouteProjectRoot(options, paths.ManifestRoot) },
				)
			},
			validateStatefileBefore: func() error {
				if stateAuthority == nil {
					return fmt.Errorf("global carrier promotion statefile authority is required")
				}
				return scheduledCarrierRemovalStatefileValidation(
					ctx,
					execution,
					ref+"/statefile/pre-registry",
					stateAuthority,
					nil,
				)
			},
			persistRegistry: func() (durablecarrier.GlobalCarrierClaims, error) {
				result := registry
				err := scheduledContinuationCall(
					execution,
					ref+"/global-registry",
					operationplan.EffectStepPersistence,
					func() error {
						successor, _, claimErr := registry.WithClaim(claim)
						if claimErr != nil {
							return claimErr
						}
						if contextErr := ctx.Err(); contextErr != nil {
							return contextErr
						}
						options.markAttempted()
						store, storeErr := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
						if storeErr != nil {
							return storeErr
						}
						observed, persistErr := store.UpsertAllIfCurrent(
							ctx,
							registry,
							[]durablecarrier.ManagedCarrierClaim{claim},
						)
						result, persistErr = globalCarrierClaimsAfterPersistence(
							registry,
							successor,
							observed,
							persistErr,
						)
						return persistErr
					},
				)
				return result, err
			},
			validateStatefileAfter: func() error {
				return scheduledCarrierRemovalStatefileValidation(
					ctx,
					execution,
					ref+"/statefile/post-registry",
					stateAuthority,
					nil,
				)
			},
			acceptRegistryVisibility: func() error {
				return scheduledContinuationCall(
					execution,
					ref+"/registry-visibility",
					operationplan.EffectStepObservation,
					func() error {
						if options.acceptVisibilityChanges == nil {
							return fmt.Errorf("global carrier promotion registry acceptance is required")
						}
						return options.acceptVisibilityChanges(ctx)
					},
				)
			},
			publishStatefile: func(nextRegistry durablecarrier.GlobalCarrierClaims) (durable.Snapshot, error) {
				next := current
				err := scheduledCarrierRemovalStatefilePublication(
					execution,
					ref+"/statefile/project-claim",
					func() error {
						entry, entryErr := stateAuthority.EntryForCommit()
						if entryErr != nil {
							return entryErr
						}
						next, entryErr = execute.CommitConvergedGlobalCarrierClaims(
							ctx,
							storagecommit.Adapter{},
							entry,
							current,
							nextRegistry,
							statefile.Codec{},
						)
						return entryErr
					},
					nil,
				)
				return next, err
			},
			validateStatefileFinal: func() error {
				return scheduledCarrierRemovalStatefileValidation(
					ctx,
					execution,
					ref+"/statefile/post-claim",
					stateAuthority,
					nil,
				)
			},
			acceptStatefileVisibility: func() error {
				return scheduledContinuationCall(
					execution,
					ref+"/statefile-visibility",
					operationplan.EffectStepObservation,
					func() error {
						if options.acceptVisibilityChanges == nil {
							return fmt.Errorf("global carrier promotion statefile acceptance is required")
						}
						return options.acceptVisibilityChanges(ctx)
					},
				)
			},
			validateProjectRootAfter: func() error {
				return scheduledContinuationCall(
					execution,
					ref+"/project-root-after-claim",
					operationplan.EffectStepObservation,
					func() error { return validateHostRouteProjectRoot(options, paths.ManifestRoot) },
				)
			},
			validateDeclarationsAfter: func() error {
				return scheduledContinuationCall(
					execution,
					ref+"/declarations-after-claim",
					operationplan.EffectStepObservation,
					func() error {
						return options.executionGuard.requireDeclarationsCurrent(
							ctx,
							"global carrier promotion after statefile persistence",
						)
					},
				)
			},
		},
	)
}

func prepareGlobalCarrierPromotionSettlementPlan(
	registryPath string,
	current durable.Snapshot,
	registry durablecarrier.GlobalCarrierClaims,
	action reconciliation.RelationAction,
	observation observerelation.CorrelationResult,
) (globalCarrierSettlementPlan, error) {
	claim, matched, err := globalCarrierPromotionClaim(current, registry, action, observation)
	if err != nil {
		return globalCarrierSettlementPlan{}, fmt.Errorf("promote observed global carrier claim: %w", err)
	}
	if !matched {
		return globalCarrierSettlementPlan{}, nil
	}
	return newGlobalCarrierPromotionSettlementPlan(registryPath, registry, action, claim)
}

func globalCarrierPromotionClaim(
	current durable.Snapshot,
	registry durablecarrier.GlobalCarrierClaims,
	action reconciliation.RelationAction,
	observation observerelation.CorrelationResult,
) (durablecarrier.ManagedCarrierClaim, bool, error) {
	if action.Scope() != target.ScopeGlobal {
		return durablecarrier.ManagedCarrierClaim{}, false, nil
	}
	pending, matched := execute.MatchPendingCarrierInstall(current, action, target.ScopeGlobal)
	if !matched {
		return durablecarrier.ManagedCarrierClaim{}, false, nil
	}
	claim, err := durablecarrier.ClaimAfterObservedInstall(
		pending,
		observation,
		registry.Claims(),
	)
	return claim, true, err
}
