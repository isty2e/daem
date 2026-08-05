package ownership

import (
	"path/filepath"
	"testing"

	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

func TestClaimTransitionSetDerivesCanonicalLifecycleConvergences(t *testing.T) {
	root := t.TempDir()
	authority := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	alphaAddress := mustAddress(t, filepath.Join(root, "alpha"), "")
	betaAddress := mustAddress(t, filepath.Join(root, "beta"), "")
	alpha, err := NewAcquireTransition(alphaAddress, authority, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	betaActive, _ := outputownership.NewActiveClaim(betaAddress, authority)
	beta, err := NewReleaseTransition(betaActive)
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewClaimTransitionSet([]ClaimTransition{beta, alpha})
	if err != nil {
		t.Fatalf("NewClaimTransitionSet returned error: %v", err)
	}

	preparation, err := set.Preparation()
	if err != nil {
		t.Fatal(err)
	}
	initial, err := outputownership.NewRegistry([]outputownership.Claim{betaActive})
	if err != nil {
		t.Fatal(err)
	}
	preparedRegistry, changed, err := preparation.Apply(initial)
	if err != nil {
		t.Fatal(err)
	}
	prepared := preparedRegistry.Claims()
	if !changed || len(prepared) != 2 || !prepared[0].Address().Equal(alphaAddress) || prepared[0].State() != outputownership.ClaimReserved ||
		!prepared[1].Address().Equal(betaAddress) || prepared[1].State() != outputownership.ClaimActive {
		t.Fatalf("prepared claims = %#v, want reserved alpha and active beta", prepared)
	}

	finalization, err := set.Finalization()
	if err != nil {
		t.Fatal(err)
	}
	finalRegistry, changed, err := finalization.Apply(preparedRegistry)
	if err != nil {
		t.Fatal(err)
	}
	final := finalRegistry.Claims()
	if !changed || len(final) != 1 || !final[0].Address().Equal(alphaAddress) || final[0].State() != outputownership.ClaimActive {
		t.Fatalf("final claims = %#v, want active alpha", final)
	}

	rollback, err := set.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	rolledBackRegistry, changed, err := rollback.Apply(preparedRegistry)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack := rolledBackRegistry.Claims()
	if !changed || len(rolledBack) != 1 || !rolledBack[0].Equal(betaActive) {
		t.Fatalf("rolled-back claims = %#v, want active beta", rolledBack)
	}
}

func TestClaimTransitionSetRejectsOverlappingAddresses(t *testing.T) {
	root := t.TempDir()
	authority := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	ancestor, err := NewAcquireTransition(mustAddress(t, filepath.Join(root, "tree"), ""), authority, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	descendant, err := NewAcquireTransition(mustAddress(t, filepath.Join(root, "tree", "child"), ""), authority, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewClaimTransitionSet([]ClaimTransition{descendant, ancestor}); err == nil {
		t.Fatal("NewClaimTransitionSet accepted overlapping addresses")
	}
}

func TestClaimTransitionSetRejectsDifferentStateAuthorities(t *testing.T) {
	root := t.TempDir()
	leftOwner := mustAuthority(t, filepath.Join(root, "left-state.json"), filepath.Join(root, "left.toml"))
	rightOwner := mustAuthority(t, filepath.Join(root, "right-state.json"), filepath.Join(root, "right.toml"))
	left, err := NewAcquireTransition(mustAddress(t, filepath.Join(root, "left"), ""), leftOwner, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewAcquireTransition(mustAddress(t, filepath.Join(root, "right"), ""), rightOwner, "operation-1")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewClaimTransitionSet([]ClaimTransition{right, left}); err == nil {
		t.Fatal("NewClaimTransitionSet accepted different state authorities")
	}
}

func TestClaimTransitionSetPreservesDistinctProvenanceForOneStateAuthority(t *testing.T) {
	root := t.TempDir()
	statefilePath := filepath.Join(root, "state.json")
	oldOwner := mustAuthority(t, statefilePath, filepath.Join(root, "old.toml"))
	currentOwner := mustAuthority(t, statefilePath, filepath.Join(root, "current.toml"))
	oldAddress := mustAddress(t, filepath.Join(root, "old"), "")
	currentAddress := mustAddress(t, filepath.Join(root, "current"), "")

	oldActive, err := outputownership.NewActiveClaim(oldAddress, oldOwner)
	if err != nil {
		t.Fatal(err)
	}
	release, err := NewReleaseTransition(oldActive)
	if err != nil {
		t.Fatal(err)
	}
	acquire, err := NewAcquireTransition(currentAddress, currentOwner, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewClaimTransitionSet([]ClaimTransition{acquire, release})
	if err != nil {
		t.Fatalf("NewClaimTransitionSet returned error: %v", err)
	}

	initial, err := outputownership.NewRegistry([]outputownership.Claim{oldActive})
	if err != nil {
		t.Fatal(err)
	}
	preparation, err := set.Preparation()
	if err != nil {
		t.Fatal(err)
	}
	prepared, changed, err := preparation.Apply(initial)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("preparation did not change the registry")
	}
	if retained, present := prepared.Exact(oldAddress); !present || !retained.Equal(oldActive) {
		t.Fatalf("retained claim = %#v, %t, want exact old provenance", retained, present)
	}
	reserved, present := prepared.Exact(currentAddress)
	if !present || reserved.State() != outputownership.ClaimReserved || !reserved.Owner().ExactEqual(currentOwner) {
		t.Fatalf("reserved claim = %#v, %t, want current provenance", reserved, present)
	}

	rollback, err := set.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, changed, err := rollback.Apply(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("rollback did not change the registry")
	}
	if restored, present := rolledBack.Exact(oldAddress); !present || !restored.Equal(oldActive) {
		t.Fatalf("restored claim = %#v, %t, want exact old provenance", restored, present)
	}
	if _, present := rolledBack.Exact(currentAddress); present {
		t.Fatal("rollback retained the current-provenance acquisition")
	}
}

func TestClaimTransitionSetRejectsDifferentOperations(t *testing.T) {
	root := t.TempDir()
	authority := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	left, err := NewAcquireTransition(mustAddress(t, filepath.Join(root, "left"), ""), authority, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewAcquireTransition(mustAddress(t, filepath.Join(root, "right"), ""), authority, "operation-2")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewClaimTransitionSet([]ClaimTransition{right, left}); err == nil {
		t.Fatal("NewClaimTransitionSet accepted different operations")
	}
}
