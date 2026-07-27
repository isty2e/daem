package execute

import (
	"context"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

type snapshotCommitter func(context.Context, []byte, os.FileMode) error

// CommitPendingCarrierInstalls records exact write-ahead correlation
// eligibility before admitted create commands are launched.
func CommitPendingCarrierInstalls(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	current durable.Snapshot,
	owner stateauthority.Authority,
	actions []reconciliation.RelationAction,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, error) {
	return commitPendingCarrierInstalls(
		ctx,
		rootedSnapshotCommitter(filesystem, authority),
		current,
		owner,
		actions,
		stateEncoder,
	)
}

func commitPendingCarrierInstalls(
	ctx context.Context,
	commit snapshotCommitter,
	current durable.Snapshot,
	owner stateauthority.Authority,
	actions []reconciliation.RelationAction,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, error) {
	pending, err := pendingCarrierInstalls(owner, actions)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("derive pending carrier installs: %w", err)
	}
	if len(pending) == 0 {
		return current, nil
	}
	next, _, err := current.WithPreparedCarrierInstalls(pending)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("update pending carrier installs: %w", err)
	}
	if stateEncoder == nil {
		return durable.Snapshot{}, fmt.Errorf("pending carrier install state codec is required")
	}
	content, err := stateEncoder.Encode(next)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("marshal pending carrier installs: %w", err)
	}
	if err := commit(ctx, content, 0o600); err != nil {
		return durable.Snapshot{}, fmt.Errorf("write pending carrier installs: %w", err)
	}
	return next, nil
}

// CommitObservedProjectCarrierClaim promotes one exact pending project install
// after fresh admitted post-observation. Global claims use their dedicated
// registry and are not written to the project statefile.
func CommitObservedProjectCarrierClaim(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	current durable.Snapshot,
	action reconciliation.RelationAction,
	observation observerelation.CorrelationResult,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, error) {
	if action.Scope() != target.ScopeProject {
		return current, nil
	}
	var promotion durablecarrier.ManagedCarrierClaim
	found := false
	for _, pending := range current.PendingCarrierInstalls() {
		if !pending.Identity().ExactEqual(action.CarrierIdentity()) ||
			!pending.InstallRequest().Equal(action.RouteRequest()) {
			continue
		}
		claim, err := durablecarrier.ClaimAfterObservedInstall(
			pending,
			observation,
			current.ManagedCarrierClaims(),
		)
		if err != nil {
			return durable.Snapshot{}, fmt.Errorf("promote observed project carrier claim: %w", err)
		}
		promotion = claim
		found = true
		break
	}
	if !found {
		return current, nil
	}
	next, changed, err := current.WithPromotedCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{promotion},
	)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("update observed project carrier claim: %w", err)
	}
	if !changed {
		return current, nil
	}
	if stateEncoder == nil {
		return durable.Snapshot{}, fmt.Errorf("project carrier claim state codec is required")
	}
	content, err := stateEncoder.Encode(next)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("marshal observed project carrier claim: %w", err)
	}
	if err := rootedSnapshotCommitter(filesystem, authority)(ctx, content, 0o600); err != nil {
		return durable.Snapshot{}, fmt.Errorf("write observed project carrier claim: %w", err)
	}
	return next, nil
}

// CommitConvergedGlobalCarrierClaims clears local pending facts whose exact
// global claims have already committed.
func CommitConvergedGlobalCarrierClaims(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	current durable.Snapshot,
	registry durablecarrier.GlobalCarrierClaims,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, error) {
	next, changed, err := current.WithConvergedGlobalCarrierClaims(registry)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("converge global carrier claims: %w", err)
	}
	if !changed {
		return current, nil
	}
	if stateEncoder == nil {
		return durable.Snapshot{}, fmt.Errorf("global carrier convergence state codec is required")
	}
	content, err := stateEncoder.Encode(next)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("marshal global carrier convergence: %w", err)
	}
	if err := rootedSnapshotCommitter(filesystem, authority)(ctx, content, 0o600); err != nil {
		return durable.Snapshot{}, fmt.Errorf("write global carrier convergence: %w", err)
	}
	return next, nil
}

