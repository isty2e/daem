//go:build darwin

package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
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

func TestGlobalOwnershipRemovalFinalizesDeletedNonASCIIPath(t *testing.T) {
	hostPath, input := globalNonASCIIManagedPathRemovalInput(t)

	result, err := ApplyWithOptions(t.Context(), input, ApplyOptions{})
	if err != nil {
		t.Fatalf("ApplyWithOptions returned error: %v", err)
	}
	assertCommittedApplyResult(t, result, input.Paths.StatefilePath, input.Paths.StatefilePath, 1)
	assertHostMissing(t, hostPath)
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestGlobalOwnershipRemovalRecoversAfterDeletedNonASCIIPath(t *testing.T) {
	hostPath, input := globalNonASCIIManagedPathRemovalInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ApplyWithOptions(ctx, input, ApplyOptions{Events: func(event Event) {
		if event.Kind == EventStatefileWritten {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}
	assertHostMissing(t, hostPath)

	plan := requireProvisionalRecoveryPlan(t, input.Paths, recovery.ClassificationNeedsFinalize, 1)
	recoverProvisionalGlobalOwnership(t, plan, input.Paths)
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestGlobalOwnershipRemovalRecoversDeletedUnicodeAlias(t *testing.T) {
	const (
		storedName      = "config-\u00e9"
		destinationName = "config-e\u0301"
	)
	hostPath, aliasPath, input := globalManagedPathRemovalInput(t, destinationName, storedName)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ApplyWithOptions(ctx, input, ApplyOptions{Events: func(event Event) {
		if event.Kind == EventStatefileWritten {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}
	assertHostMissing(t, hostPath)
	assertHostMissing(t, aliasPath)

	plan := requireProvisionalRecoveryPlan(t, input.Paths, recovery.ClassificationNeedsFinalize, 1)
	recoverProvisionalGlobalOwnership(t, plan, input.Paths)
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func globalNonASCIIManagedPathRemovalInput(t *testing.T) (string, ApplyInput) {
	t.Helper()
	const name = "config-\u00e9"
	hostPath, _, input := globalManagedPathRemovalInput(t, name, name)
	return hostPath, input
}

func globalManagedPathRemovalInput(
	t *testing.T,
	destinationName string,
	storedName string,
) (string, string, ApplyInput) {
	t.Helper()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	stateDir := filepath.Join(root, ".daem")
	paths := Paths{
		RecoveryDir:           filepath.Join(stateDir, "recovery"),
		StateDir:              stateDir,
		StatefilePath:         filepath.Join(stateDir, "state.json"),
		ManifestRoot:          root,
		DataDir:               filepath.Join(root, "data"),
		OwnershipRegistryPath: filepath.Join(root, "data", "ownership", "claims.json"),
	}
	destination := outputtest.Parse(t, "~/.agents/skills/"+destinationName)
	hostPath := filepath.Join(home, ".agents", "skills", storedName)
	logicalHostPath := filepath.Join(home, ".agents", "skills", destinationName)
	if err := os.MkdirAll(hostPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostPath, "SKILL.md"), []byte("managed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if destinationName != storedName {
		if _, err := os.Lstat(logicalHostPath); err != nil {
			if os.IsNotExist(err) {
				t.Skip("temporary filesystem does not resolve the tested normalization alias")
			}
			t.Fatal(err)
		}
		storedAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(hostPath)
		if err != nil {
			t.Fatal(err)
		}
		aliasAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(logicalHostPath)
		if err != nil {
			t.Fatal(err)
		}
		if !storedAuthority.Exact().Equal(aliasAuthority.Exact()) {
			t.Skip("temporary filesystem does not bind the tested spellings to one exact authority")
		}
	}
	view, err := access.OpenView(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := view.Hash(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	subject := testManagedPathEffectSubject(t, destinationName, "skill.global.agents")
	previous, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeGlobal,
		destination,
		hash,
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{previous}})
	if err != nil {
		t.Fatal(err)
	}
	writeRecoveryTestStatefile(t, paths.StatefilePath, current)
	owner, err := stateauthority.New(
		mustObservedPathAuthority(t, paths.StatefilePath),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	pathAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(pathAuthority.Exact(), "")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	claimValue, _ := ownership.PresentClaim(claim)
	registryStore, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registryStore.Apply(t.Context(), address, ownership.NoClaim(), claimValue); err != nil {
		t.Fatalf("seed ownership claim: %v", err)
	}
	ownershipObservation, err := observe.NewExactOwnershipObservation(
		destination,
		output.ContentPath(""),
		address,
		claimValue,
	)
	if err != nil {
		t.Fatal(err)
	}
	pathEvidence, err := observe.NewManagedPathEvidence(subject, destination, true, hash, 0)
	if err != nil {
		t.Fatal(err)
	}
	effect := ManagedPathEffect{remove: &managedPathRemoveEffect{facts: managedPathEffectFacts{
		subject: subject, scope: target.ScopeGlobal, destination: destination,
		liveHash: hash, contentKind: realization.PathProjectionDirectory,
		permissionPolicy: realization.PathPermissionsNone,
		previous:         &previous,
	}}}
	if err := effect.validate(); err != nil {
		t.Fatal(err)
	}
	return hostPath, logicalHostPath, ApplyInput{
		Paths: paths, Resolver: destinationResolver(paths),
		ManagedPathEffects: []ManagedPathEffect{effect}, ManagedPathEvidence: []observe.ManagedPathEvidence{pathEvidence},
		CurrentState: current, Owner: owner, Ownership: []observe.OwnershipObservation{ownershipObservation},
		OwnershipRegistryBinder: testOwnershipRegistryBinder(),
		StateCodec:              testStateCodec(),
		Filesystem:              testFilesystem(),
	}
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
	if err == nil || !strings.Contains(err.Error(), "ownership_conflict") {
		t.Fatalf("ApplyWithOptions error = %v, want ownership conflict before effects", err)
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

func TestProvisionalGlobalOwnershipRecoveryRejectsClaimAddedBeforeDelete(t *testing.T) {
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
	if !errors.Is(err, crash) {
		t.Fatalf("ApplyWithOptions error = %v, want injected crash", err)
	}
	plan := requireProvisionalRecoveryPlan(t, input.Paths, recovery.ClassificationNeedsRollback, 0)
	if intents := plan.ProvisionalAcquireIntents(); len(intents) != 1 {
		t.Fatalf("provisional intents = %d, want 1", len(intents))
	}

	var foreignClaim ownership.Claim
	executionErr := executeRecoveryPlanWithOptionsForTest(
		t.Context(),
		plan,
		input.Paths,
		RecoveryOptions{
			Resolver:                destinationResolver(input.Paths),
			Codecs:                  testAggregateCodecs(),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(input.Paths.StatefilePath),
			Filesystem:              testFilesystem(),
			beforeHostAction: func(int) error {
				var seedErr error
				foreignClaim, seedErr = seedForeignClaimForVisibleProvisionalOutput(t, fixture, input)
				return seedErr
			},
		},
	)
	var stale *ownership.StaleClaimError
	if !errors.As(executionErr, &stale) {
		t.Fatalf("ExecuteRecoveryPlan error = %v, want stale ownership claim", executionErr)
	}
	if _, err := os.Stat(fixture.hostConfigPath); err != nil {
		t.Fatalf("foreign-claimed output was removed: %v", err)
	}
	claim := requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath)
	if !claim.Equal(foreignClaim) {
		t.Fatalf("claim after blocked recovery = %#v, want %#v", claim, foreignClaim)
	}
	blocked := requireProvisionalRecoveryPlan(t, input.Paths, recovery.ClassificationBlocked, 0)
	if !blocked.HasErrors() {
		t.Fatal("foreign claim did not block a freshly loaded recovery plan")
	}
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

func TestProvisionalGlobalOwnershipRecoveryResumesAfterOutputRollbackBeforeClaimRollback(t *testing.T) {
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
	claim := requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath)
	if claim.State() != ownership.ClaimReserved {
		t.Fatalf("claim state = %q, want reserved", claim.State())
	}
	if err := os.RemoveAll(fixture.hostConfigPath); err != nil {
		t.Fatalf("simulate committed recovery host rollback: %v", err)
	}
	assertHostMissing(t, fixture.hostConfigPath)

	plan := requireProvisionalRecoveryPlan(t, input.Paths, recovery.ClassificationNeedsRollback, 1)
	recoverProvisionalGlobalOwnership(t, plan, input.Paths)
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

func seedForeignClaimForVisibleProvisionalOutput(
	t *testing.T,
	fixture mcpProjectionApplyFixture,
	input ApplyInput,
) (ownership.Claim, error) {
	t.Helper()
	pathAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(fixture.hostConfigPath)
	if err != nil {
		return ownership.Claim{}, err
	}
	address, err := ownership.NewManagedAddress(
		pathAuthority.Exact(),
		string(input.Ownership[0].ContentPath()),
	)
	if err != nil {
		return ownership.Claim{}, err
	}
	owner, err := stateauthority.New(
		mustObservedPathAuthority(t, filepath.Join(fixture.root, "foreign-claim", ".daem", "state.json")),
		filepath.Join(fixture.root, "foreign-claim", "daem.toml"),
	)
	if err != nil {
		return ownership.Claim{}, err
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		return ownership.Claim{}, err
	}
	value, err := ownership.PresentClaim(claim)
	if err != nil {
		return ownership.Claim{}, err
	}
	registryStore, err := ownershipstore.New(input.Paths.OwnershipRegistryPath)
	if err != nil {
		return ownership.Claim{}, err
	}
	if _, err := registryStore.Apply(t.Context(), address, ownership.NoClaim(), value); err != nil {
		return ownership.Claim{}, err
	}
	return claim, nil
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
