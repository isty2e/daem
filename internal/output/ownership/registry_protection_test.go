package ownership

import (
	"path/filepath"
	"testing"
)

func TestDocumentConvergencePreservesForeignProtection(t *testing.T) {
	root := t.TempDir()
	protected := testDocumentBinding(t, root, "protected", "@pi-agent/settings.json", filepath.Join(root, "settings.json"))
	protection := testPendingProtection(t, protected)
	other := testDocumentBinding(t, root, "other", "@codex-home/config.toml", filepath.Join(root, "config.toml"))
	before, _ := NewActiveClaim(mustAddress(t, other.PathAuthority().Key(), "/before"), other.Owner())
	after, _ := NewActiveClaim(mustAddress(t, other.PathAuthority().Key(), "/after"), other.Owner())
	registry, err := NewRegistryWithProtections([]Claim{before}, []DocumentBinding{protected, other}, []PendingProtection{protection})
	if err != nil {
		t.Fatal(err)
	}
	change, err := NewDocumentClaimChange([]Claim{before}, []Claim{after})
	if err != nil {
		t.Fatal(err)
	}
	convergence, _ := NewClaimConvergence(nil)
	convergence, err = convergence.WithDocumentChanges([]DocumentClaimChange{change})
	if err != nil {
		t.Fatal(err)
	}
	next, changed, err := convergence.Apply(registry)
	if err != nil || !changed || len(next.PendingProtections()) != 1 || !next.PendingProtections()[0].Equal(protection) {
		t.Fatalf("document fragment convergence lost protection: %v", err)
	}
	if _, found := next.Exact(after.Address()); !found {
		t.Fatal("disjoint document fragment did not converge")
	}
	otherValue, _ := PresentDocumentBinding(other)
	intent, err := NewExactDocumentBindingIntent(other.Owner(), other.Destination(), otherValue, NoDocumentBinding())
	if err != nil {
		t.Fatal(err)
	}
	bindings, _ := NewDocumentBindingConvergence([]DocumentBindingIntent{intent})
	next, changed, err = bindings.Apply(next)
	if err != nil || !changed || !next.PendingProtections()[0].Equal(protection) {
		t.Fatalf("disjoint binding convergence lost protection: %v", err)
	}
	newOwner := mustAuthority(t, filepath.Join(root, "other-next", "state.json"), filepath.Join(root, "other-next", "daem.toml"))
	transferred, err := next.TransferAuthority(other.Owner(), newOwner)
	if err != nil || !transferred.PendingProtections()[0].Equal(protection) {
		t.Fatalf("unrelated authority transfer changed protection: %v", err)
	}
	newBinding := testDocumentBinding(t, root, "another", ".pi/settings.json", filepath.Join(root, "another.json"))
	newValue, _ := PresentDocumentBinding(newBinding)
	added, err := transferred.ApplyDocumentBinding(newBinding.Owner().Key(), newBinding.Destination(), NoDocumentBinding(), newValue)
	if err != nil || !added.PendingProtections()[0].Equal(protection) {
		t.Fatalf("exact disjoint binding change lost protection: %v", err)
	}
}

func TestProtectionRefusesEveryCohortOwnerTransferEvenWithoutOrdinaryFacts(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "settings.json")
	alpha := testDocumentBinding(t, root, "alpha", "@pi-agent/settings.json", physical)
	beta := testDocumentBinding(t, root, "beta", ".pi/settings.json", physical)
	protection := testPendingProtection(t, alpha, beta)
	registry, err := NewRegistryWithProtections(nil, nil, []PendingProtection{protection})
	if err != nil {
		t.Fatal(err)
	}
	newOwner := mustAuthority(t, filepath.Join(root, "new", "state.json"), filepath.Join(root, "new", "daem.toml"))
	for _, binding := range []DocumentBinding{alpha, beta} {
		if err := registry.RequireOwnerSettled(binding.Owner().Key()); err == nil {
			t.Fatal("pending-only cohort owner was considered retired")
		}
		if _, err := registry.TransferAuthority(binding.Owner(), newOwner); err == nil {
			t.Fatal("pending-only owner or participant was transferred")
		}
	}
	projections := registry.PendingProtections()
	projections[0] = PendingProtection{}
	if err := registry.PendingProtections()[0].Validate(); err != nil {
		t.Fatalf("caller changed a registry projection: %v", err)
	}
}

func TestProtectionConvergenceCannotChangeItsFrozenScope(t *testing.T) {
	root := t.TempDir()
	binding := testDocumentBinding(t, root, "owner", "@pi-agent/settings.json", filepath.Join(root, "settings.json"))
	protection := testPendingProtection(t, binding)
	registry, err := NewRegistryWithProtections(nil, []DocumentBinding{binding}, []PendingProtection{protection})
	if err != nil {
		t.Fatal(err)
	}
	narrower, _ := NewProtectedDocument([]DocumentBinding{binding}, 501, 20, 0o600)
	modified, err := NewArmedProtection(protection.Owner(), protection.Invocation(), protection.Correlation(), []ProtectedDocument{narrower})
	if err != nil {
		t.Fatal(err)
	}
	arm, _ := NewArmProtection(modified)
	if _, _, err := registry.ApplyProtection(binding.Owner(), arm); err == nil {
		t.Fatal("arming changed an existing cap")
	}
	value, _ := PresentDocumentBinding(binding)
	intent, _ := NewExactDocumentBindingIntent(binding.Owner(), binding.Destination(), value, NoDocumentBinding())
	bindings, _ := NewDocumentBindingConvergence([]DocumentBindingIntent{intent})
	if _, _, err := bindings.Apply(registry); err == nil {
		t.Fatal("binding convergence orphaned frozen scope")
	}
	claim, _ := NewActiveClaim(mustAddress(t, binding.PathAuthority().Key(), "/unrelated-key"), binding.Owner())
	document, err := NewDocumentClaimChange(nil, []Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	claims, _ := NewClaimConvergence(nil)
	claims, err = claims.WithDocumentChanges([]DocumentClaimChange{document})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := claims.Apply(registry); err == nil {
		t.Fatal("document fragment convergence bypassed the fence")
	}
	if _, err := NewRegistryWithProtections(nil, nil, []PendingProtection{protection, protection}); err == nil {
		t.Fatal("repeated invocation was admitted")
	}
}
