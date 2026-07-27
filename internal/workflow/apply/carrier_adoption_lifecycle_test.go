package apply

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/mutation"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestCarrierAdoptionPreservesSharedGlobalClaimAndHonorsCancellation(t *testing.T) {
	t.Run("shared global claim", func(t *testing.T) {
		root, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, target.ScopeGlobal)
		present := exactClaudeCarrierObservations(t, locked, subject, target.ScopeGlobal)
		paths, err := daempaths.Resolve(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		record := locked.Locked.Subjects()[0]
		identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
		if err != nil || !admitted {
			t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
		}
		request, err := lock.DelegatedOperationRequest(record, lock.OperationInstall)
		if err != nil {
			t.Fatal(err)
		}
		foreignPath := filepath.Join(t.TempDir(), ".daem", "state.json")
		foreignKey, err := mutation.CanonicalDirectoryEntryKey(foreignPath)
		if err != nil {
			t.Fatal(err)
		}
		foreignOwner, err := stateauthority.New(
			foreignKey,
			filepath.Join(filepath.Dir(filepath.Dir(foreignPath)), "daem.toml"),
		)
		if err != nil {
			t.Fatal(err)
		}
		foreignClaim, err := durablecarrier.NewManagedCarrierClaim(
			foreignOwner,
			identity,
			request,
			durablecarrier.ClaimProvenanceInstalledObserved,
		)
		if err != nil {
			t.Fatal(err)
		}
		store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
		if err != nil {
			t.Fatal(err)
		}
		before, err := store.Upsert(t.Context(), foreignClaim)
		if err != nil {
			t.Fatal(err)
		}

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
			RelationObservations: &present,
		})
		if err != nil {
			t.Fatalf("ExecuteWithOptions: %v", err)
		}
		if len(result.CarrierAdoptionResults) != 1 ||
			result.CarrierAdoptionResults[0].Provenance() !=
				durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved {
			t.Fatalf(
				"carrier adoption results = %#v, want one explicit adoption claim",
				result.CarrierAdoptionResults,
			)
		}
		after, loadErr := store.Load(t.Context())
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		claims := after.Claims()
		if len(claims) != 2 {
			t.Fatalf("shared carrier claims = %#v, want retained foreign plus selected owner", claims)
		}
		foreignRetained := false
		adoptedAdded := false
		for _, claim := range claims {
			switch {
			case claim.ExactEqual(foreignClaim):
				foreignRetained = true
			case claim.Identity().ExactEqual(identity) &&
				claim.Provenance() == durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved:
				adoptedAdded = true
			}
		}
		if !foreignRetained || !adoptedAdded || len(before.Claims()) != 1 {
			t.Fatalf(
				"shared carrier claims = %#v, foreign_retained=%t adopted_added=%t",
				claims,
				foreignRetained,
				adoptedAdded,
			)
		}
		assertNoProjectCarrierClaim(t, root)
	})

	t.Run("pre-canceled execution", func(t *testing.T) {
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
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := ExecuteWithOptions(ctx, planning, ExecuteOptions{
			RelationObservations: &present,
		}); !errors.Is(err, context.Canceled) {
			t.Fatalf("ExecuteWithOptions error = %v, want context cancellation", err)
		}
		assertNoProjectCarrierClaim(t, root)
	})
}

