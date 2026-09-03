package apply

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/effect/mutation"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/target"
)

func stateOnlyCarrierClaimAdoptions(
	current durable.Snapshot,
	actions []carrieradoption.Action,
) ([]durablecarrier.ManagedCarrierClaim, []durablecarrier.ManagedCarrierClaim, error) {
	project := make([]durablecarrier.ManagedCarrierClaim, 0, len(actions))
	global := make([]durablecarrier.ManagedCarrierClaim, 0, len(actions))
	for index, action := range actions {
		if err := action.Validate(); err != nil {
			return nil, nil, fmt.Errorf("carrier adoption action[%d]: %w", index, err)
		}
		if !action.StateOnly() {
			continue
		}
		if action.InvokesHostRoute() {
			return nil, nil, fmt.Errorf(
				"carrier adoption action[%d] unexpectedly invokes a host route",
				index,
			)
		}
		claim, present := action.ProposedClaim()
		if !present {
			return nil, nil, fmt.Errorf(
				"carrier adoption action[%d] has no proposed claim",
				index,
			)
		}
		if completedByPendingInstall(current, claim) {
			continue
		}
		if conflictsWithPendingInstall(current, claim) {
			return nil, nil, fmt.Errorf(
				"carrier adoption action[%d] conflicts with pending acquisition",
				index,
			)
		}
		switch claim.Identity().Scope() {
		case target.ScopeProject:
			project = append(project, claim)
		case target.ScopeGlobal:
			global = append(global, claim)
		default:
			return nil, nil, fmt.Errorf(
				"carrier adoption action[%d] has unsupported scope %q",
				index,
				claim.Identity().Scope(),
			)
		}
	}
	return project, global, nil
}

func finalizedCarrierAdoptionClaims(
	actions []carrieradoption.Action,
	project durable.Snapshot,
	global durablecarrier.GlobalCarrierClaims,
) ([]durablecarrier.ManagedCarrierClaim, error) {
	results, expected, err := committedCarrierAdoptionClaims(actions, project, global)
	if err != nil {
		return nil, err
	}
	if len(results) != expected {
		return nil, fmt.Errorf(
			"carrier adoption finalized %d claims, want exactly %d",
			len(results),
			expected,
		)
	}
	return results, nil
}

func committedCarrierAdoptionClaims(
	actions []carrieradoption.Action,
	project durable.Snapshot,
	global durablecarrier.GlobalCarrierClaims,
) ([]durablecarrier.ManagedCarrierClaim, int, error) {
	results := make([]durablecarrier.ManagedCarrierClaim, 0, len(actions))
	expected := 0
	for index, action := range actions {
		if err := action.Validate(); err != nil {
			return nil, 0, fmt.Errorf("carrier adoption action[%d]: %w", index, err)
		}
		if !action.StateOnly() {
			continue
		}
		expected++
		proposed, present := action.ProposedClaim()
		if !present {
			return nil, 0, fmt.Errorf("carrier adoption action[%d] has no proposed claim", index)
		}
		var claims []durablecarrier.ManagedCarrierClaim
		switch proposed.Identity().Scope() {
		case target.ScopeProject:
			claims = project.ManagedCarrierClaims()
		case target.ScopeGlobal:
			claims = global.Claims()
		default:
			return nil, 0, fmt.Errorf(
				"carrier adoption action[%d] has unsupported scope %q",
				index,
				proposed.Identity().Scope(),
			)
		}
		var matched durablecarrier.ManagedCarrierClaim
		matchCount := 0
		for _, claim := range claims {
			if claim.SameAcquisition(proposed) {
				matched = claim
				matchCount++
			}
		}
		if matchCount > 1 {
			return nil, 0, fmt.Errorf(
				"carrier adoption action[%d] has %d committed claims, want at most one",
				index,
				matchCount,
			)
		}
		if matchCount == 1 {
			results = append(results, matched)
		}
	}
	return results, expected, nil
}

func completedByPendingInstall(
	current durable.Snapshot,
	claim durablecarrier.ManagedCarrierClaim,
) bool {
	for _, pending := range current.PendingCarrierInstalls() {
		if pending.Owner().ExactEqual(claim.Owner()) &&
			pending.Identity().ExactEqual(claim.Identity()) &&
			pending.InstallRequest().Equal(claim.InstallRequest()) {
			return true
		}
	}
	return false
}

func conflictsWithPendingInstall(
	current durable.Snapshot,
	claim durablecarrier.ManagedCarrierClaim,
) bool {
	for _, pending := range current.PendingCarrierInstalls() {
		if pending.Owner().Equal(claim.Owner()) &&
			pending.Identity().RelationSubject() == claim.Identity().RelationSubject() {
			return true
		}
	}
	return false
}

func commitGlobalCarrierAdoptions(
	ctx context.Context,
	registryPath string,
	current durablecarrier.GlobalCarrierClaims,
	adoptions []durablecarrier.ManagedCarrierClaim,
	plan globalCarrierSettlementPlan,
	options runOptions,
) (durablecarrier.GlobalCarrierClaims, int, error) {
	return executeGlobalCarrierBatchSettlement(
		ctx,
		plan,
		globalCarrierSettlementAdoption,
		registryPath,
		adoptions,
		current,
		globalCarrierBatchSettlementCallbacks{
			validateBefore: func() error {
				if options.validateBeforeEffects == nil {
					return fmt.Errorf("global carrier settlement effect validation is required")
				}
				return options.validateBeforeEffects(ctx, mutation.PhysicalAuthoritySet{})
			},
			persist: func() (durablecarrier.GlobalCarrierClaims, int, error) {
				successor, _, err := current.WithClaims(adoptions)
				if err != nil {
					return current, 0, err
				}
				if err := ctx.Err(); err != nil {
					return current, 0, err
				}
				options.markAttempted()
				store, err := carrierclaimstore.New(registryPath)
				if err != nil {
					return current, 0, err
				}
				observed, persistErr := store.UpsertAllIfCurrent(ctx, current, adoptions)
				next, persistErr := globalCarrierClaimsAfterPersistence(
					current,
					successor,
					observed,
					persistErr,
				)
				if persistErr != nil {
					return next, 0, fmt.Errorf("commit global carrier adoptions: %w", persistErr)
				}
				count := len(next.Claims()) - len(current.Claims())
				if count < 0 {
					return current, 0, fmt.Errorf("global carrier adoption removed retained claims")
				}
				return next, count, nil
			},
			validateAfter: func() error {
				if options.validateStateDir == nil || options.acceptVisibilityChanges == nil {
					return fmt.Errorf("global carrier settlement post-commit validation is required")
				}
				if err := options.validateStateDir(ctx); err != nil {
					return err
				}
				return options.acceptVisibilityChanges(ctx)
			},
		},
	)
}
