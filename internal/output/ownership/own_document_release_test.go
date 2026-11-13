package ownership

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

func ownReleaseForTest(t *testing.T, binding DocumentBinding, before []Claim, removed []ManagedAddress, retireBinding bool) OwnDocumentRelease {
	t.Helper()
	scope, err := NewDocumentScope([]pathauthority.Exact{binding.PathAuthority()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	beforeBinding := NoDocumentBinding()
	if retireBinding || len(before) != 0 {
		beforeBinding = mustBindingValue(t, binding)
	}
	release, err := NewOwnDocumentRelease(OwnDocumentReleaseInput{
		Owner: binding.Owner(), Scope: scope,
		BeforeClaims: before, RemovalAddresses: removed, BindingDestination: binding.Destination(),
		BindingBefore: beforeBinding, RetainBinding: !retireBinding && len(before) != 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return release
}

func TestOwnDocumentReleaseKeepsSurvivingAndForeignMetadata(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "settings.json")
	owner := testDocumentBinding(t, root, "owner", ".pi/settings.json", physical)
	foreign := testDocumentBinding(t, root, "foreign", ".pi/settings.json", physical)
	removed := documentTestClaim(t, physical, owner.Owner(), "removed")
	retained := documentTestClaim(t, physical, owner.Owner(), "retained")
	foreignClaim := documentTestClaim(t, physical, foreign.Owner(), "foreign")
	before, err := NewRegistry([]Claim{retained, foreignClaim, removed}, owner, foreign)
	if err != nil {
		t.Fatal(err)
	}
	release := ownReleaseForTest(t, owner, []Claim{retained, removed}, []ManagedAddress{removed.Address()}, false)
	if err := release.RequireBefore(before); err != nil {
		t.Fatal(err)
	}
	next, changed, err := release.ApplyAfterCommit(before)
	want, wantErr := NewRegistry([]Claim{retained, foreignClaim}, owner, foreign)
	if err != nil || wantErr != nil || !changed || !next.Equal(want) {
		t.Fatal("scoped release changed surviving or foreign facts", err, wantErr)
	}
	again, changed, err := release.ApplyAfterCommit(next)
	if err != nil || changed || !again.Equal(want) {
		t.Fatal("own release was not idempotent", err)
	}
	if !errors.Is(release.RequireBefore(next), ErrOwnReleaseChanged) {
		t.Fatal("precommit validation repaired or accepted a missing own claim")
	}
	query := release.BeforeClaims()
	query[0] = Claim{}
	if err := release.RequireBefore(before); err != nil {
		t.Fatal("caller changed the retained release", err)
	}
}

func TestOwnDocumentReleaseDoesNotReacquireOverANewForeignOwner(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "settings.json")
	owner := testDocumentBinding(t, root, "old", ".pi/settings.json", physical)
	foreign := testDocumentBinding(t, root, "new", ".pi/settings.json", physical)
	removed := documentTestClaim(t, physical, owner.Owner(), "selected")
	release := ownReleaseForTest(t, owner, []Claim{removed}, []ManagedAddress{removed.Address()}, true)
	for _, reserved := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "reserved"}[reserved], func(t *testing.T) {
			address := mustAddress(t, physical, "")
			claim, err := NewActiveClaim(address, foreign.Owner())
			if reserved {
				claim, err = NewReservedClaim(address, foreign.Owner(), "new-operation")
			}
			if err != nil {
				t.Fatal(err)
			}
			registry, err := NewRegistry([]Claim{claim}, foreign, owner)
			if err != nil {
				t.Fatal(err)
			}
			next, changed, err := release.ApplyAfterCommit(registry)
			want, _ := NewRegistry([]Claim{claim}, foreign)
			if err != nil || !changed || !next.Equal(want) {
				t.Fatal("completion released or reacquired foreign authority", err)
			}
			claimless := ownReleaseForTest(t, owner, nil, nil, false)
			if err := claimless.RequireBefore(want); err != nil {
				t.Fatal("claimless release treated foreign metadata as its own", err)
			}
			result, changed, err := claimless.ApplyAfterCommit(want)
			if err != nil || changed || !result.Equal(want) {
				t.Fatal("claimless release changed another owner", err)
			}
		})
	}
}

