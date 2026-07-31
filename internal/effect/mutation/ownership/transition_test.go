package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

func TestAcquireTransitionIsAbsentReservedActive(t *testing.T) {
	root := t.TempDir()
	address := mustAddress(t, filepath.Join(root, "AGENTS.md"), "")
	authority := mustAuthority(t, filepath.Join(root, ".daem", "state.json"), filepath.Join(root, "daem.toml"))
	transition, err := NewAcquireTransition(address, authority, "operation-1")
	if err != nil {
		t.Fatalf("NewAcquireTransition returned error: %v", err)
	}
	if transition.Kind() != TransitionAcquire || !transition.Address().Equal(address) || !transition.Owner().Equal(authority) {
		t.Fatalf("unexpected acquire transition: %#v", transition)
	}
	if _, present := transition.Before().Get(); present {
		t.Fatal("acquire before claim is present")
	}
	prepared, present := transition.Prepared().Get()
	if !present || prepared.State() != outputownership.ClaimReserved || prepared.OperationID() != "operation-1" {
		t.Fatalf("unexpected prepared claim: %#v, present=%v", prepared, present)
	}
	after, present := transition.After().Get()
	if !present || after.State() != outputownership.ClaimActive || after.OperationID() != "" {
		t.Fatalf("unexpected after claim: %#v, present=%v", after, present)
	}

	equivalent, err := NewAcquireTransition(address, authority, "operation-1")
	if err != nil {
		t.Fatalf("construct equivalent acquire transition: %v", err)
	}
	different, err := NewAcquireTransition(address, authority, "operation-2")
	if err != nil {
		t.Fatalf("construct different acquire transition: %v", err)
	}
	if !transition.Equal(equivalent) {
		t.Fatal("equivalent acquire transitions differ")
	}
	if transition.Equal(different) {
		t.Fatal("different prepared operation retained transition equality")
	}
}

func TestReleaseTransitionRetainsAuthorityUntilFinalization(t *testing.T) {
	root := t.TempDir()
	address := mustAddress(t, filepath.Join(root, "AGENTS.md"), "")
	authority := mustAuthority(t, filepath.Join(root, ".daem", "state.json"), filepath.Join(root, "daem.toml"))
	active, _ := outputownership.NewActiveClaim(address, authority)
	transition, err := NewReleaseTransition(active)
	if err != nil {
		t.Fatalf("NewReleaseTransition returned error: %v", err)
	}
	if transition.Kind() != TransitionRelease {
		t.Fatalf("transition kind = %q, want %q", transition.Kind(), TransitionRelease)
	}
	before, beforePresent := transition.Before().Get()
	prepared, preparedPresent := transition.Prepared().Get()
	if !beforePresent || !preparedPresent || !before.Equal(active) || !prepared.Equal(active) {
		t.Fatal("release must retain the active claim through host and local-state commit")
	}
	if _, afterPresent := transition.After().Get(); afterPresent {
		t.Fatal("release after claim is present")
	}
}

func TestReleaseTransitionRejectsReservation(t *testing.T) {
	root := t.TempDir()
	address := mustAddress(t, filepath.Join(root, "AGENTS.md"), "")
	authority := mustAuthority(t, filepath.Join(root, ".daem", "state.json"), filepath.Join(root, "daem.toml"))
	reserved, _ := outputownership.NewReservedClaim(address, authority, "operation-1")
	if _, err := NewReleaseTransition(reserved); err == nil {
		t.Fatal("NewReleaseTransition accepted a reserved claim")
	}
}

func TestRetainTransitionRequiresAndPreservesActiveClaim(t *testing.T) {
	root := t.TempDir()
	address := mustAddress(t, filepath.Join(root, "AGENTS.md"), "")
	authority := mustAuthority(t, filepath.Join(root, ".daem", "state.json"), filepath.Join(root, "daem.toml"))
	active, _ := outputownership.NewActiveClaim(address, authority)
	transition, err := NewRetainTransition(active)
	if err != nil {
		t.Fatalf("NewRetainTransition returned error: %v", err)
	}
	if transition.Kind() != TransitionRetain || !transition.Before().Equal(transition.Prepared()) || !transition.Prepared().Equal(transition.After()) {
		t.Fatalf("unexpected retain transition: %#v", transition)
	}
	reserved, _ := outputownership.NewReservedClaim(address, authority, "operation-1")
	if _, err := NewRetainTransition(reserved); err == nil {
		t.Fatal("NewRetainTransition accepted a reservation")
	}
}

