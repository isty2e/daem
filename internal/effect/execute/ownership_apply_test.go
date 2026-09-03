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
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func bindNilOwnershipRegistryStore(
	*rootedpath.CapturedRoot,
	rootedpath.Destination,
	int,
	rootedpath.PhysicalTraversalBudget,
) (ownershipmutation.RegistryStore, error) {
	return nil, nil
}

func TestGlobalOwnershipApplyRequiresRegistryBinderBeforeDurableEffects(t *testing.T) {
	fixture, input := globalOwnershipCreateInput(t)
	input.OwnershipRegistryBinder = nil

	_, err := ApplyWithOptions(context.Background(), input, ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "apply ownership registry binder is required") {
		t.Fatalf("ApplyWithOptions error = %v, want missing binder refusal", err)
	}
	assertHostMissing(t, fixture.hostConfigPath)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
	if _, statErr := os.Stat(input.Paths.OwnershipRegistryPath); !os.IsNotExist(statErr) {
		t.Fatalf("ownership registry stat error = %v, want absent", statErr)
	}
	if _, statErr := os.Stat(input.Paths.StatefilePath); !os.IsNotExist(statErr) {
		t.Fatalf("statefile stat error = %v, want absent", statErr)
	}
}

func TestGlobalOwnershipApplyRejectsNilStoreFromRegistryBinder(t *testing.T) {
	fixture, input := globalOwnershipCreateInput(t)
	input.OwnershipRegistryBinder = bindNilOwnershipRegistryStore

	_, err := ApplyWithOptions(context.Background(), input, ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "binder returned a nil store") {
		t.Fatalf("ApplyWithOptions error = %v, want nil-store refusal", err)
	}
	assertHostMissing(t, fixture.hostConfigPath)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
	if _, statErr := os.Stat(input.Paths.OwnershipRegistryPath); !os.IsNotExist(statErr) {
		t.Fatalf("ownership registry stat error = %v, want absent", statErr)
	}
}

