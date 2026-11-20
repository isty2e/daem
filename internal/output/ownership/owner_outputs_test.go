package ownership

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestOwnerOutputsRefuseReservedAndProtectedMembership(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "settings.json")
	foreign := testDocumentBinding(t, root, "foreign", "@pi-agent/settings.json", path)
	selected := testDocumentBinding(t, root, "selected", ".pi/settings.json", path)
	addr := mustAddress(t, path, "/owned")
	active, err := NewActiveClaim(addr, selected.Owner())
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := NewReservedClaim(addr, selected.Owner(), "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	before, err := NewRegistryWithProtections([]Claim{active}, []DocumentBinding{selected, foreign}, nil)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := before.SettledOwnerOutputs(selected.Owner().Key())
	if err != nil || approved.Empty() {
		t.Fatalf("fixture not settled: %v", err)
	}
	for _, test := range []struct {
		name       string
		claims     []Claim
		protection []PendingProtection
		want       error
	}{
		{name: "reservation", claims: []Claim{reserved}, want: ErrOwnerOutputsReserved},
		{name: "foreign cohort", claims: []Claim{active}, protection: []PendingProtection{testPendingProtection(t, foreign, selected)}},
		{name: "foreign scope", claims: []Claim{active}, protection: []PendingProtection{testPendingProtection(t, foreign)}},
		{name: "changed selected set", want: ErrOwnerOutputsChanged},
	} {
		t.Run(test.name, func(t *testing.T) {
			current, err := NewRegistryWithProtections(test.claims, []DocumentBinding{selected, foreign}, test.protection)
			if err != nil {
				t.Fatal(err)
			}
			_, err = current.WithoutSettledOwnerOutputs(approved)
			var protection *ProtectionPendingError
			if test.want != nil && !errors.Is(err, test.want) || test.want == nil && !errors.As(err, &protection) {
				t.Fatalf("release did not preserve the blocker: %v", err)
			}
		})
	}
}
