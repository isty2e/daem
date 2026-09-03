package apply

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestGlobalCarrierBatchSettlementRejectsFactMismatchBeforeCallbacks(t *testing.T) {
	first := newWorkflowFixture(t, target.ScopeGlobal).claim
	second := newWorkflowFixture(t, target.ScopeGlobal).claim
	registryPath := filepath.Join(t.TempDir(), "carrier-claims.json")
	plan, err := newGlobalCarrierBatchSettlementPlan(
		globalCarrierSettlementRetirement,
		registryPath,
		durablecarrier.EmptyGlobalCarrierClaims(),
		[]durablecarrier.ManagedCarrierClaim{first},
	)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := 0
	_, _, err = executeGlobalCarrierBatchSettlement(
		t.Context(),
		plan,
		globalCarrierSettlementRetirement,
		registryPath,
		[]durablecarrier.ManagedCarrierClaim{second},
		durablecarrier.EmptyGlobalCarrierClaims(),
		globalCarrierBatchSettlementCallbacks{
			validateBefore: func() error { callbacks++; return nil },
			persist: func() (durablecarrier.GlobalCarrierClaims, int, error) {
				callbacks++
				return durablecarrier.EmptyGlobalCarrierClaims(), 0, nil
			},
			validateAfter: func() error { callbacks++; return nil },
		},
	)
	if err == nil || !strings.Contains(err.Error(), "claim facts changed") {
		t.Fatalf("settlement error = %v, want claim-fact mismatch", err)
	}
	if callbacks != 0 {
		t.Fatalf("callbacks = %d, want no callback before plan admission", callbacks)
	}
}

func TestGlobalCarrierBatchSettlementRejectsRegistryBaselineMismatch(t *testing.T) {
	claim := newWorkflowFixture(t, target.ScopeGlobal).claim
	baseline, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(t.TempDir(), "carrier-claims.json")
	plan, err := newGlobalCarrierBatchSettlementPlan(
		globalCarrierSettlementRetirement,
		registryPath,
		baseline,
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := 0
	_, _, err = executeGlobalCarrierBatchSettlement(
		t.Context(),
		plan,
		globalCarrierSettlementRetirement,
		registryPath,
		[]durablecarrier.ManagedCarrierClaim{claim},
		durablecarrier.EmptyGlobalCarrierClaims(),
		globalCarrierBatchSettlementCallbacks{
			validateBefore: func() error { callbacks++; return nil },
			persist: func() (durablecarrier.GlobalCarrierClaims, int, error) {
				callbacks++
				return durablecarrier.EmptyGlobalCarrierClaims(), 0, nil
			},
			validateAfter: func() error { callbacks++; return nil },
		},
	)
	if err == nil || !strings.Contains(err.Error(), "registry baseline changed") {
		t.Fatalf("settlement error = %v, want registry-baseline mismatch", err)
	}
	if callbacks != 0 {
		t.Fatalf("callbacks = %d, want no callback before baseline admission", callbacks)
	}
}

func TestGlobalCarrierPromotionRejectsSemanticChangeWithSameOrderingIdentity(t *testing.T) {
	record, subject := applyClaudePluginCarrierContractWithDeclarationID(
		t,
		target.ScopeGlobal,
		"semantic-binding",
	)
	current := applyClaudePluginCarrierActionForSubject(
		t,
		record,
		subject,
		observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				applyClaudePluginCarrierManagedRowWithScope(
					t,
					"context7@market",
					string(subject.ExpectedRelation().ManagedInstanceKey()),
					observeclaudeplugin.HostScopeUser,
				),
			},
		},
	)
	changed := applyClaudePluginCarrierActionForSubject(
		t,
		record,
		subject,
		observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
		},
	)
	if current.Compare(changed) != 0 || current.Kind() == changed.Kind() {
		t.Fatalf("test actions do not share ordering identity with distinct semantics: %#v / %#v", current, changed)
	}
	claim := newWorkflowFixture(t, target.ScopeGlobal).claim
	registryPath := filepath.Join(t.TempDir(), "carrier-claims.json")
	plan, err := newGlobalCarrierPromotionSettlementPlan(
		registryPath,
		durablecarrier.EmptyGlobalCarrierClaims(),
		current,
		claim,
	)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := 0
	_, _, err = executeGlobalCarrierPromotionSettlement(
		t.Context(),
		plan,
		registryPath,
		changed,
		claim,
		durable.Snapshot{},
		durablecarrier.EmptyGlobalCarrierClaims(),
		globalCarrierPromotionSettlementCallbacks{
			validateDeclarationsBefore: func() error { callbacks++; return nil },
			validateProjectRootBefore:  func() error { callbacks++; return nil },
			validateStatefileBefore:    func() error { callbacks++; return nil },
			persistRegistry: func() (durablecarrier.GlobalCarrierClaims, error) {
				callbacks++
				return durablecarrier.EmptyGlobalCarrierClaims(), nil
			},
			validateStatefileAfter:   func() error { callbacks++; return nil },
			acceptRegistryVisibility: func() error { callbacks++; return nil },
			publishStatefile: func(durablecarrier.GlobalCarrierClaims) (durable.Snapshot, error) {
				callbacks++
				return durable.Snapshot{}, nil
			},
			validateStatefileFinal:    func() error { callbacks++; return nil },
			acceptStatefileVisibility: func() error { callbacks++; return nil },
			validateProjectRootAfter:  func() error { callbacks++; return nil },
			validateDeclarationsAfter: func() error { callbacks++; return nil },
		},
	)
	if err == nil || !strings.Contains(err.Error(), "promotion facts changed") {
		t.Fatalf("promotion error = %v, want semantic-plan mismatch", err)
	}
	if callbacks != 0 {
		t.Fatalf("callbacks = %d, want no callback before semantic-plan admission", callbacks)
	}
}

