package execute

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestOwnershipClaimPhasesUseOneStoreAndGateOperation(t *testing.T) {
	transitions := ownershipBatchTransitions(t, 4)

	store := &countingOwnershipRegistryStore{registry: ownership.EmptyRegistry()}
	gate := &countingVisibilityGate{}
	if err := prepareClaimTransitions(t.Context(), store, transitions, gate.effectGate()); err != nil {
		t.Fatalf("prepareClaimTransitions returned error: %v", err)
	}
	assertOwnershipBatchCounts(t, store, gate, 1)
	for _, claim := range store.registry.Claims() {
		if claim.State() != ownership.ClaimReserved {
			t.Fatalf("prepared claim state = %q, want reserved", claim.State())
		}
	}

	if err := finalizeClaimTransitions(t.Context(), store, transitions, gate.effectGate()); err != nil {
		t.Fatalf("finalizeClaimTransitions returned error: %v", err)
	}
	assertOwnershipBatchCounts(t, store, gate, 2)
	for _, claim := range store.registry.Claims() {
		if claim.State() != ownership.ClaimActive {
			t.Fatalf("final claim state = %q, want active", claim.State())
		}
	}
}

func TestOwnershipClaimRollbackConvergesMixedPreparedSnapshotOnce(t *testing.T) {
	transitions := ownershipBatchTransitions(t, 4)
	set, err := ownershipmutation.NewClaimTransitionSet(transitions)
	if err != nil {
		t.Fatal(err)
	}
	preparation, err := set.Preparation()
	if err != nil {
		t.Fatal(err)
	}
	prepared, _, err := preparation.Apply(ownership.EmptyRegistry())
	if err != nil {
		t.Fatal(err)
	}
	preparedClaims := prepared.Claims()
	mixed, err := ownership.NewRegistry(preparedClaims[:2])
	if err != nil {
		t.Fatal(err)
	}
	store := &countingOwnershipRegistryStore{registry: mixed}
	gate := &countingVisibilityGate{}

	if err := rollbackClaimsToBefore(t.Context(), store, transitions, gate.effectGate()); err != nil {
		t.Fatalf("rollbackClaimsToBefore returned error: %v", err)
	}
	assertOwnershipBatchCounts(t, store, gate, 1)
	if claims := store.registry.Claims(); len(claims) != 0 {
		t.Fatalf("rolled-back claims = %#v, want empty", claims)
	}
}

func TestOwnershipClaimPreparationRejectsStaleBatchWithoutPartialMutation(t *testing.T) {
	transitions := ownershipBatchTransitions(t, 4)
	stale, present := transitions[2].After().Get()
	if !present {
		t.Fatal("acquire transition after claim is absent")
	}
	initial, err := ownership.NewRegistry([]ownership.Claim{stale})
	if err != nil {
		t.Fatal(err)
	}
	store := &countingOwnershipRegistryStore{registry: initial}
	gate := &countingVisibilityGate{}

	err = prepareClaimTransitions(t.Context(), store, transitions, gate.effectGate())
	var staleErr *ownership.StaleClaimError
	if !errors.As(err, &staleErr) {
		t.Fatalf("prepareClaimTransitions error = %v, want stale claim", err)
	}
	if claims := store.registry.Claims(); len(claims) != 1 || !claims[0].Equal(stale) {
		t.Fatalf("registry after stale batch = %#v, want original stale claim", claims)
	}
	if store.convergences != 1 || gate.before != 1 || gate.after != 0 {
		t.Fatalf(
			"stale operations = store:%d before:%d after:%d, want 1, 1, 0",
			store.convergences,
			gate.before,
			gate.after,
		)
	}
}

