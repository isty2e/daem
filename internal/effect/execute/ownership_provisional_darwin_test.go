//go:build darwin

package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
)

func TestProvisionalGlobalOwnershipCreatePromotesAndFinalizes(t *testing.T) {
	fixture, input := provisionalGlobalOwnershipCreateInput(t)

	result, err := ApplyWithOptions(t.Context(), input, ApplyOptions{})
	if err != nil {
		t.Fatalf("ApplyWithOptions returned error: %v", err)
	}
	assertCommittedApplyResult(t, result, input.Paths.StatefilePath, input.Paths.StatefilePath, 1)
	claim := requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath)
	if claim.State() != ownership.ClaimActive || claim.OperationID() != "" {
		t.Fatalf("claim = %#v, want active exact claim without operation id", claim)
	}
	if _, err := os.Stat(fixture.hostConfigPath); err != nil {
		t.Fatalf("host config stat returned error: %v", err)
	}
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestProvisionalGlobalOwnershipRejectsAliasOfClaimThatBecomesCurrent(t *testing.T) {
	fixture, input := provisionalGlobalOwnershipCreateInput(t)
	desiredHome := filepath.Dir(fixture.hostConfigPath)
	aliasHome := filepath.Join(fixture.root, "Home\u0301")
	aliasConfig := filepath.Join(aliasHome, filepath.Base(fixture.hostConfigPath))

	if err := os.MkdirAll(aliasHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(desiredHome); err != nil {
		if os.IsNotExist(err) {
			t.Skip("temporary filesystem does not resolve the tested normalization alias")
		}
		t.Fatal(err)
	}
	desiredAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(fixture.hostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	aliasAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(aliasConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !aliasAuthority.Exact().Equal(desiredAuthority.Exact()) {
		t.Fatalf(
			"normalization aliases resolved to different exact authorities: desired=%#v alias=%#v",
			desiredAuthority.Exact(),
			aliasAuthority.Exact(),
		)
	}

	address, err := ownership.NewManagedAddress(
		aliasAuthority.Exact(),
		string(input.Ownership[0].ContentPath()),
	)
	if err != nil {
		t.Fatal(err)
	}
	foreignOwner, err := stateauthority.New(
		mustObservedPathAuthority(t, filepath.Join(fixture.root, "foreign", ".daem", "state.json")),
		filepath.Join(fixture.root, "foreign", "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	oldClaim, err := ownership.NewActiveClaim(address, foreignOwner)
	if err != nil {
		t.Fatal(err)
	}
	oldClaimValue, _ := ownership.PresentClaim(oldClaim)
	registryStore, err := ownershipstore.New(input.Paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registryStore.Apply(
		t.Context(),
		address,
		ownership.NoClaim(),
		oldClaimValue,
	); err != nil {
		t.Fatalf("seed old alias claim: %v", err)
	}
	_, err = ApplyWithOptions(t.Context(), input, ApplyOptions{})
	var stale *ownership.StaleClaimError
	if !errors.As(err, &stale) {
		t.Fatalf("ApplyWithOptions error = %v, want stale claim after alias became current", err)
	}
	assertHostMissing(t, fixture.hostConfigPath)
	assertHostMissing(t, aliasConfig)
	registry, err := registryStore.Load(t.Context())
	if err != nil {
		t.Fatalf("load old claim after rollback: %v", err)
	}
	claims := registry.Claims()
	if len(claims) != 1 || !claims[0].Equal(oldClaim) {
		t.Fatalf("claims after rollback = %#v, want only old claim %#v", claims, oldClaim)
	}
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestProvisionalGlobalOwnershipRecoversUnpromotedVisibleOutput(t *testing.T) {
	fixture, input := provisionalGlobalOwnershipCreateInput(t)
	visibilityAccepts := 0
	crash := errors.New("injected crash before ownership intent promotion")

	_, err := ApplyWithOptions(t.Context(), input, ApplyOptions{
		AcceptVisibilityChanges: func(context.Context) error {
			visibilityAccepts++
			if visibilityAccepts == 2 {
				return crash
			}
			return nil
		},
		ValidateCompensationAuthority: unavailableCompensation,
	})
	if !errors.Is(err, crash) || !strings.Contains(err.Error(), "recovery journal retained") {
		t.Fatalf("ApplyWithOptions error = %v, want retained unpromoted journal", err)
	}
	if _, err := os.Stat(fixture.hostConfigPath); err != nil {
		t.Fatalf("visible host config stat returned error: %v", err)
	}
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	plan := requireProvisionalRecoveryPlan(t, input.Paths, recovery.ClassificationNeedsRollback, 0)
	recoverProvisionalGlobalOwnership(t, plan, input.Paths)
	assertHostMissing(t, fixture.hostConfigPath)
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestProvisionalGlobalOwnershipRecoversPromotedIntentBeforeReservation(t *testing.T) {
	fixture, input := provisionalGlobalOwnershipCreateInput(t)
	visibilityAccepts := 0
	crash := errors.New("injected crash after ownership intent promotion")

	_, err := ApplyWithOptions(t.Context(), input, ApplyOptions{
		AcceptVisibilityChanges: func(context.Context) error {
			visibilityAccepts++
			if visibilityAccepts == 3 {
				return crash
			}
			return nil
		},
		ValidateCompensationAuthority: unavailableCompensation,
	})
	if !errors.Is(err, crash) || !strings.Contains(err.Error(), "recovery journal retained") {
		t.Fatalf("ApplyWithOptions error = %v, want retained promoted journal", err)
	}
	if _, err := os.Stat(fixture.hostConfigPath); err != nil {
		t.Fatalf("visible host config stat returned error: %v", err)
	}
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	plan := requireProvisionalRecoveryPlan(t, input.Paths, recovery.ClassificationNeedsRollback, 1)
	recoverProvisionalGlobalOwnership(t, plan, input.Paths)
	assertHostMissing(t, fixture.hostConfigPath)
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestProvisionalGlobalOwnershipRecoversReservedClaimBeforeStateCommit(t *testing.T) {
	fixture, input := provisionalGlobalOwnershipCreateInput(t)
	ctx, cancel := context.WithCancel(context.Background())

	_, err := ApplyWithOptions(ctx, input, ApplyOptions{
		Events: func(event Event) {
			if event.Kind == EventActionDone {
				cancel()
			}
		},
		ValidateCompensationAuthority: unavailableCompensation,
	})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "recovery journal retained") {
		t.Fatalf("ApplyWithOptions error = %v, want retained reserved-claim journal", err)
	}
	if _, err := os.Stat(fixture.hostConfigPath); err != nil {
		t.Fatalf("visible host config stat returned error: %v", err)
	}
	claim := requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath)
	if claim.State() != ownership.ClaimReserved {
		t.Fatalf("claim state = %q, want reserved", claim.State())
	}
	plan := requireProvisionalRecoveryPlan(t, input.Paths, recovery.ClassificationNeedsRollback, 1)
	recoverProvisionalGlobalOwnership(t, plan, input.Paths)
	assertHostMissing(t, fixture.hostConfigPath)
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestProvisionalGlobalOwnershipRecoversCommittedStateByFinalizingClaim(t *testing.T) {
	fixture, input := provisionalGlobalOwnershipCreateInput(t)
	ctx, cancel := context.WithCancel(context.Background())

	_, err := ApplyWithOptions(ctx, input, ApplyOptions{Events: func(event Event) {
		if event.Kind == EventStatefileWritten {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}
	claim := requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath)
	if claim.State() != ownership.ClaimReserved {
		t.Fatalf("claim state = %q, want reserved", claim.State())
	}
	plan := requireProvisionalRecoveryPlan(t, input.Paths, recovery.ClassificationNeedsFinalize, 1)
	recoverProvisionalGlobalOwnership(t, plan, input.Paths)
	claim = requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath)
	if claim.State() != ownership.ClaimActive || claim.OperationID() != "" {
		t.Fatalf("finalized claim = %#v, want active without operation id", claim)
	}
	if _, err := os.Stat(fixture.hostConfigPath); err != nil {
		t.Fatalf("host config stat returned error: %v", err)
	}
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestProvisionalGlobalOwnershipRecoversFinalizedClaimByRetiringJournal(t *testing.T) {
	fixture, input := provisionalGlobalOwnershipCreateInput(t)
	ctx, cancel := context.WithCancel(context.Background())

	_, err := ApplyWithOptions(ctx, input, ApplyOptions{Events: func(event Event) {
		if event.Kind == EventJournalCleanupStarted {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}
	claim := requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath)
	if claim.State() != ownership.ClaimActive {
		t.Fatalf("claim state = %q, want active", claim.State())
	}
	plan := requireProvisionalRecoveryPlan(t, input.Paths, recovery.ClassificationCleanAfter, 1)
	recoverProvisionalGlobalOwnership(t, plan, input.Paths)
	if _, err := os.Stat(fixture.hostConfigPath); err != nil {
		t.Fatalf("host config stat returned error: %v", err)
	}
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func provisionalGlobalOwnershipCreateInput(t *testing.T) (mcpProjectionApplyFixture, ApplyInput) {
	t.Helper()
	fixture, input := globalAggregateOwnershipInputAtHome(t, false, "Hom\u00e9")
	if len(input.Ownership) != 1 {
		t.Fatalf("ownership observations = %d, want one", len(input.Ownership))
	}
	if _, provisional := input.Ownership[0].ProvisionalPath(); !provisional {
		t.Fatal("normalization-sensitive missing HOME did not produce provisional ownership")
	}
	return fixture, input
}

func unavailableCompensation(context.Context) error {
	return errors.New("injected unavailable immediate compensation")
}

func requireProvisionalRecoveryPlan(
	t *testing.T,
	paths Paths,
	wantClassification recovery.Classification,
	wantTransitions int,
) recovery.Plan {
	t.Helper()
	plan, err := loadActivePlanWithTestCodecs(t.Context(), paths)
	if err != nil {
		t.Fatalf("LoadActivePlan returned error: %v", err)
	}
	if plan.Classification() != wantClassification {
		t.Fatalf("classification = %q, want %q", plan.Classification(), wantClassification)
	}
	if transitions := plan.ClaimTransitions(); len(transitions) != wantTransitions {
		t.Fatalf("claim transitions = %d, want %d", len(transitions), wantTransitions)
	}
	return plan
}

func recoverProvisionalGlobalOwnership(t *testing.T, plan recovery.Plan, paths Paths) {
	t.Helper()
	if err := executeRecoveryPlanWithOptionsForTest(
		t.Context(),
		plan,
		paths,
		RecoveryOptions{
			Resolver:                destinationResolver(paths),
			Codecs:                  testAggregateCodecs(),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(paths.StatefilePath),
			Filesystem:              testFilesystem(),
		},
	); err != nil {
		t.Fatalf("ExecuteRecoveryPlan returned error: %v", err)
	}
}