// CommitRetiredPendingCarrierInstall removes the exact write-ahead fact after
// an invocation has returned without establishing a claim. Attempt history is
// persisted separately and never substitutes for this transition.
func CommitRetiredPendingCarrierInstall(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	current durable.Snapshot,
	owner stateauthority.Authority,
	action reconciliation.RelationAction,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, error) {
	if action.Kind() != reconciliation.ActionCreate {
		return current, nil
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(
		owner,
		action.CarrierIdentity(),
		action.RouteRequest(),
	)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("derive completed carrier install: %w", err)
	}
	next, changed, err := current.WithoutPendingCarrierInstall(pending)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("retire completed carrier install: %w", err)
	}
	if !changed {
		return current, nil
	}
	if stateEncoder == nil {
		return durable.Snapshot{}, fmt.Errorf("completed carrier install state codec is required")
	}
	content, err := stateEncoder.Encode(next)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("marshal completed carrier install: %w", err)
	}
	if err := rootedSnapshotCommitter(filesystem, authority)(ctx, content, 0o600); err != nil {
		return durable.Snapshot{}, fmt.Errorf("write completed carrier install: %w", err)
	}
	return next, nil
}

// CommitPendingCarrierRemoval writes one exact E4 boundary before an admitted
// removal effect may execute. A retained exact boundary is idempotent but is
// still returned to the caller for E8 correlation.
func CommitPendingCarrierRemoval(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	current durable.Snapshot,
	globalClaims durablecarrier.GlobalCarrierClaims,
	action carrierabsence.Action,
	effectBaselines durablecarrier.EffectBaselineSet,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, durablecarrier.PendingCarrierRemoval, error) {
	if err := action.Validate(); err != nil {
		return durable.Snapshot{}, durablecarrier.PendingCarrierRemoval{},
			fmt.Errorf("prepare carrier removal action: %w", err)
	}
	if !action.InvokesHostRoute() && !action.MutatesDirectProjection() {
		return durable.Snapshot{}, durablecarrier.PendingCarrierRemoval{},
			fmt.Errorf("prepare carrier removal requires an effectful action")
	}
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		action.Claim(),
		action.RouteAdmission().Request(),
		action.RouteAdmission().Operation().EffectPostconditions(),
		effectBaselines,
	)
	if err != nil {
		return durable.Snapshot{}, durablecarrier.PendingCarrierRemoval{},
			fmt.Errorf("derive pending carrier removal: %w", err)
	}
	next, changed, err := current.WithPreparedCarrierRemovals(
		[]durablecarrier.PendingCarrierRemoval{pending},
		globalClaims,
	)
	if err != nil {
		return durable.Snapshot{}, durablecarrier.PendingCarrierRemoval{},
			fmt.Errorf("update pending carrier removal: %w", err)
	}
	if !changed {
		return current, pending, nil
	}
	if err := commitCarrierState(
		ctx,
		filesystem,
		authority,
		next,
		stateEncoder,
		"pending carrier removal",
	); err != nil {
		return durable.Snapshot{}, durablecarrier.PendingCarrierRemoval{}, err
	}
	return next, pending, nil
}

// CommitRetiredProjectCarrierRemoval atomically removes one exact project
// claim and its E4 boundary after successful absent postcondition
// classification.
func CommitRetiredProjectCarrierRemoval(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	current durable.Snapshot,
	pending durablecarrier.PendingCarrierRemoval,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, error) {
	next, changed, err := current.WithRetiredProjectCarrierRemoval(pending)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("retire project carrier removal: %w", err)
	}
	if !changed {
		return durable.Snapshot{}, fmt.Errorf("retire project carrier removal made no state transition")
	}
	if err := commitCarrierState(
		ctx,
		filesystem,
		authority,
		next,
		stateEncoder,
		"retired project carrier removal",
	); err != nil {
		return durable.Snapshot{}, err
	}
	return next, nil
}

// CommitClearedGlobalCarrierRemovalPending removes only the project-local E4
// boundary after verified global absence. The caller must then retire the
// exact claim through the global registry.
func CommitClearedGlobalCarrierRemovalPending(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	current durable.Snapshot,
	pending durablecarrier.PendingCarrierRemoval,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, error) {
	if pending.Identity().Scope() != target.ScopeGlobal {
		return durable.Snapshot{}, fmt.Errorf(
			"clear global carrier removal pending requires global scope",
		)
	}
	next, changed, err := current.WithoutPendingCarrierRemoval(pending)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("clear global carrier removal pending: %w", err)
	}
	if !changed {
		return durable.Snapshot{}, fmt.Errorf(
			"clear global carrier removal pending has no exact boundary",
		)
	}
	if err := commitCarrierState(
		ctx,
		filesystem,
		authority,
		next,
		stateEncoder,
		"cleared global carrier removal pending",
	); err != nil {
		return durable.Snapshot{}, err
	}
	return next, nil
}

