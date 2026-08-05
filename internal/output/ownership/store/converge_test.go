package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output/ownership"
)

func TestStoreConvergesMultipleClaimsThroughOneCanonicalSuccessor(t *testing.T) {
	root := canonicalTestRoot(t)
	registryStore := mustStore(t, filepath.Join(root, "data", "ownership", "claims.json"))
	alpha := testActiveClaim(t, root, "owner", filepath.Join(root, "host", "alpha"), "")
	beta := testActiveClaim(t, root, "owner", filepath.Join(root, "host", "beta"), "")
	gamma := testActiveClaim(t, root, "owner", filepath.Join(root, "host", "gamma"), "")
	alphaValue, _ := ownership.PresentClaim(alpha)
	betaValue, _ := ownership.PresentClaim(beta)
	gammaValue, _ := ownership.PresentClaim(gamma)
	if _, err := registryStore.Apply(t.Context(), alpha.Address(), ownership.NoClaim(), alphaValue); err != nil {
		t.Fatal(err)
	}
	if _, err := registryStore.Apply(t.Context(), beta.Address(), ownership.NoClaim(), betaValue); err != nil {
		t.Fatal(err)
	}
	convergence, err := ownership.NewClaimConvergence([]ownership.ClaimChange{
		mustStoreClaimChange(t, gamma.Address(), ownership.NoClaim(), gammaValue),
		mustStoreClaimChange(t, alpha.Address(), alphaValue, ownership.NoClaim()),
		mustStoreClaimChange(t, beta.Address(), betaValue, betaValue),
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := registryStore.Converge(context.Background(), convergence)
	if err != nil {
		t.Fatalf("Store.Converge returned error: %v", err)
	}
	claims := result.Claims()
	if len(claims) != 2 || !claims[0].Equal(beta) || !claims[1].Equal(gamma) {
		t.Fatalf("converged claims = %#v, want beta and gamma", claims)
	}
	first, err := os.ReadFile(registryStore.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registryStore.Converge(context.Background(), convergence); err != nil {
		t.Fatalf("idempotent Store.Converge returned error: %v", err)
	}
	second, err := os.ReadFile(registryStore.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("idempotent convergence rewrote different registry bytes")
	}
}

func TestStoreConvergenceHonorsCancellationBeforeRegistryIO(t *testing.T) {
	root := canonicalTestRoot(t)
	registryStore := mustStore(t, filepath.Join(root, "data", "ownership", "claims.json"))
	claim := testActiveClaim(t, root, "owner", filepath.Join(root, "host", "alpha"), "")
	claimValue, _ := ownership.PresentClaim(claim)
	convergence, err := ownership.NewClaimConvergence([]ownership.ClaimChange{
		mustStoreClaimChange(t, claim.Address(), ownership.NoClaim(), claimValue),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := registryStore.Converge(ctx, convergence); !errors.Is(err, context.Canceled) {
		t.Fatalf("Store.Converge error = %v, want context cancellation", err)
	}
	if _, err := os.Stat(registryStore.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ownership registry stat error = %v, want absent", err)
	}
}

func mustStoreClaimChange(
	t *testing.T,
	address ownership.ManagedAddress,
	expected ownership.ClaimValue,
	target ownership.ClaimValue,
) ownership.ClaimChange {
	t.Helper()
	change, err := ownership.NewClaimChange(address, expected, target)
	if err != nil {
		t.Fatal(err)
	}
	return change
}
