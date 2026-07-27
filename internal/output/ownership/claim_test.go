package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

func TestClaimStateInvariants(t *testing.T) {
	root := t.TempDir()
	address := mustAddress(t, filepath.Join(root, "AGENTS.md"), "")
	authority := mustAuthority(t, filepath.Join(root, ".daem", "state.json"), filepath.Join(root, "daem.toml"))

	reserved, err := NewReservedClaim(address, authority, "operation-1")
	if err != nil {
		t.Fatalf("NewReservedClaim returned error: %v", err)
	}
	if reserved.State() != ClaimReserved || reserved.OperationID() != "operation-1" || !reserved.OwnedBy(authority) {
		t.Fatalf("unexpected reserved claim: %#v", reserved)
	}
	active, err := NewActiveClaim(address, authority)
	if err != nil {
		t.Fatalf("NewActiveClaim returned error: %v", err)
	}
	if active.State() != ClaimActive || active.OperationID() != "" || !active.Address().Equal(address) {
		t.Fatalf("unexpected active claim: %#v", active)
	}
	if _, err := NewReservedClaim(address, authority, ""); err == nil {
		t.Fatal("reserved claim accepted an empty operation id")
	}
	if _, err := NewReservedClaim(address, authority, "../unsafe"); err == nil {
		t.Fatal("reserved claim accepted an unsafe operation id")
	}
	if _, err := NewReservedClaim(address, authority, "unsafe:windows"); err == nil {
		t.Fatal("reserved claim accepted a non-portable operation id")
	}
}

func TestClaimConflictsIgnoreIdenticalContentAndSubjectConcepts(t *testing.T) {
	root := t.TempDir()
	address := mustAddress(t, filepath.Join(root, "AGENTS.md"), "")
	leftOwner := mustAuthority(t, filepath.Join(root, "left", "state.json"), filepath.Join(root, "left.toml"))
	rightOwner := mustAuthority(t, filepath.Join(root, "right", "state.json"), filepath.Join(root, "right.toml"))
	left, _ := NewActiveClaim(address, leftOwner)
	right, _ := NewActiveClaim(address, rightOwner)
	if !left.ConflictsWith(right) {
		t.Fatal("same address must conflict regardless of external content or subject equality")
	}
}

func mustAuthority(t *testing.T, statefilePath string, manifestPath string) stateauthority.Authority {
	t.Helper()
	authority, err := stateauthority.New(filepath.Clean(statefilePath), filepath.Clean(manifestPath))
	if err != nil {
		t.Fatalf("stateauthority.New returned error: %v", err)
	}
	return authority
}