func TestGlobalCarrierPromotionRejectsClaimFactChangeBeforeCallbacks(t *testing.T) {
	plannedClaim := newWorkflowFixture(t, target.ScopeGlobal).claim
	currentClaim := newWorkflowFixture(t, target.ScopeGlobal).claim
	if plannedClaim.ExactEqual(currentClaim) {
		t.Fatal("test promotion claims unexpectedly match")
	}
	registryPath := filepath.Join(t.TempDir(), "carrier-claims.json")
	action := reconcile.RelationAction{}
	plan, err := newGlobalCarrierPromotionSettlementPlan(
		registryPath,
		durablecarrier.EmptyGlobalCarrierClaims(),
		action,
		plannedClaim,
	)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := 0
	_, _, err = executeGlobalCarrierPromotionSettlement(
		t.Context(),
		plan,
		registryPath,
		action,
		currentClaim,
		durable.Snapshot{},
		durablecarrier.EmptyGlobalCarrierClaims(),
		globalCarrierPromotionSettlementCallbacks{
			validateDeclarationsBefore: func() error { callbacks++; return nil },
			validateProjectRootBefore:  func() error { callbacks++; return nil },
			validateStatefileBefore:    func() error { callbacks++; return nil },
			persistRegistry: func() (durablecarrier.GlobalCarrierClaims, error) {
				callbacks++
				return durablecarrier.EmptyGlobalCarrierClaims(), nil
			},
			validateStatefileAfter:   func() error { callbacks++; return nil },
			acceptRegistryVisibility: func() error { callbacks++; return nil },
			publishStatefile: func(durablecarrier.GlobalCarrierClaims) (durable.Snapshot, error) {
				callbacks++
				return durable.Snapshot{}, nil
			},
			validateStatefileFinal:    func() error { callbacks++; return nil },
			acceptStatefileVisibility: func() error { callbacks++; return nil },
			validateProjectRootAfter:  func() error { callbacks++; return nil },
			validateDeclarationsAfter: func() error { callbacks++; return nil },
		},
	)
	if err == nil || !strings.Contains(err.Error(), "promotion facts changed") {
		t.Fatalf("promotion error = %v, want claim-plan mismatch", err)
	}
	if callbacks != 0 {
		t.Fatalf("callbacks = %d, want no callback before claim-plan admission", callbacks)
	}
}

