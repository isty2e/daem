//go:build darwin || linux

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestRecoveryLoadChargesRegistryAndEveryClaimAuthority(t *testing.T) {
	root := canonicalTestRoot(t)
	registryStore := mustStore(t, filepath.Join(root, "data", "ownership", "claims.json"))
	claims := []ownership.Claim{
		testActiveClaim(t, root, "shared-owner", filepath.Join(root, "host", "first"), ""),
		testActiveClaim(t, root, "shared-owner", filepath.Join(root, "host", "second"), ""),
	}
	for _, claim := range claims {
		value, _ := ownership.PresentClaim(claim)
		if _, err := registryStore.Apply(t.Context(), claim.Address(), ownership.NoClaim(), value); err != nil {
			t.Fatalf("seed ownership claim: %v", err)
		}
	}

	measured := &recordingPhysicalTraversalBudget{remaining: 1 << 30}
	if _, err := registryStore.LoadForClaimRemovals(
		t.Context(),
		nil,
		recovery.MaximumPhysicalPathDepth,
		measured,
	); err != nil {
		t.Fatalf("measure bounded recovery load: %v", err)
	}
	if measured.admitted <= 0 || measured.calls < 3 {
		t.Fatalf(
			"recovery load path work = components:%d calls:%d, want registry and claim observations",
			measured.admitted,
			measured.calls,
		)
	}

	exhausted := &recordingPhysicalTraversalBudget{remaining: measured.admitted - 1}
	if _, err := registryStore.LoadForClaimRemovals(
		t.Context(),
		nil,
		recovery.MaximumPhysicalPathDepth,
		exhausted,
	); err == nil || !strings.Contains(err.Error(), errTestTraversalBudgetExhausted.Error()) {
		t.Fatalf("under-budget recovery load error = %v, want traversal-budget refusal", err)
	}
}