// CommitDelegateAttempts writes last delegate history through retained
// statefile authority after host projection has already committed.
func CommitDelegateAttempts(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	current durable.Snapshot,
	attempts []durableattempt.DelegateAttempt,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, error) {
	return commitDelegateAttempts(
		ctx,
		rootedSnapshotCommitter(filesystem, authority),
		current,
		attempts,
		stateEncoder,
	)
}

func commitDelegateAttempts(
	ctx context.Context,
	commit snapshotCommitter,
	current durable.Snapshot,
	attempts []durableattempt.DelegateAttempt,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, error) {
	if len(attempts) == 0 {
		return current, nil
	}

	next, err := current.WithRecordedDelegateAttempts(attempts)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("update statefile delegate attempts: %w", err)
	}
	if stateEncoder == nil {
		return durable.Snapshot{}, fmt.Errorf("delegate attempt state codec is required")
	}
	content, err := stateEncoder.Encode(next)
	if err != nil {
		return durable.Snapshot{}, fmt.Errorf("marshal statefile delegate attempts: %w", err)
	}
	if err := commit(ctx, content, 0o600); err != nil {
		return durable.Snapshot{}, fmt.Errorf("write statefile delegate attempts: %w", err)
	}
	return next, nil
}

// CommitHostRouteAttempts writes last host-route attempt diagnostics after
// ordinary host projection state has already committed. Persistence uses
// retained statefile authority.
func CommitHostRouteAttempts(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	current durable.Snapshot,
	attempts []durableattempt.HostRouteAttempt,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, error) {
	return commitHostRouteAttempts(
		ctx,
		rootedSnapshotCommitter(filesystem, authority),
		current,
		attempts,
		stateEncoder,
	)
}

func commitHostRouteAttempts(
	ctx context.Context,
	commit snapshotCommitter,
	current durable.Snapshot,
	attempts []durableattempt.HostRouteAttempt,
	stateEncoder durable.SnapshotEncoder,
) (durable.Snapshot, error) {
	if len(attempts) == 0 {
		return current, nil
	}

	next, err := current.WithRecordedHostRouteAttempts(attempts)
	if err != nil {
		return current, fmt.Errorf("update statefile host route attempts: %w", err)
	}
	if stateEncoder == nil {
		return current, fmt.Errorf("host route attempt state codec is required")
	}
	content, err := stateEncoder.Encode(next)
	if err != nil {
		return current, fmt.Errorf("marshal statefile host route attempts: %w", err)
	}
	if err := commit(ctx, content, 0o600); err != nil {
		return current, fmt.Errorf("write statefile host route attempts: %w", err)
	}
	return next, nil
}

func pendingCarrierInstalls(
	owner stateauthority.Authority,
	actions []reconciliation.RelationAction,
) ([]durablecarrier.PendingCarrierInstall, error) {
	pending := make([]durablecarrier.PendingCarrierInstall, 0, len(actions))
	for _, action := range actions {
		if action.Kind() != reconciliation.ActionCreate {
			continue
		}
		value, err := durablecarrier.NewPendingCarrierInstall(
			owner,
			action.CarrierIdentity(),
			action.RouteRequest(),
		)
		if err != nil {
			return nil, err
		}
		pending = append(pending, value)
	}
	return pending, nil
}

func promotedProjectCarrierClaims(
	current durable.Snapshot,
	actions []reconciliation.RelationAction,
) ([]durablecarrier.ManagedCarrierClaim, error) {
	claims := make([]durablecarrier.ManagedCarrierClaim, 0, len(actions))
	for _, action := range actions {
		if action.Kind() != reconciliation.ActionNoOp ||
			action.Scope() != target.ScopeProject {
			continue
		}
		correlation, present := action.Correlation()
		if !present {
			continue
		}
		for _, pending := range current.PendingCarrierInstalls() {
			if !pending.Identity().ExactEqual(action.CarrierIdentity()) ||
				!pending.InstallRequest().Equal(action.RouteRequest()) {
				continue
			}
			claim, err := durablecarrier.ClaimAfterObservedInstall(
				pending,
				correlation,
				current.ManagedCarrierClaims(),
			)
			if err != nil {
				return nil, err
			}
			claims = append(claims, claim)
		}
	}
	return claims, nil
}
