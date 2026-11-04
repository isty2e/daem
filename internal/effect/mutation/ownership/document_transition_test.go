package ownership

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

func TestDocumentClaimTransitionKeepsExclusionAcrossReshaping(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	path := filepath.Join(root, "settings.json")
	parent := transitionDocumentClaim(t, path, owner, "a")
	left := transitionDocumentClaim(t, path, owner, "a", "b")
	right := transitionDocumentClaim(t, path, owner, "a", "c")
	whole := transitionDocumentClaim(t, path, owner)
	reservedParent, err := outputownership.NewReservedClaim(parent.Address(), owner, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	reservedWhole, err := outputownership.NewReservedClaim(whole.Address(), owner, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name                    string
		before, prepared, after []outputownership.Claim
	}{
		{name: "expand", before: []outputownership.Claim{left, right}, prepared: []outputownership.Claim{reservedParent}, after: []outputownership.Claim{parent}},
		{name: "contract", before: []outputownership.Claim{parent}, prepared: []outputownership.Claim{parent}, after: []outputownership.Claim{left, right}},
		{name: "whole geometry", before: []outputownership.Claim{left, right}, prepared: []outputownership.Claim{reservedWhole}, after: []outputownership.Claim{whole}},
		{name: "acquire", prepared: []outputownership.Claim{reservedParent}, after: []outputownership.Claim{parent}},
		{name: "retire", before: []outputownership.Claim{parent}, prepared: []outputownership.Claim{parent}},
		{name: "retain", before: []outputownership.Claim{parent}, prepared: []outputownership.Claim{parent}, after: []outputownership.Claim{parent}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transition, err := NewDocumentClaimTransition(test.before, test.after, "operation-1")
			if err != nil {
				t.Fatal(err)
			}
			if !transitionClaimsEqual(transition.Prepared(), test.prepared) {
				t.Fatal("prepared exclusion differs from actual coarsest input units")
			}
			before, err := outputownership.NewRegistry(test.before)
			if err != nil {
				t.Fatal(err)
			}
			prepare, err := transition.Preparation()
			if err != nil {
				t.Fatal(err)
			}
			prepared := applyDocumentTransitionPhase(t, before, prepare)
			for _, claim := range append(slices.Clone(test.before), test.after...) {
				if _, excluded := prepared.Conflict(claim.Address()); !excluded {
					t.Fatalf("prepared phase left an acquisition gap at %v", claim.Address())
				}
			}
			finalize, err := transition.Finalization()
			if err != nil {
				t.Fatal(err)
			}
			after := applyDocumentTransitionPhase(t, prepared, finalize)
			if !transitionClaimsEqual(after.Claims(), test.after) {
				t.Fatal("finalization did not replace complete prepared exclusion")
			}
			rollback, err := transition.Rollback()
			if err != nil {
				t.Fatal(err)
			}
			if !applyDocumentTransitionPhase(t, prepared, rollback).Equal(before) {
				t.Fatal("rollback did not restore the exact before claim set")
			}
			if values := transition.Prepared(); len(values) != 0 {
				values[0] = outputownership.Claim{}
				if transition.Prepared()[0].Validate() != nil {
					t.Fatal("caller mutation changed prepared claims")
				}
			}
		})
	}
}

func TestDocumentClaimTransitionRejectsInvalidLifecycleAndProvenance(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	claim := transitionDocumentClaim(t, filepath.Join(root, "settings.json"), owner, "a")
	reserved, err := outputownership.NewReservedClaim(claim.Address(), owner, "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	aliasOwner := mustAuthority(t, owner.StatefileKey(), filepath.Join(root, "alias.toml"))
	aliasClaim, err := outputownership.NewActiveClaim(claim.Address(), aliasOwner)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		before, after []outputownership.Claim
		operation     string
	}{
		{before: []outputownership.Claim{reserved}},
		{after: []outputownership.Claim{reserved}},
		{before: []outputownership.Claim{claim}, after: []outputownership.Claim{aliasClaim}},
		{after: []outputownership.Claim{claim}},
		{after: []outputownership.Claim{claim}, operation: "../unsafe"},
	} {
		if _, err := NewDocumentClaimTransition(test.before, test.after, test.operation); err == nil {
			t.Fatal("invalid document transition was accepted")
		}
	}
	for _, after := range [][]outputownership.Claim{nil, {claim}} {
		if _, err := NewDocumentClaimTransition([]outputownership.Claim{claim}, after, ""); err != nil {
			t.Fatalf("no-acquisition transition unnecessarily requires operation identity: %v", err)
		}
	}
	var zero DocumentClaimTransition
	for _, phase := range []func() (outputownership.DocumentClaimChange, error){zero.Preparation, zero.Finalization, zero.Rollback} {
		if change, err := phase(); err == nil || change.Validate() == nil {
			t.Fatal("zero transition exposed a usable phase")
		}
	}
}

func transitionDocumentClaim(t *testing.T, path string, owner stateauthority.Authority, keys ...string) outputownership.Claim {
	t.Helper()
	address := mustAddress(t, path, "")
	if len(keys) != 0 {
		key, err := output.NewKeyPath(keys...)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		address, err = outputownership.NewManagedAddress(address.PathAuthority(), footprint)
		if err != nil {
			t.Fatal(err)
		}
	}
	claim, err := outputownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func applyDocumentTransitionPhase(t *testing.T, registry outputownership.Registry, change outputownership.DocumentClaimChange) outputownership.Registry {
	t.Helper()
	base, err := outputownership.NewClaimConvergence(nil)
	if err != nil {
		t.Fatal(err)
	}
	convergence, err := base.WithDocumentChanges([]outputownership.DocumentClaimChange{change})
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := convergence.Apply(registry)
	if err != nil {
		t.Fatal(err)
	}
	if again, changed, err := convergence.Apply(result); err != nil || changed || !again.Equal(result) {
		t.Fatalf("phase retry is not exact and idempotent: %v", err)
	}
	return result
}

func transitionClaimsEqual(left, right []outputownership.Claim) bool {
	return slices.EqualFunc(left, right, func(left, right outputownership.Claim) bool { return left.Equal(right) })
}