func TestRecoveryReaderDefersPhysicalResolutionToBoundedLoad(t *testing.T) {
	root := canonicalTestRoot(t)
	physical := filepath.Join(root, "physical")
	if err := os.MkdirAll(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(alias, "ownership", "claims.json")

	reader, err := NewRecoveryReader(path)
	if err != nil {
		t.Fatalf("NewRecoveryReader returned error before a budget existed: %v", err)
	}
	budget := &recordingPhysicalTraversalBudget{remaining: 0}
	if _, err := reader.LoadForClaimRemovals(
		t.Context(),
		nil,
		recovery.MaximumPhysicalPathDepth,
		budget,
	); err == nil || !strings.Contains(err.Error(), errTestTraversalBudgetExhausted.Error()) {
		t.Fatalf("bounded recovery load error = %v, want traversal-budget refusal", err)
	}
	if budget.calls == 0 {
		t.Fatal("bounded recovery load did not charge physical path resolution")
	}
}

func TestRecoveryRootedLoadChargesRegistryAndEveryClaimAuthority(t *testing.T) {
	root := canonicalTestRoot(t)
	registryStore := mustStore(t, filepath.Join(root, "data", "ownership", "claims.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "host", "entry"), "")
	value, _ := ownership.PresentClaim(claim)
	if _, err := registryStore.Apply(t.Context(), claim.Address(), ownership.NoClaim(), value); err != nil {
		t.Fatalf("seed ownership claim: %v", err)
	}

	capturedRoot, destination, err := rootedpath.CaptureDestination(registryStore.Path())
	if err != nil {
		t.Fatalf("capture ownership registry root: %v", err)
	}
	defer capturedRoot.Close()
	rootedBudget := &recordingPhysicalTraversalBudget{remaining: 1 << 30}
	rootedStore, err := NewRooted(
		capturedRoot,
		destination,
		recovery.MaximumPhysicalPathDepth,
		rootedBudget,
	)
	if err != nil {
		t.Fatalf("bind rooted ownership registry: %v", err)
	}

	measured := &recordingPhysicalTraversalBudget{remaining: 1 << 30}
	if _, err := rootedStore.LoadForClaimRemovals(
		t.Context(),
		nil,
		recovery.MaximumPhysicalPathDepth,
		measured,
	); err != nil {
		t.Fatalf("measure bounded rooted recovery load: %v", err)
	}
	if measured.admitted <= 0 || measured.calls < 2 {
		t.Fatalf(
			"rooted recovery load path work = components:%d calls:%d, want registry and claim observations",
			measured.admitted,
			measured.calls,
		)
	}

	exhausted := &recordingPhysicalTraversalBudget{remaining: measured.admitted - 1}
	if _, err := rootedStore.LoadForClaimRemovals(
		t.Context(),
		nil,
		recovery.MaximumPhysicalPathDepth,
		exhausted,
	); err == nil || !strings.Contains(err.Error(), errTestTraversalBudgetExhausted.Error()) {
		t.Fatalf("under-budget rooted recovery load error = %v, want traversal-budget refusal", err)
	}
}

func TestBoundedRootedStoreKeepsBudgetForLaterOperations(t *testing.T) {
	root := canonicalTestRoot(t)
	registryStore := mustStore(t, filepath.Join(root, "data", "ownership", "claims.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "host", "entry"), "")
	claimValue, _ := ownership.PresentClaim(claim)
	if _, err := registryStore.Apply(t.Context(), claim.Address(), ownership.NoClaim(), claimValue); err != nil {
		t.Fatalf("seed ownership claim: %v", err)
	}

	for _, test := range []struct {
		name string
		run  func(Store) error
	}{
		{
			name: "load",
			run: func(store Store) error {
				_, err := store.Load(t.Context())
				return err
			},
		},
		{
			name: "converge",
			run: func(store Store) error {
				convergence, err := ownership.NewClaimConvergence([]ownership.ClaimChange{
					mustStoreClaimChange(t, claim.Address(), claimValue, claimValue),
				})
				if err != nil {
					return err
				}
				_, err = store.Converge(t.Context(), convergence)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			capturedRoot, destination, err := rootedpath.CaptureDestination(registryStore.Path())
			if err != nil {
				t.Fatal(err)
			}
			defer capturedRoot.Close()
			budget := &recordingPhysicalTraversalBudget{remaining: 1 << 30}
			bounded, err := NewRooted(
				capturedRoot,
				destination,
				recovery.MaximumPhysicalPathDepth,
				budget,
			)
			if err != nil {
				t.Fatal(err)
			}
			budget.remaining = 0
			if err := test.run(bounded); err == nil ||
				!strings.Contains(err.Error(), errTestTraversalBudgetExhausted.Error()) {
				t.Fatalf("bounded %s error = %v, want traversal-budget refusal", test.name, err)
			}
		})
	}
}

func TestRecoveryAuthorityObservationSessionDeduplicatesSharedPath(t *testing.T) {
	root := canonicalTestRoot(t)
	statefilePath := filepath.Join(root, "owner", ".daem", "state.json")
	budget := &recordingPhysicalTraversalBudget{remaining: 1 << 30}
	session, err := newBoundedPathAuthorityObservationSession(
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		t.Fatalf("construct bounded authority session: %v", err)
	}
	first, err := session.observePersisted(statefilePath)
	if err != nil {
		t.Fatalf("observe statefile authority: %v", err)
	}
	afterFirst := budget.admitted
	second, err := session.observePersisted(statefilePath)
	if err != nil {
		t.Fatalf("observe cached statefile authority: %v", err)
	}
	if !first.Equal(second) {
		t.Fatal("cached statefile authority changed")
	}
	if budget.admitted != afterFirst {
		t.Fatalf(
			"cached statefile observation charged %d additional components",
			budget.admitted-afterFirst,
		)
	}
}

func TestRecoveryLoadRequiresCompleteBoundedPolicy(t *testing.T) {
	registryStore := mustStore(t, filepath.Join(canonicalTestRoot(t), "claims.json"))
	tests := []struct {
		name    string
		depth   int
		budget  rootedpath.PhysicalTraversalBudget
		message string
	}{
		{
			name:    "missing depth",
			budget:  &recordingPhysicalTraversalBudget{remaining: 1 << 30},
			message: "maximum physical depth must be positive",
		},
		{
			name:    "missing budget",
			depth:   recovery.MaximumPhysicalPathDepth,
			message: "traversal budget is required",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := registryStore.LoadForClaimRemovals(
				t.Context(),
				nil,
				test.depth,
				test.budget,
			)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("bounded-policy error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestRecoveryLoadPreservesCancellationBeforePathObservation(t *testing.T) {
	registryStore := mustStore(t, filepath.Join(canonicalTestRoot(t), "claims.json"))
	budget := &recordingPhysicalTraversalBudget{remaining: 1 << 30}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := registryStore.LoadForClaimRemovals(
		ctx,
		nil,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled recovery load error = %v, want context.Canceled", err)
	}
	if budget.calls != 0 || budget.admitted != 0 {
		t.Fatalf("canceled recovery load consumed path budget: %#v", budget)
	}
}

var errTestTraversalBudgetExhausted = errors.New("test physical traversal budget exhausted")

type recordingPhysicalTraversalBudget struct {
	remaining int
	admitted  int
	calls     int
}

func (budget *recordingPhysicalTraversalBudget) AdmitPathComponents(count int) error {
	budget.calls++
	if count < 0 || count > budget.remaining {
		return errTestTraversalBudgetExhausted
	}
	budget.remaining -= count
	budget.admitted += count
	return nil
}
