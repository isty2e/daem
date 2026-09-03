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

func commitInterruptedGlobalCarrierClaims(
	ctx context.Context,
	paths daempaths.Paths,
	stateAuthority *statefileEffectAuthority,
	current durable.Snapshot,
	registry durablecarrier.GlobalCarrierClaims,
	actions []reconciliation.RelationAction,
	options runOptions,
) (durable.Snapshot, durablecarrier.GlobalCarrierClaims, error) {
	nextState := current
	nextRegistry := registry
	for _, action := range actions {
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
		nextState, nextRegistry, err = commitObservedGlobalCarrierClaim(
			ctx,
			paths,
			stateAuthority,
			nextState,
			nextRegistry,
			action,
			correlation,
			plan,
			options,
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
				return options.executionGuard.requireDeclarationsCurrent(
					ctx,
					"global carrier promotion before registry persistence",
				)
			},
			validateProjectRootBefore: func() error {
				return validateHostRouteProjectRoot(options, paths.ManifestRoot)
			},
			validateStatefileBefore: func() error {
				if stateAuthority == nil {
					return fmt.Errorf("global carrier promotion statefile authority is required")
				}
				return stateAuthority.Validate(ctx)
			},
			persistRegistry: func() (durablecarrier.GlobalCarrierClaims, error) {
				successor, _, err := registry.WithClaim(claim)
				if err != nil {
					return registry, err
				}
				if err := ctx.Err(); err != nil {
					return registry, err
				}
				options.markAttempted()
				store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
				if err != nil {
					return registry, err
				}
				observed, persistErr := store.UpsertAllIfCurrent(
					ctx,
					registry,
					[]durablecarrier.ManagedCarrierClaim{claim},
				)
				return globalCarrierClaimsAfterPersistence(
					registry,
					successor,
					observed,
					persistErr,
				)
			},
			validateStatefileAfter: func() error {
				return stateAuthority.Validate(ctx)
			},
			acceptRegistryVisibility: func() error {
				if options.acceptVisibilityChanges == nil {
					return fmt.Errorf("global carrier promotion registry acceptance is required")
				}
				return options.acceptVisibilityChanges(ctx)
			},
			publishStatefile: func(nextRegistry durablecarrier.GlobalCarrierClaims) (durable.Snapshot, error) {
				entry, err := stateAuthority.EntryForCommit()
				if err != nil {
					return current, err
				}
				return execute.CommitConvergedGlobalCarrierClaims(
					ctx,
					storagecommit.Adapter{},
					entry,
					current,
					nextRegistry,
					statefile.Codec{},
				)
			},
			validateStatefileFinal: func() error {
				return stateAuthority.Validate(ctx)
			},
			acceptStatefileVisibility: func() error {
				if options.acceptVisibilityChanges == nil {
					return fmt.Errorf("global carrier promotion statefile acceptance is required")
				}
				return options.acceptVisibilityChanges(ctx)
			},
			validateProjectRootAfter: func() error {
				return validateHostRouteProjectRoot(options, paths.ManifestRoot)
			},
			validateDeclarationsAfter: func() error {
				return options.executionGuard.requireDeclarationsCurrent(
					ctx,
					"global carrier promotion after statefile persistence",
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
