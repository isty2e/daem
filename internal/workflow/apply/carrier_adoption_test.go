package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	executeeffect "github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/mutation"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestExecuteWithOptionsAdoptsExactExternalCarrierWithoutHostMutation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, scope)
			present := exactClaudeCarrierObservations(t, locked, subject, scope)
			hostCalls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					hostCalls++
					return subprocess.CommandResult{Started: true, HasExitCode: true}
				},
			})

			planning, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:           manifestPath,
				LockfilePath:           lockfilePath,
				TargetValues:           []string{"claude-code"},
				RelationObservations:   &present,
				ManageUnmanagedMatches: true,
			})
			if err != nil {
				t.Fatalf("PlanWrite: %v", err)
			}
			adoptions := planning.Reconciliation.CarrierAdoptions()
			if len(adoptions) != 1 ||
				adoptions[0].Result() != carrieradoption.ResultEligibleExactRelation ||
				!adoptions[0].StateOnly() {
				t.Fatalf("carrier adoptions = %#v, want one eligible state-only action", adoptions)
			}

			result, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
				RelationObservations: &present,
				HostRouteExecutor:    executor,
			})
			if err != nil {
				t.Fatalf("ExecuteWithOptions: %v", err)
			}
			if result.ActionCount != 1 || hostCalls != 0 || len(result.HostRouteAttempts) != 0 {
				t.Fatalf(
					"adoption result = actions=%d host_calls=%d attempts=%d, want 1/0/0",
					result.ActionCount,
					hostCalls,
					len(result.HostRouteAttempts),
				)
			}
			assertAdoptedCarrierClaim(t, root, manifestPath, locked.Locked.Subjects()[0], scope)

			retry, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:           manifestPath,
				LockfilePath:           lockfilePath,
				TargetValues:           []string{"claude-code"},
				RelationObservations:   &present,
				ManageUnmanagedMatches: true,
			})
			if err != nil {
				t.Fatalf("retry PlanWrite: %v", err)
			}
			retryResult, err := ExecuteWithOptions(context.Background(), retry, ExecuteOptions{
				RelationObservations: &present,
				HostRouteExecutor:    executor,
			})
			if err != nil {
				t.Fatalf("retry ExecuteWithOptions: %v", err)
			}
			if retryResult.ActionCount != 0 || hostCalls != 0 {
				t.Fatalf("retry = actions=%d host_calls=%d, want idempotent 0/0", retryResult.ActionCount, hostCalls)
			}
			assertAdoptedCarrierClaim(t, root, manifestPath, locked.Locked.Subjects()[0], scope)
		})
	}
}

func TestCarrierAdoptionDryRunAndStaleReobservationRemainPassive(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root, manifestPath, lockfilePath, missing, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, scope)
			present := exactClaudeCarrierObservations(t, locked, subject, scope)

			dryRun, err := PlanDryRun(context.Background(), CommandInput{
				ManifestPath:           manifestPath,
				LockfilePath:           lockfilePath,
				TargetValues:           []string{"claude-code"},
				RelationObservations:   &present,
				ManageUnmanagedMatches: true,
			})
			if err != nil {
				t.Fatalf("PlanDryRun: %v", err)
			}
			if actions := dryRun.Reconciliation.CarrierAdoptions(); len(actions) != 1 || !actions[0].StateOnly() {
				t.Fatalf("dry-run adoptions = %#v, want one state-only action", actions)
			}
			assertNoAdoptedCarrierClaim(t, root, manifestPath, scope)

			planning, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:           manifestPath,
				LockfilePath:           lockfilePath,
				TargetValues:           []string{"claude-code"},
				RelationObservations:   &present,
				ManageUnmanagedMatches: true,
			})
			if err != nil {
				t.Fatalf("PlanWrite: %v", err)
			}
			result, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
				RelationObservations: &missing,
				PlanWasDisclosed:     true,
			})
			var stale mutation.StalePlanError
			if !errors.As(err, &stale) {
				t.Fatalf("ExecuteWithOptions error = %v, want StalePlanError", err)
			}
			if result.ExecutionAttempted {
				t.Fatal("stale carrier adoption reported crossing the execution boundary")
			}
			assertNoAdoptedCarrierClaim(t, root, manifestPath, scope)
		})
	}
}

