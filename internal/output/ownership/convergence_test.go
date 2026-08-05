package ownership

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestClaimConvergenceIsPermutationInvariantAndIdempotent(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	alpha, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "alpha"), ""), owner)
	beta, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "beta"), ""), owner)
	gamma, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "gamma"), ""), owner)
	alphaValue, _ := PresentClaim(alpha)
	betaValue, _ := PresentClaim(beta)
	gammaValue, _ := PresentClaim(gamma)
	changes := []ClaimChange{
		mustClaimChange(t, beta.Address(), betaValue, NoClaim()),
		mustClaimChange(t, alpha.Address(), NoClaim(), alphaValue),
		mustClaimChange(t, gamma.Address(), gammaValue, gammaValue),
	}
	initial, _ := NewRegistry([]Claim{gamma, beta})

	forward := mustClaimConvergence(t, changes)
	reversedChanges := append([]ClaimChange(nil), changes...)
	slices.Reverse(reversedChanges)
	reversed := mustClaimConvergence(t, reversedChanges)
	forwardResult, forwardChanged, err := forward.Apply(initial)
	if err != nil {
		t.Fatalf("forward Apply returned error: %v", err)
	}
	reversedResult, reversedChanged, err := reversed.Apply(initial)
	if err != nil {
		t.Fatalf("reversed Apply returned error: %v", err)
	}
	if !forwardChanged || !reversedChanged || !claimsEqual(forwardResult.Claims(), reversedResult.Claims()) {
		t.Fatalf("permuted convergence results differ: %#v vs %#v", forwardResult.Claims(), reversedResult.Claims())
	}
	if got := forwardResult.Claims(); len(got) != 2 || !got[0].Equal(alpha) || !got[1].Equal(gamma) {
		t.Fatalf("converged claims = %#v, want alpha and gamma", got)
	}

	idempotent, changed, err := forward.Apply(forwardResult)
	if err != nil {
		t.Fatalf("idempotent Apply returned error: %v", err)
	}
	if changed || !claimsEqual(idempotent.Claims(), forwardResult.Claims()) {
		t.Fatalf("idempotent convergence changed result: %#v", idempotent.Claims())
	}
}

func TestClaimConvergenceAcceptsMixedExpectedAndTargetSnapshot(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	alpha, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "alpha"), ""), owner)
	beta, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "beta"), ""), owner)
	alphaValue, _ := PresentClaim(alpha)
	betaValue, _ := PresentClaim(beta)
	convergence := mustClaimConvergence(t, []ClaimChange{
		mustClaimChange(t, alpha.Address(), NoClaim(), alphaValue),
		mustClaimChange(t, beta.Address(), betaValue, NoClaim()),
	})
	mixed, _ := NewRegistry([]Claim{alpha, beta})

	result, changed, err := convergence.Apply(mixed)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if !changed {
		t.Fatal("mixed convergence reported no change")
	}
	if got := result.Claims(); len(got) != 1 || !got[0].Equal(alpha) {
		t.Fatalf("converged claims = %#v, want alpha", got)
	}
}

func TestClaimConvergenceRejectsStaleAndOverlappingFacts(t *testing.T) {
	root := t.TempDir()
	leftOwner := mustAuthority(t, filepath.Join(root, "left-state.json"), filepath.Join(root, "left.toml"))
	rightOwner := mustAuthority(t, filepath.Join(root, "right-state.json"), filepath.Join(root, "right.toml"))
	address := mustAddress(t, filepath.Join(root, "shared"), "")
	left, _ := NewActiveClaim(address, leftOwner)
	right, _ := NewActiveClaim(address, rightOwner)
	leftValue, _ := PresentClaim(left)
	rightValue, _ := PresentClaim(right)
	convergence := mustClaimConvergence(t, []ClaimChange{
		mustClaimChange(t, address, leftValue, NoClaim()),
	})
	registry, _ := NewRegistry([]Claim{right})

	_, _, err := convergence.Apply(registry)
	var stale *StaleClaimError
	if !errors.As(err, &stale) || !stale.Actual.Equal(rightValue) {
		t.Fatalf("Apply error = %v, want stale right-owner claim", err)
	}

	ancestor := mustAddress(t, filepath.Join(root, "tree"), "")
	descendant := mustAddress(t, filepath.Join(root, "tree", "child"), "")
	interleaved := mustAddress(t, filepath.Join(root, "tree-other"), "")
	_, err = NewClaimConvergence([]ClaimChange{
		mustClaimChange(t, descendant, NoClaim(), NoClaim()),
		mustClaimChange(t, interleaved, NoClaim(), NoClaim()),
		mustClaimChange(t, ancestor, NoClaim(), NoClaim()),
	})
	if err == nil {
		t.Fatal("NewClaimConvergence accepted overlapping ancestor and descendant")
	}
}