func TestObservedReinstallPreservesAdoptedCarrierProvenance(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root, manifestPath, lockfilePath, missing, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, scope)
			present := exactClaudeCarrierObservations(t, locked, subject, scope)

			adoption, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:           manifestPath,
				LockfilePath:           lockfilePath,
				TargetValues:           []string{"claude-code"},
				RelationObservations:   &present,
				ManageUnmanagedMatches: true,
			})
			if err != nil {
				t.Fatalf("adoption PlanWrite: %v", err)
			}
			if _, err := ExecuteWithOptions(context.Background(), adoption, ExecuteOptions{
				RelationObservations: &present,
			}); err != nil {
				t.Fatalf("adoption ExecuteWithOptions: %v", err)
			}
			assertAdoptedCarrierClaim(t, root, manifestPath, locked.Locked.Subjects()[0], scope)

			reinstall, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath:         manifestPath,
				LockfilePath:         lockfilePath,
				TargetValues:         []string{"claude-code"},
				RelationObservations: &missing,
			})
			if err != nil {
				t.Fatalf("reinstall PlanWrite: %v", err)
			}
			if actions := reinstall.Reconciliation.Relations(); len(actions) != 1 || !actions[0].InvokesHostRoute() {
				t.Fatalf("reinstall relation actions = %#v, want one host route", actions)
			}
			hostCalls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					hostCalls++
					return subprocess.CommandResult{Started: true, HasExitCode: true}
				},
			})
			observer := func(
				context.Context,
				executehostroute.Command,
				[]durablecarrier.PendingCarrierInstall,
				[]durablecarrier.ManagedCarrierClaim,
			) assurancehostroute.ObservationFact {
				return assurancehostroute.CurrentObservation(
					observeclaudeplugin.Correlate(subject, exactClaudeCarrierInventory(t, subject, scope)),
				)
			}
			result, err := ExecuteWithOptions(context.Background(), reinstall, ExecuteOptions{
				RelationObservations: &missing,
				HostRouteExecutor:    executor,
				HostRouteObserver:    observer,
			})
			if err != nil {
				t.Fatalf("reinstall ExecuteWithOptions: %v", err)
			}
			if hostCalls != 1 || result.ActionCount != 0 {
				t.Fatalf(
					"reinstall = host_calls=%d state_actions=%d, want 1/0",
					hostCalls,
					result.ActionCount,
				)
			}
			assertAdoptedCarrierClaim(t, root, manifestPath, locked.Locked.Subjects()[0], scope)
		})
	}
}

func TestExactPendingInstallCompletionTakesPrecedenceOverAdoption(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			root, manifestPath, lockfilePath, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, scope)
			present := exactClaudeCarrierObservations(t, locked, subject, scope)
			paths, err := daempaths.Resolve(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			statefileKey, err := mutation.CanonicalDirectoryEntryKey(paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			owner, err := stateauthority.New(statefileKey, manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			record := locked.Locked.Subjects()[0]
			identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
			if err != nil || !admitted {
				t.Fatalf("ManagedCarrierIdentityFromLockedRecord = (%#v, %t, %v)", identity, admitted, err)
			}
			request, err := lock.DelegatedOperationRequest(record, lock.OperationInstall)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
			if err != nil {
				t.Fatal(err)
			}
			writeApplyStatefile(t, paths.StatefilePath, applyStateSnapshot(t, durable.SnapshotInput{
				PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
			}))

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
			hostCalls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
				Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					hostCalls++
					return subprocess.CommandResult{Started: true, HasExitCode: true}
				},
			})
			result, err := ExecuteWithOptions(context.Background(), planning, ExecuteOptions{
				RelationObservations: &present,
				HostRouteExecutor:    executor,
			})
			if err != nil {
				t.Fatalf("ExecuteWithOptions: %v", err)
			}
			if hostCalls != 0 {
				t.Fatalf("host route calls = %d, want none for observed pending completion", hostCalls)
			}
			if len(result.CarrierAdoptionResults) != 1 ||
				result.CarrierAdoptionResults[0].Provenance() != durablecarrier.ClaimProvenanceInstalledObserved {
				t.Fatalf(
					"carrier adoption results = %#v, want one install-recovery claim",
					result.CarrierAdoptionResults,
				)
			}
			claims := loadCarrierClaimsForScope(t, root, manifestPath, scope)
			if len(claims) != 1 ||
				!claims[0].MatchesLockedRecord(record) ||
				claims[0].Provenance() != durablecarrier.ClaimProvenanceInstalledObserved {
				t.Fatalf("completed pending claims = %#v, want one installed provenance claim", claims)
			}
			state := loadApplyStatefile(t, paths.StatefilePath)
			if len(state.PendingCarrierInstalls()) != 0 {
				t.Fatalf("pending installs = %#v, want completed fact retired", state.PendingCarrierInstalls())
			}
		})
	}
}