func TestProjectCarrierAdoptionReportsCommittedClaimAfterPostCommitCancellation(t *testing.T) {
	root, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, target.ScopeProject)
	present := exactClaudeCarrierObservations(t, locked, subject, target.ScopeProject)
	planning, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:           manifestPath,
		LockfilePath:           lockfilePath,
		TargetValues:           []string{"claude-code"},
		RelationObservations:   &present,
		ManageUnmanagedMatches: true,
	})
	if err != nil {
		t.Fatalf("PlanWrite: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result, err := ExecuteWithOptions(ctx, planning, ExecuteOptions{
		RelationObservations: &present,
		ExecuteEvents: func(event executeeffect.Event) {
			if event.Kind == executeeffect.EventStatefileWritten {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteWithOptions error = %v, want post-commit cancellation", err)
	}
	if result.ActionCount != 1 || result.StatefilePath == "" {
		t.Fatalf(
			"partial adoption result = actions=%d state_path=%q, want committed 1/non-empty",
			result.ActionCount,
			result.StatefilePath,
		)
	}
	if len(result.CarrierAdoptionResults) != 1 ||
		result.CarrierAdoptionResults[0].Provenance() !=
			durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved {
		t.Fatalf(
			"partial adoption claims = %#v, want exact committed claim",
			result.CarrierAdoptionResults,
		)
	}
	assertAdoptedCarrierClaim(
		t,
		root,
		manifestPath,
		locked.Locked.Subjects()[0],
		target.ScopeProject,
	)
}

func TestOrdinaryApplyCannotAdoptExactExternalCarrier(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, scope)
			present := exactClaudeCarrierObservations(t, locked, subject, scope)

			prepared, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &present,
			})
			if prepared != nil {
				defer prepared.Close()
			}
			if !errors.Is(err, ErrRelationActionBlock) {
				t.Fatalf("PlanWrite error = %v, want present-unclaimed relation block", err)
			}
			assertNoAdoptedCarrierClaim(t, root, manifestPath, scope)
		})
	}
}

func TestConcurrentCarrierAdoptionPlansRequireFreshReplan(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, scope)
			present := exactClaudeCarrierObservations(t, locked, subject, scope)
			input := CommandInput{
				ManifestPath:           manifestPath,
				LockfilePath:           lockfilePath,
				TargetValues:           []string{"claude-code"},
				RelationObservations:   &present,
				ManageUnmanagedMatches: true,
			}

			first, err := PlanWrite(context.Background(), input)
			if err != nil {
				t.Fatalf("first PlanWrite: %v", err)
			}
			second, err := PlanWrite(context.Background(), input)
			if err != nil {
				t.Fatalf("second PlanWrite: %v", err)
			}

			if _, err := ExecuteWithOptions(context.Background(), first, ExecuteOptions{
				RelationObservations: &present,
				PlanWasDisclosed:     true,
			}); err != nil {
				t.Fatalf("first ExecuteWithOptions: %v", err)
			}
			_, err = ExecuteWithOptions(context.Background(), second, ExecuteOptions{
				RelationObservations: &present,
				PlanWasDisclosed:     true,
			})
			var stale mutation.StalePlanError
			if !errors.As(err, &stale) {
				t.Fatalf("second ExecuteWithOptions error = %v, want stale plan", err)
			}
			assertAdoptedCarrierClaim(t, root, manifestPath, locked.Locked.Subjects()[0], scope)

			retry, err := PlanWrite(context.Background(), input)
			if err != nil {
				t.Fatalf("retry PlanWrite: %v", err)
			}
			result, err := ExecuteWithOptions(context.Background(), retry, ExecuteOptions{
				RelationObservations: &present,
				PlanWasDisclosed:     true,
			})
			if err != nil {
				t.Fatalf("retry ExecuteWithOptions: %v", err)
			}
			if result.ActionCount != 0 {
				t.Fatalf("retry action count = %d, want converged no-op", result.ActionCount)
			}
			assertAdoptedCarrierClaim(t, root, manifestPath, locked.Locked.Subjects()[0], scope)
		})
	}
}