func TestOwnDocumentReleaseRefusesSameOwnerDriftWithoutPartialSuccessor(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "settings.json")
	owner := testDocumentBinding(t, root, "owner", ".pi/settings.json", physical)
	removed := documentTestClaim(t, physical, owner.Owner(), "selected")
	retained := documentTestClaim(t, physical, owner.Owner(), "retained")
	release := ownReleaseForTest(t, owner, []Claim{removed, retained}, []ManagedAddress{removed.Address()}, false)
	provenance, err := stateauthority.New(owner.Owner().StatefileAuthority(), filepath.Join(root, "other.toml"))
	if err != nil {
		t.Fatal(err)
	}
	stale, _ := NewActiveClaim(removed.Address(), provenance)
	reserved, _ := NewReservedClaim(removed.Address(), owner.Owner(), "unrelated-operation")
	extra := documentTestClaim(t, physical, owner.Owner(), "unrecorded")
	for _, claims := range [][]Claim{{stale, retained}, {reserved, retained}, {removed, retained, extra}, {removed}} {
		registry, err := NewRegistry(claims, owner)
		if err != nil {
			t.Fatal(err)
		}
		if result, changed, err := release.ApplyAfterCommit(registry); !errors.Is(err, ErrOwnReleaseChanged) || changed || result.Equal(result) {
			t.Fatal("same-owner drift exposed a partial or unrecorded release", err)
		}
	}
}

func TestOwnDocumentReleaseKeepsProtectionAsABarrierEvenWhenAlreadyReleased(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "settings.json")
	owner := testDocumentBinding(t, root, "old", ".pi/settings.json", physical)
	foreign := testDocumentBinding(t, root, "new", ".pi/settings.json", physical)
	protection := testPendingProtection(t, foreign)
	registry, err := NewRegistryWithProtections(nil, []DocumentBinding{foreign}, []PendingProtection{protection})
	if err != nil {
		t.Fatal(err)
	}
	release := ownReleaseForTest(t, owner, nil, nil, false)
	var pending *ProtectionPendingError
	if !errors.As(release.RequireBefore(registry), &pending) {
		t.Fatal("claimless precommit release bypassed pending protection")
	}
	if result, changed, err := release.ApplyAfterCommit(registry); !errors.As(err, &pending) || changed || result.Equal(result) {
		t.Fatal("idempotent postcommit release bypassed pending protection", err)
	}
}

func TestOwnDocumentReleaseObservesBindingAbsenceInsteadOfIgnoringTheKey(t *testing.T) {
	root := t.TempDir()
	owner := testDocumentBinding(t, root, "owner", ".pi/settings.json", filepath.Join(root, "settings.json"))
	release := ownReleaseForTest(t, owner, nil, nil, false)
	registry, err := NewRegistry(nil, owner)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(release.RequireBefore(registry), ErrOwnReleaseChanged) {
		t.Fatal("claimless before accepted an unrecorded own binding")
	}
	if result, changed, err := release.ApplyAfterCommit(registry); !errors.Is(err, ErrOwnReleaseChanged) || changed || result.Equal(result) {
		t.Fatal("claimless completion ignored an unexpected own binding", err)
	}
}