func TestOwnershipClaimPreparationIndeterminateStateRollsBackAsOneBatch(t *testing.T) {
	transitions := ownershipBatchTransitions(t, 4)
	indeterminate := errors.New("injected indeterminate registry commit")
	store := &countingOwnershipRegistryStore{
		registry: ownership.EmptyRegistry(),
		outcomes: []registryConvergenceOutcome{{publish: true, err: indeterminate}},
	}
	forwardGate := &countingVisibilityGate{}

	err := prepareClaimTransitions(t.Context(), store, transitions, forwardGate.effectGate())
	if !errors.Is(err, indeterminate) {
		t.Fatalf("prepareClaimTransitions error = %v, want indeterminate commit", err)
	}
	if claims := store.registry.Claims(); len(claims) != len(transitions) {
		t.Fatalf("indeterminate registry claims = %d, want %d", len(claims), len(transitions))
	}
	for _, claim := range store.registry.Claims() {
		if claim.State() != ownership.ClaimReserved {
			t.Fatalf("indeterminate claim state = %q, want reserved", claim.State())
		}
	}
	if store.convergences != 1 || forwardGate.before != 1 || forwardGate.after != 0 {
		t.Fatalf(
			"indeterminate operations = store:%d before:%d after:%d, want 1, 1, 0",
			store.convergences,
			forwardGate.before,
			forwardGate.after,
		)
	}

	compensationGate := &countingVisibilityGate{}
	if err := rollbackClaimsToBefore(t.Context(), store, transitions, compensationGate.effectGate()); err != nil {
		t.Fatalf("rollbackClaimsToBefore returned error: %v", err)
	}
	if claims := store.registry.Claims(); len(claims) != 0 {
		t.Fatalf("rolled-back claims = %#v, want empty", claims)
	}
	if store.convergences != 2 || compensationGate.before != 1 || compensationGate.after != 1 {
		t.Fatalf(
			"rollback operations = store:%d before:%d after:%d, want 2, 1, 1",
			store.convergences,
			compensationGate.before,
			compensationGate.after,
		)
	}
}

func TestOwnershipClaimPreparationUncommittedFailureLeavesBeforeState(t *testing.T) {
	transitions := ownershipBatchTransitions(t, 4)
	uncommitted := errors.New("injected uncommitted registry failure")
	store := &countingOwnershipRegistryStore{
		registry: ownership.EmptyRegistry(),
		outcomes: []registryConvergenceOutcome{{err: uncommitted}},
	}
	forwardGate := &countingVisibilityGate{}

	err := prepareClaimTransitions(t.Context(), store, transitions, forwardGate.effectGate())
	if !errors.Is(err, uncommitted) {
		t.Fatalf("prepareClaimTransitions error = %v, want uncommitted failure", err)
	}
	if claims := store.registry.Claims(); len(claims) != 0 {
		t.Fatalf("uncommitted registry claims = %#v, want empty", claims)
	}
	if store.convergences != 1 || forwardGate.before != 1 || forwardGate.after != 0 {
		t.Fatalf(
			"uncommitted operations = store:%d before:%d after:%d, want 1, 1, 0",
			store.convergences,
			forwardGate.before,
			forwardGate.after,
		)
	}

	compensationGate := &countingVisibilityGate{}
	if err := rollbackClaimsToBefore(t.Context(), store, transitions, compensationGate.effectGate()); err != nil {
		t.Fatalf("rollbackClaimsToBefore returned error: %v", err)
	}
	if claims := store.registry.Claims(); len(claims) != 0 {
		t.Fatalf("claims after idempotent rollback = %#v, want empty", claims)
	}
	if store.convergences != 2 || compensationGate.before != 1 || compensationGate.after != 1 {
		t.Fatalf(
			"rollback operations = store:%d before:%d after:%d, want 2, 1, 1",
			store.convergences,
			compensationGate.before,
			compensationGate.after,
		)
	}
}