func TestCommittedCarrierAdoptionClaimsReportsOnlyDurableOutcomes(t *testing.T) {
	_, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, target.ScopeProject)
	present := exactClaudeCarrierObservations(t, locked, subject, target.ScopeProject)
	planning, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:           manifestPath,
		LockfilePath:           lockfilePath,
		TargetValues:           []string{"claude-code"},
		RelationObservations:   &present,
		ManageUnmanagedMatches: true,
	})
	if err != nil {
		t.Fatalf("PlanWrite: %v", err)
	}
	defer planning.Close()
	actions := planning.Reconciliation.CarrierAdoptions()
	if len(actions) != 1 {
		t.Fatalf("carrier adoption actions = %#v, want one", actions)
	}
	proposed, ok := actions[0].ProposedClaim()
	if !ok {
		t.Fatal("carrier adoption action has no proposed claim")
	}

	results, expected, err := committedCarrierAdoptionClaims(
		actions,
		durable.EmptySnapshot(),
		durablecarrier.GlobalCarrierClaims{},
	)
	if err != nil || len(results) != 0 || expected != 1 {
		t.Fatalf(
			"uncommitted outcomes = (%#v, %d, %v), want no results and one expected action",
			results,
			expected,
			err,
		)
	}

	project, changed, err := durable.EmptySnapshot().WithAdoptedCarrierClaims(
		[]durablecarrier.ManagedCarrierClaim{proposed},
	)
	if err != nil || !changed {
		t.Fatalf("WithAdoptedCarrierClaims = (%#v, %t, %v)", project, changed, err)
	}
	results, expected, err = committedCarrierAdoptionClaims(
		actions,
		project,
		durablecarrier.GlobalCarrierClaims{},
	)
	if err != nil || len(results) != 1 || expected != 1 ||
		!results[0].ExactEqual(proposed) {
		t.Fatalf(
			"committed outcomes = (%#v, %d, %v), want exact committed claim",
			results,
			expected,
			err,
		)
	}
}

func exactClaudeCarrierObservations(
	t *testing.T,
	locked lock.File,
	subject realization.DelegatedRelation,
	scope target.Scope,
) observerelation.Batch {
	t.Helper()
	return applyClaudeObservationBatch(t, locked, subject, exactClaudeCarrierInventory(t, subject, scope))
}

func exactClaudeCarrierInventory(
	t *testing.T,
	subject realization.DelegatedRelation,
	scope target.Scope,
) observeclaudeplugin.Inventory {
	t.Helper()
	hostScope := observeclaudeplugin.HostScopeProject
	if scope == target.ScopeGlobal {
		hostScope = observeclaudeplugin.HostScopeUser
	}
	return applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			applyClaudePluginCarrierManagedRowWithScope(
				t,
				"context7@market",
				string(subject.ExpectedRelation().ManagedInstanceKey()),
				hostScope,
			),
		},
	})
}

func assertAdoptedCarrierClaim(
	t *testing.T,
	root string,
	manifestPath string,
	record lock.LockedSubjectContract,
	scope target.Scope,
) {
	t.Helper()
	claims := loadCarrierClaimsForScope(t, root, manifestPath, scope)
	if len(claims) != 1 ||
		!claims[0].MatchesLockedRecord(record) ||
		claims[0].Provenance() != durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved {
		t.Fatalf("managed carrier claims = %#v, want one exact adopted claim", claims)
	}
	if scope == target.ScopeGlobal {
		assertNoProjectCarrierClaim(t, root)
		return
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Claims()) != 0 {
		t.Fatalf("project adoption wrote global claims: %#v", registry.Claims())
	}
}

func assertNoAdoptedCarrierClaim(
	t *testing.T,
	root string,
	manifestPath string,
	scope target.Scope,
) {
	t.Helper()
	if claims := loadCarrierClaimsForScope(t, root, manifestPath, scope); len(claims) != 0 {
		t.Fatalf("managed carrier claims = %#v, want none", claims)
	}
}

func assertNoProjectCarrierClaim(t *testing.T, root string) {
	t.Helper()
	statePath := filepath.Join(root, ".daem", "state.json")
	if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
		return
	}
	if claims := loadApplyStatefile(t, statePath).ManagedCarrierClaims(); len(claims) != 0 {
		t.Fatalf("project carrier claims = %#v, want none", claims)
	}
}

func loadCarrierClaimsForScope(
	t *testing.T,
	root string,
	manifestPath string,
	scope target.Scope,
) []durablecarrier.ManagedCarrierClaim {
	t.Helper()
	if scope == target.ScopeProject {
		statePath := filepath.Join(root, ".daem", "state.json")
		if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return loadApplyStatefile(t, statePath).ManagedCarrierClaims()
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatalf("open carrier claim registry: %v", err)
	}
	registry, err := store.Load(t.Context())
	if err != nil {
		t.Fatalf("load carrier claim registry: %v", err)
	}
	return registry.Claims()
}