func TestCommitGlobalCarrierRetirementsRejectsBeforeRegistryConstruction(t *testing.T) {
	claim := newWorkflowFixture(t, target.ScopeGlobal).claim
	current, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(t.TempDir(), "missing", "carrier-claims.json")
	plan, err := newGlobalCarrierBatchSettlementPlan(
		globalCarrierSettlementRetirement,
		registryPath,
		current,
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		t.Fatal(err)
	}
	refusal := errors.New("injected pre-registry refusal")
	attempted := false
	next, count, err := commitGlobalCarrierRetirements(
		t.Context(),
		registryPath,
		current,
		[]durablecarrier.ManagedCarrierClaim{claim},
		plan,
		runOptions{
			validateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
				return refusal
			},
			validateStateDir:        func(context.Context) error { return nil },
			acceptVisibilityChanges: func(context.Context) error { return nil },
			markExecutionAttempted:  func() { attempted = true },
		},
	)
	if !errors.Is(err, refusal) {
		t.Fatalf("retirement error = %v, want pre-registry refusal", err)
	}
	if attempted {
		t.Fatal("registry construction or persistence was attempted before admission")
	}
	if count != 0 || !next.Equal(current) {
		t.Fatalf("retirement result = (%#v, %d), want unchanged current claims", next.Claims(), count)
	}
}