func TestOwnershipClaimFinalizationRetriesIndeterminatePublishedBatch(t *testing.T) {
	transitions := ownershipBatchTransitions(t, 4)
	set, err := ownershipmutation.NewClaimTransitionSet(transitions)
	if err != nil {
		t.Fatal(err)
	}
	preparation, err := set.Preparation()
	if err != nil {
		t.Fatal(err)
	}
	prepared, _, err := preparation.Apply(ownership.EmptyRegistry())
	if err != nil {
		t.Fatal(err)
	}
	indeterminate := errors.New("injected indeterminate registry commit")
	store := &countingOwnershipRegistryStore{
		registry: prepared,
		outcomes: []registryConvergenceOutcome{{publish: true, err: indeterminate}},
	}
	gate := &countingVisibilityGate{}

	err = finalizeClaimTransitions(t.Context(), store, transitions, gate.effectGate())
	if !errors.Is(err, indeterminate) {
		t.Fatalf("finalizeClaimTransitions error = %v, want indeterminate commit", err)
	}
	for _, claim := range store.registry.Claims() {
		if claim.State() != ownership.ClaimActive {
			t.Fatalf("indeterminate final claim state = %q, want active", claim.State())
		}
	}
	if store.convergences != 1 || gate.before != 1 || gate.after != 0 {
		t.Fatalf(
			"indeterminate operations = store:%d before:%d after:%d, want 1, 1, 0",
			store.convergences,
			gate.before,
			gate.after,
		)
	}

	if err := finalizeClaimTransitions(t.Context(), store, transitions, gate.effectGate()); err != nil {
		t.Fatalf("finalizeClaimTransitions retry returned error: %v", err)
	}
	if store.convergences != 2 || gate.before != 2 || gate.after != 1 {
		t.Fatalf(
			"retry operations = store:%d before:%d after:%d, want 2, 2, 1",
			store.convergences,
			gate.before,
			gate.after,
		)
	}
}

type countingOwnershipRegistryStore struct {
	registry     ownership.Registry
	convergences int
	outcomes     []registryConvergenceOutcome
}

type registryConvergenceOutcome struct {
	publish bool
	err     error
}

func (store *countingOwnershipRegistryStore) Load(context.Context) (ownership.Registry, error) {
	return store.registry, nil
}

func (store *countingOwnershipRegistryStore) LoadForClaimRemovals(
	context.Context,
	[]ownership.Claim,
) (ownership.Registry, error) {
	return store.registry, nil
}

func (store *countingOwnershipRegistryStore) Path() string {
	return "/ownership-registry"
}

func (store *countingOwnershipRegistryStore) Converge(
	_ context.Context,
	convergence ownership.ClaimConvergence,
) (ownership.Registry, error) {
	store.convergences++
	next, _, err := convergence.Apply(store.registry)
	if err != nil {
		return ownership.Registry{}, err
	}
	if len(store.outcomes) != 0 {
		outcome := store.outcomes[0]
		store.outcomes = store.outcomes[1:]
		if outcome.publish {
			store.registry = next
		}
		if outcome.err != nil {
			return ownership.Registry{}, outcome.err
		}
	}
	store.registry = next
	return next, nil
}

type countingVisibilityGate struct {
	before int
	after  int
}

func (gate *countingVisibilityGate) effectGate() visibilityEffectGate {
	return visibilityEffectGate{
		before: func(context.Context) error {
			gate.before++
			return nil
		},
		after: func(context.Context) error {
			gate.after++
			return nil
		},
	}
}

func ownershipBatchTransitions(t *testing.T, count int) []ownershipmutation.ClaimTransition {
	t.Helper()
	root := t.TempDir()
	statefileAuthority := mustObservedPathAuthority(t, filepath.Join(root, "state.json"))
	owner, err := stateauthority.New(statefileAuthority, filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	transitions := make([]ownershipmutation.ClaimTransition, 0, count)
	for index := range count {
		address, err := ownership.NewManagedAddress(
			mustObservedPathAuthority(t, filepath.Join(root, "output", string(rune('a'+index)))),
			"",
		)
		if err != nil {
			t.Fatal(err)
		}
		transition, err := ownershipmutation.NewAcquireTransition(address, owner, "operation-1")
		if err != nil {
			t.Fatal(err)
		}
		transitions = append(transitions, transition)
	}
	return transitions
}

func assertOwnershipBatchCounts(
	t *testing.T,
	store *countingOwnershipRegistryStore,
	gate *countingVisibilityGate,
	want int,
) {
	t.Helper()
	if store.convergences != want || gate.before != want || gate.after != want {
		t.Fatalf(
			"operations = store:%d before:%d after:%d, want %d each",
			store.convergences,
			gate.before,
			gate.after,
			want,
		)
	}
}
