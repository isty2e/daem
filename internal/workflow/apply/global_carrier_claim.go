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
	if action.Kind() != reconciliation.ActionNoOp || action.Scope() != target.ScopeGlobal {
		return false
	}
	if _, present := action.Correlation(); !present {
		return false
	}
	for _, pending := range current.PendingCarrierInstalls() {
		if pending.Identity().ExactEqual(action.CarrierIdentity()) &&
			pending.InstallRequest().Equal(action.RouteRequest()) {
			return true
		}
	}
	return false
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
		var err error
		nextState, nextRegistry, err = commitObservedGlobalCarrierClaim(
			ctx,
			paths,
			stateAuthority,
			nextState,
			nextRegistry,
			action,
			correlation,
		)
		if err != nil {
			return nextState, nextRegistry, err
		}
		if err := validateHostRouteProjectRoot(options, paths.ManifestRoot); err != nil {
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
) (durable.Snapshot, durablecarrier.GlobalCarrierClaims, error) {
	if action.Scope() != target.ScopeGlobal {
		return current, registry, nil
	}
	var claim durablecarrier.ManagedCarrierClaim
	found := false
	for _, pending := range current.PendingCarrierInstalls() {
		if !pending.Identity().ExactEqual(action.CarrierIdentity()) ||
			!pending.InstallRequest().Equal(action.RouteRequest()) {
			continue
		}
		promoted, err := durablecarrier.ClaimAfterObservedInstall(
			pending,
			observation,
			registry.Claims(),
		)
		if err != nil {
			return current, registry, fmt.Errorf("promote observed global carrier claim: %w", err)
		}
		claim = promoted
		found = true
		break
	}
	if !found {
		return current, registry, nil
	}
	store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		return current, registry, err
	}
	if err := stateAuthority.Validate(ctx); err != nil {
		return current, registry, err
	}
	nextRegistry, err := store.UpsertAllIfCurrent(
		ctx,
		registry,
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		return current, registry, err
	}
	if err := stateAuthority.Validate(ctx); err != nil {
		return current, nextRegistry, err
	}
	entry, err := stateAuthority.EntryForCommit()
	if err != nil {
		return current, nextRegistry, err
	}
	nextState, err := execute.CommitConvergedGlobalCarrierClaims(
		ctx,
		storagecommit.Adapter{},
		entry,
		current,
		nextRegistry,
		statefile.Codec{},
	)
	if err != nil {
		return current, nextRegistry, err
	}
	if err := stateAuthority.Validate(ctx); err != nil {
		return nextState, nextRegistry, err
	}
	return nextState, nextRegistry, nil
}