func TestCommitGlobalCarrierAdoptionsRejectsCancellationBeforeRegistryConstruction(t *testing.T) {
	claim := newWorkflowFixture(t, target.ScopeGlobal).claim
	registryPath := filepath.Join(t.TempDir(), "missing", "carrier-claims.json")
	plan, err := newGlobalCarrierBatchSettlementPlan(
		globalCarrierSettlementAdoption,
		registryPath,
		durablecarrier.EmptyGlobalCarrierClaims(),
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	attempted := false
	next, count, err := commitGlobalCarrierAdoptions(
		ctx,
		registryPath,
		durablecarrier.EmptyGlobalCarrierClaims(),
		[]durablecarrier.ManagedCarrierClaim{claim},
		plan,
		runOptions{
			validateBeforeEffects:   func(context.Context, mutation.PhysicalAuthoritySet) error { return nil },
			validateStateDir:        func(context.Context) error { return nil },
			acceptVisibilityChanges: func(context.Context) error { return nil },
			markExecutionAttempted:  func() { attempted = true },
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("adoption error = %v, want context cancellation", err)
	}
	if attempted {
		t.Fatal("registry construction or persistence was attempted after cancellation")
	}
	if count != 0 || len(next.Claims()) != 0 {
		t.Fatalf("adoption result = (%#v, %d), want unchanged empty claims", next.Claims(), count)
	}
}

func TestCommitGlobalCarrierRetirementsPreservesSuccessorAfterPostCommitDrift(t *testing.T) {
	claim := newWorkflowFixture(t, target.ScopeGlobal).claim
	registryPath := filepath.Join(t.TempDir(), "carrier-claims.json")
	store, err := carrierclaimstore.New(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Upsert(t.Context(), claim)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := newGlobalCarrierBatchSettlementPlan(
		globalCarrierSettlementRetirement,
		registryPath,
		current,
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		t.Fatal(err)
	}
	drift := errors.New("injected post-registry StateDir drift")
	next, count, err := commitGlobalCarrierRetirements(
		t.Context(),
		registryPath,
		current,
		[]durablecarrier.ManagedCarrierClaim{claim},
		plan,
		runOptions{
			validateBeforeEffects:   func(context.Context, mutation.PhysicalAuthoritySet) error { return nil },
			validateStateDir:        func(context.Context) error { return drift },
			acceptVisibilityChanges: func(context.Context) error { return nil },
		},
	)
	if !errors.Is(err, drift) {
		t.Fatalf("retirement error = %v, want post-registry drift", err)
	}
	if count != 1 || len(next.Claims()) != 0 {
		t.Fatalf("retirement result = (%#v, %d), want committed empty successor", next.Claims(), count)
	}
	loaded, loadErr := store.Load(t.Context())
	if loadErr != nil || !loaded.Equal(next) {
		t.Fatalf("durable registry = (%#v, %v), want returned successor", loaded.Claims(), loadErr)
	}
}

func TestCommitGlobalCarrierAdoptionsRejectsStaleBaseline(t *testing.T) {
	adopted := newWorkflowFixture(t, target.ScopeGlobal).claim
	concurrent := newWorkflowFixture(t, target.ScopeGlobal).claim
	registryPath := filepath.Join(t.TempDir(), "carrier-claims.json")
	store, err := carrierclaimstore.New(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	durableCurrent, err := store.Upsert(t.Context(), concurrent)
	if err != nil {
		t.Fatal(err)
	}
	baseline := durablecarrier.EmptyGlobalCarrierClaims()
	plan, err := newGlobalCarrierBatchSettlementPlan(
		globalCarrierSettlementAdoption,
		registryPath,
		baseline,
		[]durablecarrier.ManagedCarrierClaim{adopted},
	)
	if err != nil {
		t.Fatal(err)
	}
	next, count, err := commitGlobalCarrierAdoptions(
		t.Context(),
		registryPath,
		baseline,
		[]durablecarrier.ManagedCarrierClaim{adopted},
		plan,
		runOptions{
			validateBeforeEffects:   func(context.Context, mutation.PhysicalAuthoritySet) error { return nil },
			validateStateDir:        func(context.Context) error { return nil },
			acceptVisibilityChanges: func(context.Context) error { return nil },
		},
	)
	if err == nil || !strings.Contains(err.Error(), "changed since confirmed observation") {
		t.Fatalf("adoption error = %v, want stale baseline", err)
	}
	if count != 0 || !next.Equal(baseline) {
		t.Fatalf("adoption result = (%#v, %d), want confirmed baseline", next.Claims(), count)
	}
	loaded, loadErr := store.Load(t.Context())
	if loadErr != nil || !loaded.Equal(durableCurrent) {
		t.Fatalf("durable registry = (%#v, %v), want concurrent state", loaded.Claims(), loadErr)
	}
}

func TestCommitGlobalCarrierAdoptionsPreservesSuccessorAfterPostCommitDrift(t *testing.T) {
	claim := newWorkflowFixture(t, target.ScopeGlobal).claim
	registryPath := filepath.Join(t.TempDir(), "carrier-claims.json")
	baseline := durablecarrier.EmptyGlobalCarrierClaims()
	plan, err := newGlobalCarrierBatchSettlementPlan(
		globalCarrierSettlementAdoption,
		registryPath,
		baseline,
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		t.Fatal(err)
	}
	drift := errors.New("injected post-registry declaration drift")
	next, count, err := commitGlobalCarrierAdoptions(
		t.Context(),
		registryPath,
		baseline,
		[]durablecarrier.ManagedCarrierClaim{claim},
		plan,
		runOptions{
			validateBeforeEffects:   func(context.Context, mutation.PhysicalAuthoritySet) error { return nil },
			validateStateDir:        func(context.Context) error { return nil },
			acceptVisibilityChanges: func(context.Context) error { return drift },
		},
	)
	if !errors.Is(err, drift) {
		t.Fatalf("adoption error = %v, want post-registry drift", err)
	}
	if count != 1 || len(next.Claims()) != 1 || !next.Claims()[0].ExactEqual(claim) {
		t.Fatalf("adoption result = (%#v, %d), want committed successor", next.Claims(), count)
	}
	store, err := carrierclaimstore.New(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	loaded, loadErr := store.Load(t.Context())
	if loadErr != nil || !loaded.Equal(next) {
		t.Fatalf("durable registry = (%#v, %v), want returned successor", loaded.Claims(), loadErr)
	}
}

func TestCommitGlobalCarrierAdoptionsRunsScheduledBoundaryInOrder(t *testing.T) {
	claim := newWorkflowFixture(t, target.ScopeGlobal).claim
	registryPath := filepath.Join(t.TempDir(), "carrier-claims.json")
	plan, err := newGlobalCarrierBatchSettlementPlan(
		globalCarrierSettlementAdoption,
		registryPath,
		durablecarrier.EmptyGlobalCarrierClaims(),
		[]durablecarrier.ManagedCarrierClaim{claim},
	)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	next, count, err := commitGlobalCarrierAdoptions(
		t.Context(),
		registryPath,
		durablecarrier.EmptyGlobalCarrierClaims(),
		[]durablecarrier.ManagedCarrierClaim{claim},
		plan,
		runOptions{
			validateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
				order = append(order, "pre-registry")
				return nil
			},
			validateStateDir: func(context.Context) error {
				order = append(order, "post-registry-state-dir")
				return nil
			},
			acceptVisibilityChanges: func(context.Context) error {
				order = append(order, "post-registry-accept")
				return nil
			},
			markExecutionAttempted: func() {
				order = append(order, "registry-persistence")
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{
		"pre-registry",
		"registry-persistence",
		"post-registry-state-dir",
		"post-registry-accept",
	}
	if strings.Join(order, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("settlement order = %v, want %v", order, wantOrder)
	}
	if count != 1 || len(next.Claims()) != 1 || !next.Claims()[0].ExactEqual(claim) {
		t.Fatalf("adoption result = (%#v, %d), want one adopted claim", next.Claims(), count)
	}
	store, err := carrierclaimstore.New(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil || !loaded.Equal(next) {
		t.Fatalf("durable registry = (%#v, %v), want returned successor", loaded.Claims(), err)
	}
}

func TestCommitInterruptedGlobalCarrierClaimsSettlesMultiplePromotions(t *testing.T) {
	root := t.TempDir()
	paths := isolatedApplyTestPaths(t, root)
	statefileKey, err := mutation.CanonicalDirectoryEntryKey(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(mustObservedPathAuthority(t, statefileKey), paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	actions := []reconcile.RelationAction{
		interruptedGlobalPromotionAction(t, "alpha"),
		interruptedGlobalPromotionAction(t, "beta"),
	}
	pending := make([]durablecarrier.PendingCarrierInstall, 0, len(actions))
	for _, action := range actions {
		fact, pendingErr := durablecarrier.NewPendingCarrierInstall(
			owner,
			action.CarrierIdentity(),
			action.RouteRequest(),
		)
		if pendingErr != nil {
			t.Fatal(pendingErr)
		}
		pending = append(pending, fact)
	}
	current, err := durable.NewSnapshot(durable.SnapshotInput{PendingCarrierInstalls: pending})
	if err != nil {
		t.Fatal(err)
	}
	writeApplyStatefile(t, paths.StatefilePath, current)
	options := applyDelegateRunOptions(t, paths, runOptions{
		acceptVisibilityChanges: func(context.Context) error { return nil },
	})
	if err := options.validateBeforeEffects(t.Context(), mutation.PhysicalAuthoritySet{}); err != nil {
		t.Fatal(err)
	}
	authority, err := newStatefileEffectAuthority(
		paths.StatefilePath,
		statefileEffectPlan{validations: 6, fileCommits: 2},
		options.reserveStatefileAuthority,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Ensure(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authority.Close(); err != nil {
			t.Errorf("close statefile authority: %v", err)
		}
	})
	nextState, nextRegistry, err := commitInterruptedGlobalCarrierClaims(
		t.Context(),
		paths,
		authority,
		current,
		durablecarrier.EmptyGlobalCarrierClaims(),
		actions,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(nextState.PendingCarrierInstalls()) != 0 || len(nextRegistry.Claims()) != 2 {
		t.Fatalf(
			"settled state = pending %#v registry %#v, want no pending and two claims",
			nextState.PendingCarrierInstalls(),
			nextRegistry.Claims(),
		)
	}
	loadedState := loadApplyStatefile(t, paths.StatefilePath)
	if !loadedState.Equal(nextState) {
		t.Fatalf("durable state = %#v, want %#v", loadedState, nextState)
	}
	store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	loadedRegistry, err := store.Load(t.Context())
	if err != nil || !loadedRegistry.Equal(nextRegistry) {
		t.Fatalf("durable registry = (%#v, %v), want %#v", loadedRegistry.Claims(), err, nextRegistry.Claims())
	}
}

func interruptedGlobalPromotionAction(t *testing.T, declarationID string) reconcile.RelationAction {
	t.Helper()
	record, subject := applyClaudePluginCarrierContractWithDeclarationID(
		t,
		target.ScopeGlobal,
		declarationID,
	)
	action := applyClaudePluginCarrierActionForSubject(
		t,
		record,
		subject,
		observeclaudeplugin.InventorySpec{
			Availability: observerelation.InventorySupported,
			Freshness:    observerelation.EvidenceFresh,
			Rows: []observeclaudeplugin.Row{
				applyClaudePluginCarrierManagedRowWithScope(
					t,
					"context7@market",
					string(subject.ExpectedRelation().ManagedInstanceKey()),
					observeclaudeplugin.HostScopeUser,
				),
			},
		},
	)
	if action.Kind() != reconcile.ActionNoOp {
		t.Fatalf("promotion action = %#v, want exact no-op", action)
	}
	if _, present := action.Correlation(); !present {
		t.Fatal("promotion action has no exact correlation")
	}
	return action
}

func TestGlobalCarrierPromotionPreservesRegistryFirstSplitWrite(t *testing.T) {
	claim := newWorkflowFixture(t, target.ScopeGlobal).claim
	successor, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	current, err := durable.NewSnapshot(durable.SnapshotInput{})
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(t.TempDir(), "carrier-claims.json")
	action := reconcile.RelationAction{}
	plan, err := newGlobalCarrierPromotionSettlementPlan(
		registryPath,
		durablecarrier.EmptyGlobalCarrierClaims(),
		action,
		claim,
	)
	if err != nil {
		t.Fatal(err)
	}
	postRegistry := errors.New("injected authority drift after registry commit")
	publishedStatefile := false
	nextState, nextRegistry, err := executeGlobalCarrierPromotionSettlement(
		t.Context(),
		plan,
		registryPath,
		action,
		claim,
		current,
		durablecarrier.EmptyGlobalCarrierClaims(),
		globalCarrierPromotionSettlementCallbacks{
			validateDeclarationsBefore: func() error { return nil },
			validateProjectRootBefore:  func() error { return nil },
			validateStatefileBefore:    func() error { return nil },
			persistRegistry:            func() (durablecarrier.GlobalCarrierClaims, error) { return successor, nil },
			validateStatefileAfter:     func() error { return nil },
			acceptRegistryVisibility:   func() error { return postRegistry },
			publishStatefile: func(durablecarrier.GlobalCarrierClaims) (durable.Snapshot, error) {
				publishedStatefile = true
				return current, nil
			},
			validateStatefileFinal:    func() error { return nil },
			acceptStatefileVisibility: func() error { return nil },
			validateProjectRootAfter:  func() error { return nil },
			validateDeclarationsAfter: func() error { return nil },
		},
	)
	if !errors.Is(err, postRegistry) {
		t.Fatalf("promotion error = %v, want post-registry drift", err)
	}
	if !nextRegistry.Equal(successor) {
		t.Fatalf("returned registry = %#v, want committed successor", nextRegistry.Claims())
	}
	if !nextState.Equal(current) || publishedStatefile {
		t.Fatalf("statefile result = %#v published=%t, want unchanged pending state", nextState, publishedStatefile)
	}
}

func TestGlobalCarrierPromotionPreservesPriorStateAfterStatefileFailure(t *testing.T) {
	claim := newWorkflowFixture(t, target.ScopeGlobal).claim
	successor, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	current, err := durable.NewSnapshot(durable.SnapshotInput{})
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(t.TempDir(), "carrier-claims.json")
	action := reconcile.RelationAction{}
	plan, err := newGlobalCarrierPromotionSettlementPlan(
		registryPath,
		durablecarrier.EmptyGlobalCarrierClaims(),
		action,
		claim,
	)
	if err != nil {
		t.Fatal(err)
	}
	statefileFailure := errors.New("injected statefile publication failure")
	nextState, nextRegistry, err := executeGlobalCarrierPromotionSettlement(
		t.Context(),
		plan,
		registryPath,
		action,
		claim,
		current,
		durablecarrier.EmptyGlobalCarrierClaims(),
		globalCarrierPromotionSettlementCallbacks{
			validateDeclarationsBefore: func() error { return nil },
			validateProjectRootBefore:  func() error { return nil },
			validateStatefileBefore:    func() error { return nil },
			persistRegistry:            func() (durablecarrier.GlobalCarrierClaims, error) { return successor, nil },
			validateStatefileAfter:     func() error { return nil },
			acceptRegistryVisibility:   func() error { return nil },
			publishStatefile: func(durablecarrier.GlobalCarrierClaims) (durable.Snapshot, error) {
				return durable.Snapshot{}, statefileFailure
			},
			validateStatefileFinal:    func() error { return nil },
			acceptStatefileVisibility: func() error { return nil },
			validateProjectRootAfter:  func() error { return nil },
			validateDeclarationsAfter: func() error { return nil },
		},
	)
	if !errors.Is(err, statefileFailure) {
		t.Fatalf("promotion error = %v, want statefile failure", err)
	}
	if !nextState.Equal(current) {
		t.Fatalf("returned state = %#v, want prior state", nextState)
	}
	if !nextRegistry.Equal(successor) {
		t.Fatalf("returned registry = %#v, want committed successor", nextRegistry.Claims())
	}
}

func TestGlobalCarrierClaimsAfterPersistencePreservesPossibleSuccessor(t *testing.T) {
	claim := newWorkflowFixture(t, target.ScopeGlobal).claim
	successor, err := durablecarrier.NewGlobalCarrierClaims([]durablecarrier.ManagedCarrierClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	current := durablecarrier.EmptyGlobalCarrierClaims()
	for _, test := range []struct {
		name string
		kind mutationfs.FailureKind
		want durablecarrier.GlobalCarrierClaims
	}{
		{name: "indeterminate", kind: mutationfs.FailureIndeterminateCommit, want: successor},
		{name: "retained-residue", kind: mutationfs.FailureRetainedResidue, want: successor},
		{name: "uncommitted", kind: mutationfs.FailureUncommitted, want: current},
	} {
		t.Run(test.name, func(t *testing.T) {
			next, gotErr := globalCarrierClaimsAfterPersistence(
				current,
				successor,
				durablecarrier.EmptyGlobalCarrierClaims(),
				globalCarrierSettlementClassifiedError{kind: test.kind},
			)
			if gotErr == nil || !next.Equal(test.want) {
				t.Fatalf(
					"persistence result = (%#v, %v), want %#v and classified error",
					next.Claims(),
					gotErr,
					test.want.Claims(),
				)
			}
		})
	}
}

type globalCarrierSettlementClassifiedError struct {
	kind mutationfs.FailureKind
}

func (failure globalCarrierSettlementClassifiedError) Error() string {
	return "injected classified persistence failure"
}

func (failure globalCarrierSettlementClassifiedError) Kind() mutationfs.FailureKind {
	return failure.kind
}