func TestGlobalOwnershipRecoveryRequiresReaderAndBinderBeforeEffects(t *testing.T) {
	fixture, input := globalOwnershipCreateInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ApplyWithOptions(ctx, input, ApplyOptions{Events: func(event Event) {
		if event.Kind == EventStatefileWritten {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}

	_, err = journal.LoadActivePlanWithOptions(
		context.Background(),
		input.Paths.journalPaths(),
		journal.PlanLoadOptions{
			Filesystem:  testFilesystem(),
			Resolver:    destinationResolver(input.Paths),
			Codecs:      testAggregateCodecs(),
			StateCodec:  testStateCodec(),
			StateReader: testStateReader(input.Paths.StatefilePath),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "ownership registry reader is required") {
		t.Fatalf("LoadActivePlanWithOptions error = %v, want missing reader refusal", err)
	}
	recoveryPlan, err := loadActivePlanWithTestCodecs(context.Background(), input.Paths)
	if err != nil {
		t.Fatalf("LoadActivePlanWithOptions with reader returned error: %v", err)
	}
	err = executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		recoveryPlan,
		input.Paths,
		RecoveryOptions{
			Resolver:    destinationResolver(input.Paths),
			Codecs:      testAggregateCodecs(),
			StateCodec:  testStateCodec(),
			StateReader: testStateReader(input.Paths.StatefilePath),
			Filesystem:  testFilesystem(),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "recovery ownership registry binder is required") {
		t.Fatalf("ExecuteRecoveryPlanWithOptions error = %v, want missing binder refusal", err)
	}
	if claim := requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath); claim.State() != ownership.ClaimReserved {
		t.Fatalf("claim state = %q, want reserved", claim.State())
	}
	if _, statErr := os.Stat(fixture.hostConfigPath); statErr != nil {
		t.Fatalf("host config stat returned error after binder refusal: %v", statErr)
	}
	if err := journal.RequireNoInterruptedApply(t.Context(), input.Paths.RecoveryDir); err == nil {
		t.Fatal("missing recovery binder removed active recovery evidence")
	}
}

func TestManagedPathOwnershipRelocationTreatsOldAndNewLocalityIndependently(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	owner, err := stateauthority.New(mustObservedPathAuthority(t, filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	projectState := testManagedPathEffectState(t, "oracle", outputtest.Parse(t, ".agents/skills/oracle"))
	globalState, err := durable.NewManagedPathState(
		projectState.Subject(),
		projectState.ConsumerTargets(),
		target.ScopeGlobal,
		outputtest.Parse(t, "~/global-old"),
		projectState.ContentHash(),
		projectState.ContentKind(),
		projectState.PermissionPolicy(),
		projectState.FileMode(),
	)
	if err != nil {
		t.Fatal(err)
	}
	oldGlobal := managedPathOwnershipObservation(t, filepath.Join(root, "global-old"), globalState.Destination(), owner, true)
	newGlobal := managedPathOwnershipObservation(t, filepath.Join(root, "global-new"), outputtest.Parse(t, "~/global-new"), owner, false)

	tests := []struct {
		name         string
		previous     durable.ManagedPathState
		scope        target.Scope
		destination  output.Destination
		observations []observe.OwnershipObservation
		wantKinds    []ownershipmutation.TransitionKind
	}{
		{
			name: "project to global acquires only", previous: projectState,
			scope: target.ScopeGlobal, destination: outputtest.Parse(t, "~/global-new"),
			observations: []observe.OwnershipObservation{newGlobal},
			wantKinds:    []ownershipmutation.TransitionKind{ownershipmutation.TransitionAcquire},
		},
		{
			name: "global to project releases only", previous: globalState,
			scope: target.ScopeProject, destination: outputtest.Parse(t, ".agents/skills/oracle"),
			observations: []observe.OwnershipObservation{oldGlobal},
			wantKinds:    []ownershipmutation.TransitionKind{ownershipmutation.TransitionRelease},
		},
		{
			name: "global to global releases then acquires", previous: globalState,
			scope: target.ScopeGlobal, destination: outputtest.Parse(t, "~/global-new"),
			observations: []observe.OwnershipObservation{oldGlobal, newGlobal},
			wantKinds:    []ownershipmutation.TransitionKind{ownershipmutation.TransitionRelease, ownershipmutation.TransitionAcquire},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previous := test.previous
			effect := ManagedPathEffect{replace: &managedPathReplaceEffect{facts: managedPathEffectFacts{
				subject: previous.Subject(), consumerTargets: previous.ConsumerTargets(),
				scope: test.scope, destination: test.destination,
				desiredHash: testArtifactHash("new"), contentKind: previous.ContentKind(), previous: &previous,
			}}}
			plan, err := ownershipPlanForManagedPathEffects(
				[]ManagedPathEffect{effect}, owner, test.observations, "operation-1",
			)
			if err != nil {
				t.Fatalf("ownershipPlanForManagedPathEffects returned error: %v", err)
			}
			transitions := plan.transitions
			if len(transitions) != len(test.wantKinds) {
				t.Fatalf("transitions = %#v, want kinds %#v", transitions, test.wantKinds)
			}
			for index, want := range test.wantKinds {
				if transitions[index].Kind() != want {
					t.Fatalf("transition[%d].Kind() = %q, want %q", index, transitions[index].Kind(), want)
				}
			}
		})
	}
}

func managedPathOwnershipObservation(
	t *testing.T,
	path string,
	destination output.Destination,
	owner stateauthority.Authority,
	claimed bool,
) observe.OwnershipObservation {
	t.Helper()
	address, err := ownership.NewManagedAddress(mustObservedPathAuthority(t, path), "")
	if err != nil {
		t.Fatal(err)
	}
	claim := ownership.NoClaim()
	if claimed {
		active, err := ownership.NewActiveClaim(address, owner)
		if err != nil {
			t.Fatal(err)
		}
		claim, _ = ownership.PresentClaim(active)
	}
	observation, err := observe.NewExactOwnershipObservation(destination, "", address, claim)
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func TestGlobalOwnershipCancellationAfterReservationRestoresAbsentClaim(t *testing.T) {
	fixture, input := globalOwnershipCreateInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ApplyWithOptions(ctx, input, ApplyOptions{Events: func(event Event) {
		if event.Kind == EventRollbackStageStarted {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	assertHostMissing(t, fixture.hostConfigPath)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestGlobalOwnershipPreparationFailureSettlesClaimAndJournal(t *testing.T) {
	fixture, input := globalOwnershipCreateInput(t)
	wantErr := errors.New("injected ownership preparation validation failure")
	validationCalls := 0

	_, err := ApplyWithOptions(context.Background(), input, ApplyOptions{
		ValidateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
			validationCalls++
			if validationCalls == 2 {
				return wantErr
			}
			return nil
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ApplyWithOptions error = %v, want ownership preparation failure", err)
	}
	if strings.Contains(err.Error(), "failure settlement") {
		t.Fatalf("ApplyWithOptions error exposed cursor settlement failure: %v", err)
	}
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	assertHostMissing(t, fixture.hostConfigPath)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestGlobalOwnershipPreparationRollbackFailureRetainsJournalAndPrimaryError(t *testing.T) {
	fixture, input := globalOwnershipCreateInput(t)
	primary := errors.New("injected ownership preparation validation failure")
	rollback := errors.New("injected ownership rollback validation failure")
	validationCalls := 0

	_, err := ApplyWithOptions(context.Background(), input, ApplyOptions{
		ValidateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
			validationCalls++
			if validationCalls == 2 {
				return primary
			}
			return nil
		},
		ValidateCompensationAuthority: func(context.Context) error {
			return rollback
		},
	})
	if !errors.Is(err, primary) {
		t.Fatalf("ApplyWithOptions error = %v, want primary preparation failure", err)
	}
	if !strings.Contains(err.Error(), rollback.Error()) ||
		!strings.Contains(err.Error(), "recovery journal retained") {
		t.Fatalf("ApplyWithOptions error = %v, want rollback failure and recovery guidance", err)
	}
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	assertHostMissing(t, fixture.hostConfigPath)
	assertActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestGlobalOwnershipCancellationAfterHostActionRollsBackHostAndClaim(t *testing.T) {
	fixture, input := globalOwnershipCreateInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ApplyWithOptions(ctx, input, ApplyOptions{Events: func(event Event) {
		if event.Kind == EventActionDone {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	assertHostMissing(t, fixture.hostConfigPath)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestGlobalOwnershipCancellationAfterStateCommitRecoversByFinalizingClaim(t *testing.T) {
	fixture, input := globalOwnershipCreateInput(t)
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
	if _, err := os.Stat(fixture.hostConfigPath); err != nil {
		t.Fatalf("host config stat returned error: %v", err)
	}
	if _, err := statefile.Load(t.Context(), input.Paths.StatefilePath); err != nil {
		t.Fatalf("statefile load returned error: %v", err)
	}

	recoveryPlan, err := loadActivePlanWithTestCodecs(context.Background(), input.Paths)
	if err != nil {
		t.Fatalf("LoadActivePlan returned error: %v", err)
	}
	if recoveryPlan.Classification() != recovery.ClassificationNeedsFinalize {
		t.Fatalf("classification = %q, want needs_finalize", recoveryPlan.Classification())
	}
	if err := executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		recoveryPlan,
		input.Paths,
		RecoveryOptions{
			Resolver:                destinationResolver(input.Paths),
			Codecs:                  testAggregateCodecs(),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(input.Paths.StatefilePath),
			Filesystem:              testFilesystem(),
		},
	); err != nil {
		t.Fatalf("ExecuteRecoveryPlan returned error: %v", err)
	}
	claim = requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath)
	if claim.State() != ownership.ClaimActive || claim.OperationID() != "" {
		t.Fatalf("finalized claim = %#v, want active without operation id", claim)
	}
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestGlobalOwnershipRecoveryRejectsUnadmittedSuccessorAfterConvergence(t *testing.T) {
	fixture, input := globalOwnershipCreateInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ApplyWithOptions(ctx, input, ApplyOptions{Events: func(event Event) {
		if event.Kind == EventStatefileWritten {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}
	recoveryPlan, err := loadActivePlanWithTestCodecs(context.Background(), input.Paths)
	if err != nil {
		t.Fatalf("LoadActivePlan returned error: %v", err)
	}
	if recoveryPlan.Classification() != recovery.ClassificationNeedsFinalize {
		t.Fatalf("classification = %q, want needs_finalize", recoveryPlan.Classification())
	}

	prepared := requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath)
	unrelatedAddress, err := ownership.NewManagedAddress(
		mustObservedPathAuthority(t, fixture.hostConfigPath+".unrelated"),
		"",
	)
	if err != nil {
		t.Fatalf("construct unrelated ownership address: %v", err)
	}
	unrelatedClaim, err := ownership.NewActiveClaim(unrelatedAddress, prepared.Owner())
	if err != nil {
		t.Fatalf("construct unrelated ownership claim: %v", err)
	}
	unrelatedValue, _ := ownership.PresentClaim(unrelatedClaim)
	store, err := ownershipstore.New(input.Paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatalf("construct ownership store: %v", err)
	}
	acceptedEffects := 0

	err = executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		recoveryPlan,
		input.Paths,
		RecoveryOptions{
			Resolver:                destinationResolver(input.Paths),
			Codecs:                  testAggregateCodecs(),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(input.Paths.StatefilePath),
			Filesystem:              testFilesystem(),
			AcceptVisibilityChanges: func(context.Context) error {
				acceptedEffects++
				if acceptedEffects != 1 {
					return nil
				}
				_, applyErr := store.Apply(
					context.Background(),
					unrelatedAddress,
					ownership.NoClaim(),
					unrelatedValue,
				)
				return applyErr
			},
		},
	)
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteRecoveryPlan error = %v, want stale ownership successor", err)
	}
	if acceptedEffects != 1 {
		t.Fatalf("accepted recovery effects = %d, want 1", acceptedEffects)
	}
	registry, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load mutated ownership registry: %v", err)
	}
	if claims := registry.Claims(); len(claims) != 2 {
		t.Fatalf("ownership claims = %#v, want finalized plus unrelated", claims)
	}
	assertRecoveryJournalRetained(t, recoveryPlan)
}

func TestGlobalOwnershipReleaseRemainsActiveUntilRecoveryFinalizesRemoval(t *testing.T) {
	_, input := globalOwnershipRemoveInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ApplyWithOptions(ctx, input, ApplyOptions{Events: func(event Event) {
		if event.Kind == EventStatefileWritten {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}
	if claim := requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath); claim.State() != ownership.ClaimActive {
		t.Fatalf("release claim state = %q, want active before finalization", claim.State())
	}

	recoveryPlan, err := loadActivePlanWithTestCodecs(context.Background(), input.Paths)
	if err != nil {
		t.Fatalf("LoadActivePlan returned error: %v", err)
	}
	if recoveryPlan.Classification() != recovery.ClassificationNeedsFinalize {
		t.Fatalf("classification = %q, want needs_finalize", recoveryPlan.Classification())
	}
	if err := executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		recoveryPlan,
		input.Paths,
		RecoveryOptions{
			Resolver:                destinationResolver(input.Paths),
			Codecs:                  testAggregateCodecs(),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(input.Paths.StatefilePath),
			Filesystem:              testFilesystem(),
		},
	); err != nil {
		t.Fatalf("ExecuteRecoveryPlan returned error: %v", err)
	}
	assertOwnershipRegistryClaimCount(t, input.Paths.OwnershipRegistryPath, 0)
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestGlobalOwnershipCancellationAfterFinalizationLeavesCleanAfterJournal(t *testing.T) {
	_, input := globalOwnershipCreateInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	result, err := ApplyWithOptions(ctx, input, ApplyOptions{Events: func(event Event) {
		if event.Kind == EventJournalCleanupStarted {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation during cleanup", err)
	}
	assertCommittedApplyResult(t, result, input.Paths.StatefilePath, input.Paths.StatefilePath, 1)
	if claim := requireOnlyOwnershipClaim(t, input.Paths.OwnershipRegistryPath); claim.State() != ownership.ClaimActive {
		t.Fatalf("claim state = %q, want finalized active", claim.State())
	}
	recoveryPlan, err := loadActivePlanWithTestCodecs(context.Background(), input.Paths)
	if err != nil {
		t.Fatalf("LoadActivePlan returned error: %v", err)
	}
	if recoveryPlan.Classification() != recovery.ClassificationCleanAfter {
		t.Fatalf("classification = %q, want clean_after", recoveryPlan.Classification())
	}
	if err := executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		recoveryPlan,
		input.Paths,
		RecoveryOptions{
			Resolver:                destinationResolver(input.Paths),
			Codecs:                  testAggregateCodecs(),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(input.Paths.StatefilePath),
			Filesystem:              testFilesystem(),
		},
	); err != nil {
		t.Fatalf("ExecuteRecoveryPlan returned error: %v", err)
	}
	assertNoActiveRecoveryOperation(t, input.Paths.RecoveryDir)
}

func TestGlobalOwnershipContradictoryFinalizeEvidenceBlocksAndPreservesUnrelatedClaims(t *testing.T) {
	_, input := globalOwnershipCreateInput(t)
	store, err := ownershipstore.New(input.Paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatalf("ownership store New returned error: %v", err)
	}
	var tamperErr error
	result, err := ApplyWithOptions(context.Background(), input, ApplyOptions{Events: func(event Event) {
		if event.Kind != EventStatefileWritten || tamperErr != nil {
			return
		}
		registry, loadErr := store.Load(context.Background())
		if loadErr != nil {
			tamperErr = loadErr
			return
		}
		claims := registry.Claims()
		if len(claims) != 1 {
			tamperErr = errors.New("expected one prepared claim")
			return
		}
		prepared, _ := ownership.PresentClaim(claims[0])
		_, tamperErr = store.Apply(context.Background(), claims[0].Address(), prepared, ownership.NoClaim())
	}})
	if tamperErr != nil {
		t.Fatalf("tamper ownership claim returned error: %v", tamperErr)
	}
	if err == nil || !strings.Contains(err.Error(), "finalize ownership claim") {
		t.Fatalf("ApplyWithOptions error = %v, want finalize failure", err)
	}
	assertCommittedApplyResult(t, result, input.Paths.StatefilePath, input.Paths.StatefilePath, 1)
	blocked, err := loadActivePlanWithTestCodecs(context.Background(), input.Paths)
	if err != nil {
		t.Fatalf("LoadActivePlan returned error: %v", err)
	}
	if blocked.Classification() != recovery.ClassificationBlocked {
		t.Fatalf("classification = %q, want blocked", blocked.Classification())
	}

	transition := blocked.ClaimTransitions()[0]
	if _, err := store.Apply(context.Background(), transition.Address(), ownership.NoClaim(), transition.Prepared()); err != nil {
		t.Fatalf("restore prepared claim returned error: %v", err)
	}
	unrelatedAddress, err := ownership.NewManagedAddress(mustObservedPathAuthority(t, transition.Address().Path()+".unrelated"), "")
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	unrelatedClaim, err := ownership.NewActiveClaim(unrelatedAddress, transition.Owner())
	if err != nil {
		t.Fatalf("NewActiveClaim returned error: %v", err)
	}
	unrelatedValue, _ := ownership.PresentClaim(unrelatedClaim)
	if _, err := store.Apply(context.Background(), unrelatedAddress, ownership.NoClaim(), unrelatedValue); err != nil {
		t.Fatalf("write unrelated claim returned error: %v", err)
	}

	finalizable, err := loadActivePlanWithTestCodecs(context.Background(), input.Paths)
	if err != nil {
		t.Fatalf("reload active plan returned error: %v", err)
	}
	if finalizable.Classification() != recovery.ClassificationNeedsFinalize {
		t.Fatalf("classification = %q, want needs_finalize", finalizable.Classification())
	}
	if err := executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		finalizable,
		input.Paths,
		RecoveryOptions{
			Resolver:                destinationResolver(input.Paths),
			Codecs:                  testAggregateCodecs(),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(input.Paths.StatefilePath),
			Filesystem:              testFilesystem(),
		},
	); err != nil {
		t.Fatalf("ExecuteRecoveryPlan returned error: %v", err)
	}
	registry, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("ownership store Load returned error: %v", err)
	}
	if claims := registry.Claims(); len(claims) != 2 {
		t.Fatalf("ownership claims = %#v, want finalized plus unrelated", claims)
	}
}

func globalOwnershipCreateInput(t *testing.T) (mcpProjectionApplyFixture, ApplyInput) {
	t.Helper()
	return globalAggregateOwnershipInput(t, false)
}

func globalOwnershipRemoveInput(t *testing.T) (mcpProjectionApplyFixture, ApplyInput) {
	t.Helper()
	return globalAggregateOwnershipInput(t, true)
}

func globalAggregateOwnershipInput(
	t *testing.T,
	removing bool,
) (mcpProjectionApplyFixture, ApplyInput) {
	return globalAggregateOwnershipInputAtHome(t, removing, "home")
}

func globalAggregateOwnershipInputAtHome(
	t *testing.T,
	removing bool,
	homeComponent string,
) (mcpProjectionApplyFixture, ApplyInput) {
	t.Helper()

	fixture := newClaudeGlobalMCPProjectionApplyFixtureAtHome(t, homeComponent)
	fixture.paths.DataDir = filepath.Join(fixture.root, "data")
	fixture.paths.OwnershipRegistryPath = filepath.Join(fixture.paths.DataDir, "ownership", "claims.json")

	canonical := fixture.claudeGlobalCanonicalEntry(t, "context7", "npx")
	lockedContract := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
		PlacementID:         aggregate.MCPPlacementClaudeGlobal,
		ServerID:            "context7",
		LauncherCommand:     "npx",
		CanonicalProjection: string(canonical),
	})
	contribution, present, err := lockedContract.ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatalf("ManagedAggregateContribution = %#v, %t, %v", contribution, present, err)
	}
	locked, err := lock.NewLockedSection([]lock.LockedSubjectContract{lockedContract}, nil)
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	expected := []lock.LockedSubjectContract{lockedContract}

	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{
		contribution.Contribution().Contract(),
	})
	if err != nil {
		t.Fatalf("NewSelection returned error: %v", err)
	}
	codec, admitted := testAggregateCodecs().Lookup(selection.CodecContractID())
	if !admitted {
		t.Fatalf("codec %q is not admitted", selection.CodecContractID())
	}

	before := aggregate.AbsentDocument()
	fileMode := os.FileMode(0)
	desired := []aggregate.SubjectContribution{contribution}
	var states []durable.ManagedAggregateState
	currentState := durable.EmptySnapshot()
	if removing {
		existing, mergeErr := mergeMCPPlacementCanonicalEntry(
			t,
			aggregate.MCPPlacementClaudeGlobal,
			nil,
			"context7",
			canonical,
		)
		if mergeErr != nil {
			t.Fatalf("MergeCanonicalEntry returned error: %v", mergeErr)
		}
		fixture.writeMCPConfig(t, existing)
		before = aggregate.ExistingDocument(existing)
		fileMode = aggregate.DocumentFileMode
		desired = nil
		expected = nil
		locked, err = lock.NewLockedSection(nil, nil)
		if err != nil {
			t.Fatalf("empty NewLockedSection returned error: %v", err)
		}

		managedState, stateErr := durable.NewManagedAggregateState(
			contribution.SubjectID(),
			contribution.Contribution(),
		)
		if stateErr != nil {
			t.Fatalf("NewManagedAggregateState returned error: %v", stateErr)
		}
		states = []durable.ManagedAggregateState{managedState}
		currentState, stateErr = durable.NewSnapshot(durable.SnapshotInput{
			ManagedAggregates: states,
		})
		if stateErr != nil {
			t.Fatalf("NewSnapshot returned error: %v", stateErr)
		}
		fixture.writeStatefile(t, currentState)
	}

	snapshot, failure := codec.Read(before, selection)
	if failure != nil {
		t.Fatalf("codec Read returned failure: %v", failure)
	}
	evidence, err := observe.NewAggregateEvidence(before, snapshot, fileMode)
	if err != nil {
		t.Fatalf("NewAggregateEvidence returned error: %v", err)
	}
	statefileKey, err := mutation.CanonicalDirectoryEntryKey(fixture.paths.StatefilePath)
	if err != nil {
		t.Fatalf("canonicalize statefile authority: %v", err)
	}
	owner, err := stateauthority.New(
		mustObservedPathAuthority(t, statefileKey),
		filepath.Join(fixture.root, "daem.toml"),
	)
	if err != nil {
		t.Fatalf("stateauthority.New returned error: %v", err)
	}
	claimValue := ownership.NoClaim()
	pathObservation, err := mutation.ObserveDirectoryEntryAuthority(fixture.hostConfigPath)
	if err != nil {
		t.Fatalf("observe aggregate destination: %v", err)
	}
	var ownershipObservation observe.OwnershipObservation
	if exact, present := pathObservation.Exact(); present {
		managedAddress, addressErr := ownership.NewManagedAddress(
			exact,
			contribution.Contribution().ContentPath(),
		)
		if addressErr != nil {
			t.Fatalf("NewManagedAddress returned error: %v", addressErr)
		}
		if removing {
			active, claimErr := ownership.NewActiveClaim(managedAddress, owner)
			if claimErr != nil {
				t.Fatalf("NewActiveClaim returned error: %v", claimErr)
			}
			claimValue, _ = ownership.PresentClaim(active)
			store, storeErr := ownershipstore.New(fixture.paths.OwnershipRegistryPath)
			if storeErr != nil {
				t.Fatalf("ownership store New returned error: %v", storeErr)
			}
			if _, storeErr := store.Apply(
				context.Background(),
				managedAddress,
				ownership.NoClaim(),
				claimValue,
			); storeErr != nil {
				t.Fatalf("seed ownership registry returned error: %v", storeErr)
			}
		}
		ownershipObservation, err = observe.NewExactOwnershipObservation(
			fixture.destination,
			output.ContentPath(contribution.Contribution().ContentPath()),
			managedAddress,
			claimValue,
		)
		if err != nil {
			t.Fatal(err)
		}
	} else if provisional, present := pathObservation.Provisional(); present {
		if removing {
			t.Fatal("removal fixture cannot start from provisional path authority")
		}
		ownershipObservation, err = observe.NewProvisionalOwnershipObservation(
			fixture.destination,
			output.ContentPath(contribution.Contribution().ContentPath()),
			provisional,
			ownership.NoClaim(),
		)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal("aggregate destination has no path authority observation")
	}
	ownershipEvidence := []observe.OwnershipObservation{ownershipObservation}
	decisions, err := reconcileprojection.BuildAggregateDecisions(reconcileprojection.AggregateInput{
		Locked:          locked,
		Expected:        expected,
		Desired:         desired,
		States:          states,
		Evidence:        []observe.AggregateEvidence{evidence},
		SelectedTargets: testSelectedTargets(t, target.TargetClaudeCode),
		Owner:           owner,
		Ownership:       ownershipEvidence,
		Codecs:          testAggregateCodecs(),
	})
	if err != nil {
		t.Fatalf("BuildAggregateDecisions returned error: %v", err)
	}
	effects, err := AggregateEffects(decisions)
	if err != nil {
		t.Fatalf("AggregateEffects returned error: %v", err)
	}
	return fixture, ApplyInput{
		Paths:                   fixture.paths,
		Resolver:                destinationResolver(fixture.paths),
		AggregateEffects:        effects,
		CurrentState:            currentState,
		Owner:                   owner,
		Ownership:               ownershipEvidence,
		Codecs:                  testAggregateCodecs(),
		OwnershipRegistryBinder: testOwnershipRegistryBinder(),
		StateCodec:              testStateCodec(),
		Filesystem:              testFilesystem(),
	}
}

func requireOnlyOwnershipClaim(t *testing.T, path string) ownership.Claim {
	t.Helper()
	store, err := ownershipstore.New(path)
	if err != nil {
		t.Fatalf("ownership store New returned error: %v", err)
	}
	registry, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("ownership store Load returned error: %v", err)
	}
	claims := registry.Claims()
	if len(claims) != 1 {
		t.Fatalf("ownership claims = %#v, want one", claims)
	}
	return claims[0]
}

func assertOwnershipRegistryClaimCount(t *testing.T, path string, want int) {
	t.Helper()
	store, err := ownershipstore.New(path)
	if err != nil {
		t.Fatalf("ownership store New returned error: %v", err)
	}
	registry, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("ownership store Load returned error: %v", err)
	}
	if claims := registry.Claims(); len(claims) != want {
		t.Fatalf("ownership claims = %#v, want count %d", claims, want)
	}
}

func assertNoActiveRecoveryOperation(t *testing.T, recoveryDir string) {
	t.Helper()
	entries, err := os.ReadDir(recoveryDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read recovery directory returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("recovery entries = %#v, want none", entries)
	}
}