func TestTransitionRejectsZeroAndIllegalPhaseCombinations(t *testing.T) {
	var zero ClaimTransition
	if err := zero.Validate(); err == nil {
		t.Fatal("zero ClaimTransition validated")
	}

	root := t.TempDir()
	address := mustAddress(t, filepath.Join(root, "AGENTS.md"), "")
	authority := mustAuthority(t, filepath.Join(root, ".daem", "state.json"), filepath.Join(root, "daem.toml"))
	active, err := outputownership.NewActiveClaim(address, authority)
	if err != nil {
		t.Fatalf("NewActiveClaim returned error: %v", err)
	}
	activeValue, err := outputownership.PresentClaim(active)
	if err != nil {
		t.Fatalf("PresentClaim returned error: %v", err)
	}
	reserved, err := outputownership.NewReservedClaim(address, authority, "operation-1")
	if err != nil {
		t.Fatalf("NewReservedClaim returned error: %v", err)
	}
	reservedValue, err := outputownership.PresentClaim(reserved)
	if err != nil {
		t.Fatalf("PresentClaim returned error: %v", err)
	}
	otherAuthority := mustAuthority(
		t,
		authority.StatefileKey(),
		filepath.Join(root, "other-daem.toml"),
	)
	otherActive, err := outputownership.NewActiveClaim(address, otherAuthority)
	if err != nil {
		t.Fatalf("NewActiveClaim returned error: %v", err)
	}
	otherActiveValue, err := outputownership.PresentClaim(otherActive)
	if err != nil {
		t.Fatalf("PresentClaim returned error: %v", err)
	}

	tests := []struct {
		name     string
		kind     TransitionKind
		before   outputownership.ClaimValue
		prepared outputownership.ClaimValue
		after    outputownership.ClaimValue
	}{
		{
			name: "acquire starts active",
			kind: TransitionAcquire, before: activeValue, prepared: activeValue, after: activeValue,
		},
		{
			name: "acquire changes owner provenance",
			kind: TransitionAcquire, before: outputownership.NoClaim(), prepared: reservedValue, after: otherActiveValue,
		},
		{
			name: "release loses authority before finalization",
			kind: TransitionRelease, before: activeValue, prepared: outputownership.NoClaim(), after: outputownership.NoClaim(),
		},
		{
			name: "retain drops claim",
			kind: TransitionRetain, before: activeValue, prepared: activeValue, after: outputownership.NoClaim(),
		},
		{
			name: "unknown kind",
			kind: "future", before: activeValue, prepared: activeValue, after: activeValue,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewTransition(test.kind, test.before, test.prepared, test.after); err == nil {
				t.Fatal("NewTransition accepted illegal phases")
			}
		})
	}
}

func TestTransitionRejectsCrossPhaseIdentityAndStateDrift(t *testing.T) {
	root := t.TempDir()
	address := mustAddress(t, filepath.Join(root, "AGENTS.md"), "")
	otherAddress := mustAddress(t, filepath.Join(root, "CLAUDE.md"), "")
	authority := mustAuthority(t, filepath.Join(root, ".daem", "state.json"), filepath.Join(root, "daem.toml"))
	otherAuthority := mustAuthority(t, filepath.Join(root, ".daem", "other-state.json"), filepath.Join(root, "daem.toml"))

	active := mustClaimValue(t, func() (outputownership.Claim, error) {
		return outputownership.NewActiveClaim(address, authority)
	})
	reserved := mustClaimValue(t, func() (outputownership.Claim, error) {
		return outputownership.NewReservedClaim(address, authority, "operation-1")
	})
	otherAddressActive := mustClaimValue(t, func() (outputownership.Claim, error) {
		return outputownership.NewActiveClaim(otherAddress, authority)
	})
	otherAddressReserved := mustClaimValue(t, func() (outputownership.Claim, error) {
		return outputownership.NewReservedClaim(otherAddress, authority, "operation-1")
	})
	otherOwnerActive := mustClaimValue(t, func() (outputownership.Claim, error) {
		return outputownership.NewActiveClaim(address, otherAuthority)
	})

	tests := []struct {
		name     string
		kind     TransitionKind
		before   outputownership.ClaimValue
		prepared outputownership.ClaimValue
		after    outputownership.ClaimValue
	}{
		{
			name: "acquire changes address",
			kind: TransitionAcquire, before: outputownership.NoClaim(), prepared: reserved, after: otherAddressActive,
		},
		{
			name: "acquire stays reserved",
			kind: TransitionAcquire, before: outputownership.NoClaim(), prepared: reserved, after: reserved,
		},
		{
			name: "release changes address while prepared",
			kind: TransitionRelease, before: active, prepared: otherAddressActive, after: outputownership.NoClaim(),
		},
		{
			name: "release changes owner while prepared",
			kind: TransitionRelease, before: active, prepared: otherOwnerActive, after: outputownership.NoClaim(),
		},
		{
			name: "retain changes owner",
			kind: TransitionRetain, before: active, prepared: active, after: otherOwnerActive,
		},
		{
			name: "retain uses reservation",
			kind: TransitionRetain, before: active, prepared: otherAddressReserved, after: active,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewTransition(test.kind, test.before, test.prepared, test.after); err == nil {
				t.Fatal("NewTransition accepted cross-phase drift")
			}
		})
	}
}