func TestOwnDocumentReleaseRejectsUnscopedForeignAndUnrecordedInputs(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "settings.json")
	owner := testDocumentBinding(t, root, "owner", ".pi/settings.json", physical)
	foreign := testDocumentBinding(t, root, "foreign", ".pi/settings.json", physical)
	scope, err := NewDocumentScope([]pathauthority.Exact{owner.PathAuthority()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	claim := documentTestClaim(t, physical, owner.Owner(), "selected")
	foreignClaim := documentTestClaim(t, physical, foreign.Owner(), "selected")
	reserved, _ := NewReservedClaim(claim.Address(), owner.Owner(), "unrelated-operation")
	empty, _ := NewDocumentScope(nil, nil)
	base := OwnDocumentReleaseInput{Owner: owner.Owner(), Scope: scope, BindingDestination: owner.Destination()}
	for name, change := range map[string]func(*OwnDocumentReleaseInput){
		"empty-scope":     func(input *OwnDocumentReleaseInput) { input.Scope = empty },
		"foreign-claim":   func(input *OwnDocumentReleaseInput) { input.BeforeClaims = []Claim{foreignClaim} },
		"reserved-claim":  func(input *OwnDocumentReleaseInput) { input.BeforeClaims = []Claim{reserved} },
		"unknown-removal": func(input *OwnDocumentReleaseInput) { input.RemovalAddresses = []ManagedAddress{claim.Address()} },
		"repeated-removal": func(input *OwnDocumentReleaseInput) {
			input.BeforeClaims = []Claim{claim}
			input.RemovalAddresses = []ManagedAddress{claim.Address(), claim.Address()}
		},
		"foreign-binding":       func(input *OwnDocumentReleaseInput) { input.BindingBefore = mustBindingValue(t, foreign) },
		"retain-absent-binding": func(input *OwnDocumentReleaseInput) { input.RetainBinding = true },
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			change(&input)
			if _, err := NewOwnDocumentRelease(input); err == nil {
				t.Fatal("invalid release input was admitted")
			}
		})
	}
}

func TestOwnDocumentReleaseDoesNotSelectAParentOrProvisionalSibling(t *testing.T) {
	root := t.TempDir()
	namespace := filepath.Join(root, "settings")
	candidate := filepath.Join(namespace, "caf\u00e9.json")
	exact := pathtest.DarwinCaseSensitive(candidate)
	missing, err := pathauthority.NewProvisional(candidate, exact.Witness(), namespace, pathtest.DarwinCaseSensitive(namespace).Witness())
	if err != nil {
		t.Fatal(err)
	}
	owner := mustAuthority(t, filepath.Join(root, "owner", "state.json"), filepath.Join(root, "owner", "daem.toml"))
	selectedAddress, _ := NewManagedAddressFromLegacy(exact, "/selected")
	selected, _ := NewActiveClaim(selectedAddress, owner)
	logical := testDocumentBinding(t, root, "owner", ".pi/settings.json", candidate).Destination()
	parentAddress, _ := NewManagedAddressFromLegacy(pathtest.DarwinCaseSensitive(namespace), "")
	parent, _ := NewActiveClaim(parentAddress, owner)
	siblingAddress, _ := NewManagedAddressFromLegacy(pathtest.DarwinCaseSensitive(filepath.Join(namespace, "other.json")), "")
	sibling, _ := NewActiveClaim(siblingAddress, owner)
	for _, provisional := range []bool{false, true} {
		scope, err := NewDocumentScope([]pathauthority.Exact{exact}, nil)
		if provisional {
			scope, err = NewDocumentScope(nil, []pathauthority.Provisional{missing})
		}
		if err != nil {
			t.Fatal(err)
		}
		input := OwnDocumentReleaseInput{
			Owner: owner, Scope: scope, BeforeClaims: []Claim{selected},
			RemovalAddresses: []ManagedAddress{selected.Address()}, BindingDestination: logical,
		}
		release, err := NewOwnDocumentRelease(input)
		if err != nil {
			t.Fatal(err)
		}
		for _, unrelated := range []Claim{parent, sibling} {
			registry, err := NewRegistry([]Claim{unrelated})
			if err != nil {
				t.Fatal(err)
			}
			result, changed, err := release.ApplyAfterCommit(registry)
			if err != nil || changed || !result.Equal(registry) {
				t.Fatal("possible protection overlap selected unrelated own metadata", provisional, err)
			}
			input.BeforeClaims, input.RemovalAddresses = []Claim{unrelated}, []ManagedAddress{unrelated.Address()}
			if _, err := NewOwnDocumentRelease(input); err == nil {
				t.Fatal("possible overlap authorized a parent or sibling removal", provisional)
			}
		}
	}
}