func TestClaimConvergenceStaleFailureUsesCanonicalAddressOrder(t *testing.T) {
	root := t.TempDir()
	expectedOwner := mustAuthority(t, filepath.Join(root, "expected-state.json"), filepath.Join(root, "expected.toml"))
	actualOwner := mustAuthority(t, filepath.Join(root, "actual-state.json"), filepath.Join(root, "actual.toml"))
	alphaAddress := mustAddress(t, filepath.Join(root, "alpha"), "")
	betaAddress := mustAddress(t, filepath.Join(root, "beta"), "")
	alphaExpected, _ := NewActiveClaim(alphaAddress, expectedOwner)
	betaExpected, _ := NewActiveClaim(betaAddress, expectedOwner)
	alphaActual, _ := NewActiveClaim(alphaAddress, actualOwner)
	betaActual, _ := NewActiveClaim(betaAddress, actualOwner)
	alphaExpectedValue, _ := PresentClaim(alphaExpected)
	betaExpectedValue, _ := PresentClaim(betaExpected)
	registry, _ := NewRegistry([]Claim{betaActual, alphaActual})
	changes := []ClaimChange{
		mustClaimChange(t, betaAddress, betaExpectedValue, NoClaim()),
		mustClaimChange(t, alphaAddress, alphaExpectedValue, NoClaim()),
	}

	for _, ordered := range [][]ClaimChange{changes, {changes[1], changes[0]}} {
		convergence := mustClaimConvergence(t, ordered)
		_, _, err := convergence.Apply(registry)
		var stale *StaleClaimError
		if !errors.As(err, &stale) || !stale.Address.Equal(alphaAddress) {
			t.Fatalf("Apply error = %v, want canonical alpha stale failure", err)
		}
	}
}

func TestClaimConvergenceRejectsDuplicateAddress(t *testing.T) {
	address := mustAddress(t, filepath.Join(t.TempDir(), "duplicate"), "")
	change := mustClaimChange(t, address, NoClaim(), NoClaim())

	if _, err := NewClaimConvergence([]ClaimChange{change, change}); err == nil {
		t.Fatal("NewClaimConvergence accepted a duplicate address")
	}
}

func TestClaimConvergenceDistinguishesConstructedEmptyRegistryFromZeroValue(t *testing.T) {
	convergence := mustClaimConvergence(t, nil)
	constructed, err := NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, changed, err := convergence.Apply(constructed); err != nil || changed {
		t.Fatalf("Apply to constructed empty registry = changed %t, error %v", changed, err)
	}
	if _, _, err := convergence.Apply(Registry{}); err == nil {
		t.Fatal("Apply accepted a zero-value registry")
	}
}

func TestClaimConvergenceExpectedRemovalsAreCanonical(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	alpha, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "alpha"), ""), owner)
	beta, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "beta"), ""), owner)
	alphaValue, _ := PresentClaim(alpha)
	betaValue, _ := PresentClaim(beta)
	convergence := mustClaimConvergence(t, []ClaimChange{
		mustClaimChange(t, beta.Address(), betaValue, NoClaim()),
		mustClaimChange(t, alpha.Address(), alphaValue, NoClaim()),
	})

	removals := convergence.ExpectedRemovals()
	if len(removals) != 2 || !removals[0].Equal(alpha) || !removals[1].Equal(beta) {
		t.Fatalf("expected removals = %#v, want canonical alpha then beta", removals)
	}
}

func mustClaimChange(
	t *testing.T,
	address ManagedAddress,
	expected ClaimValue,
	target ClaimValue,
) ClaimChange {
	t.Helper()
	change, err := NewClaimChange(address, expected, target)
	if err != nil {
		t.Fatalf("NewClaimChange returned error: %v", err)
	}
	return change
}

func mustClaimConvergence(t *testing.T, changes []ClaimChange) ClaimConvergence {
	t.Helper()
	convergence, err := NewClaimConvergence(changes)
	if err != nil {
		t.Fatalf("NewClaimConvergence returned error: %v", err)
	}
	return convergence
}

func claimsEqual(left []Claim, right []Claim) bool {
	return slices.EqualFunc(left, right, func(leftClaim Claim, rightClaim Claim) bool {
		return leftClaim.Equal(rightClaim)
	})
}