func TestTransitionRehydrationMatchesFamilyConstructors(t *testing.T) {
	root := t.TempDir()
	address := mustAddress(t, filepath.Join(root, "AGENTS.md"), "")
	authority := mustAuthority(t, filepath.Join(root, ".daem", "state.json"), filepath.Join(root, "daem.toml"))
	active, err := outputownership.NewActiveClaim(address, authority)
	if err != nil {
		t.Fatalf("NewActiveClaim returned error: %v", err)
	}

	transitions := make([]ClaimTransition, 0, 3)
	acquire, err := NewAcquireTransition(address, authority, "operation-1")
	if err != nil {
		t.Fatalf("NewAcquireTransition returned error: %v", err)
	}
	transitions = append(transitions, acquire)
	release, err := NewReleaseTransition(active)
	if err != nil {
		t.Fatalf("NewReleaseTransition returned error: %v", err)
	}
	transitions = append(transitions, release)
	retain, err := NewRetainTransition(active)
	if err != nil {
		t.Fatalf("NewRetainTransition returned error: %v", err)
	}
	transitions = append(transitions, retain)

	for _, transition := range transitions {
		rehydrated, err := NewTransition(
			transition.Kind(),
			transition.Before(),
			transition.Prepared(),
			transition.After(),
		)
		if err != nil {
			t.Fatalf("NewTransition(%s) returned error: %v", transition.Kind(), err)
		}
		if !rehydrated.Equal(transition) {
			t.Fatalf("rehydrated %s transition differs", transition.Kind())
		}
		if err := rehydrated.Validate(); err != nil {
			t.Fatalf("rehydrated %s transition failed validation: %v", transition.Kind(), err)
		}
	}
}

func TestAcquireTransitionRejectsUnsafeOperationIdentity(t *testing.T) {
	root := t.TempDir()
	address := mustAddress(t, filepath.Join(root, "AGENTS.md"), "")
	authority := mustAuthority(t, filepath.Join(root, ".daem", "state.json"), filepath.Join(root, "daem.toml"))

	for _, operationID := range []string{"", ".", "..", "bad/id", " bad", "bad\nid"} {
		t.Run(operationID, func(t *testing.T) {
			if _, err := NewAcquireTransition(address, authority, operationID); err == nil {
				t.Fatal("NewAcquireTransition accepted unsafe operation id")
			}
		})
	}
}

func mustAddress(t *testing.T, path string, contentPath string) outputownership.ManagedAddress {
	t.Helper()
	address, err := outputownership.NewManagedAddress(pathtest.Exact(path), contentPath)
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	return address
}

func mustAuthority(t *testing.T, statefilePath string, manifestPath string) stateauthority.Authority {
	t.Helper()
	authority, err := stateauthority.New(pathtest.Exact(statefilePath), manifestPath)
	if err != nil {
		t.Fatalf("stateauthority.New returned error: %v", err)
	}
	return authority
}

func mustClaimValue(
	t *testing.T,
	construct func() (outputownership.Claim, error),
) outputownership.ClaimValue {
	t.Helper()
	claim, err := construct()
	if err != nil {
		t.Fatalf("construct claim: %v", err)
	}
	value, err := outputownership.PresentClaim(claim)
	if err != nil {
		t.Fatalf("PresentClaim returned error: %v", err)
	}
	return value
}
