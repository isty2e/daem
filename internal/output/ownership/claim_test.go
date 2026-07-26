package ownership

import (
	"path/filepath"
	"testing"
)

func TestOwnerAuthorityEqualityUsesStatefileKey(t *testing.T) {
	root := t.TempDir()
	statefile := filepath.Join(root, ".daem", "state.json")
	left := mustAuthority(t, statefile, filepath.Join(root, "daem.toml"))
	right := mustAuthority(t, statefile, filepath.Join(root, "alternate.toml"))
	foreign := mustAuthority(t, filepath.Join(root, "other", ".daem", "state.json"), filepath.Join(root, "other", "daem.toml"))

	if !left.Equal(right) {
		t.Fatal("authorities with the same canonical statefile key must be equal")
	}
	if left.Equal(foreign) {
		t.Fatal("authorities with different statefile keys must not be equal")
	}
}

func TestOwnerAuthorityRejectsPartialOrUncleanIdentity(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name         string
		statefileKey string
		manifestPath string
	}{
		{name: "missing statefile", manifestPath: filepath.Join(root, "daem.toml")},
		{name: "relative statefile", statefileKey: ".daem/state.json", manifestPath: filepath.Join(root, "daem.toml")},
		{name: "missing manifest", statefileKey: filepath.Join(root, ".daem", "state.json")},
		{name: "unclean manifest", statefileKey: filepath.Join(root, ".daem", "state.json"), manifestPath: root + string(filepath.Separator) + "nested" + string(filepath.Separator) + ".." + string(filepath.Separator) + "daem.toml"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewOwnerAuthority(test.statefileKey, test.manifestPath); err == nil {
				t.Fatal("NewOwnerAuthority returned nil error")
			}
		})
	}
}

func TestOwnerAuthorityPreservesWhitespaceInCanonicalAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	statefileKey := filepath.Join(root, "state directory\n", "state.json")
	manifestPath := filepath.Join(root, "daem.toml\n")
	authority, err := NewOwnerAuthority(statefileKey, manifestPath)
	if err != nil {
		t.Fatalf("NewOwnerAuthority returned error: %v", err)
	}
	if authority.StatefileKey() != statefileKey || authority.ManifestPath() != manifestPath {
		t.Fatalf("authority = %#v, want exact whitespace-bearing paths", authority)
	}
}

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

func mustAuthority(t *testing.T, statefilePath string, manifestPath string) OwnerAuthority {
	t.Helper()
	authority, err := NewOwnerAuthority(filepath.Clean(statefilePath), filepath.Clean(manifestPath))
	if err != nil {
		t.Fatalf("NewOwnerAuthority returned error: %v", err)
	}
	return authority
}
