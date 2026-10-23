package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
)

func testDocumentBinding(t *testing.T, root, ownerName, destination, physical string) DocumentBinding {
	t.Helper()
	owner := mustAuthority(t, filepath.Join(root, ownerName, "state.json"), filepath.Join(root, ownerName, "daem.toml"))
	logical, err := output.Parse(destination)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewDocumentBinding(owner, logical, mustPathAuthority(t, physical))
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestDocumentBindingsAreIndependentIdentityEvidence(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "config.json")
	alpha := testDocumentBinding(t, root, "alpha", "@pi-agent/settings.json", physical)
	beta := testDocumentBinding(t, root, "beta", "@pi-agent/settings.json", physical)
	registry, err := NewRegistry(nil, beta, alpha)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Claims()) != 0 {
		t.Fatal("identity bindings fabricated exclusive claims")
	}
	if _, conflict := registry.Conflict(mustAddress(t, physical, "")); conflict {
		t.Fatal("identity binding blocked another owner's claim")
	}
	for _, binding := range []DocumentBinding{alpha, beta} {
		actual, present := registry.DocumentBinding(binding.Owner().Key(), binding.Destination())
		if !present || !actual.Equal(binding) {
			t.Fatal("binding lost its authoritative owner or logical document")
		}
	}
	bindings := registry.DocumentBindings()
	bindings[0] = DocumentBinding{}
	if !registry.DocumentBindings()[0].Equal(alpha) {
		t.Fatal("caller mutated retained binding values")
	}
	reordered, err := NewRegistry(nil, alpha, beta)
	if err != nil || !registry.Equal(reordered) {
		t.Fatalf("binding order changed registry identity: %v", err)
	}
	if registry.Equal(EmptyRegistry()) {
		t.Fatal("registry equality ignored the binding family")
	}
}

func TestDocumentBindingKeyExcludesDiagnosticManifestProvenance(t *testing.T) {
	root := t.TempDir()
	first := testDocumentBinding(t, root, "owner", ".pi/settings.json", filepath.Join(root, "config.json"))
	otherOwner, err := stateauthority.New(first.Owner().StatefileAuthority(), filepath.Join(root, "alias.toml"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewDocumentBinding(otherOwner, first.Destination(), first.PathAuthority())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(nil, first)
	if err != nil {
		t.Fatal(err)
	}
	if actual, present := registry.DocumentBinding(otherOwner.Key(), first.Destination()); !present || !actual.Equal(first) {
		t.Fatal("diagnostic manifest path split one state authority")
	}
	if first.Equal(other) {
		t.Fatal("exact record comparison discarded diagnostic provenance")
	}
	if _, err := NewRegistry(nil, first, other); err == nil {
		t.Fatal("duplicate binding identity was accepted")
	}
	if _, err := NewRegistry(nil, first, first); err == nil {
		t.Fatal("repeated identical binding was accepted")
	}
	if _, err := NewRegistry(nil, DocumentBinding{}); err == nil {
		t.Fatal("zero binding was accepted")
	}
}

func TestBindingTransitionsPreserveClaimsAndRefuseStaleInputs(t *testing.T) {
	root := t.TempDir()
	binding := testDocumentBinding(t, root, "owner", "@pi-agent/settings.json", filepath.Join(root, "before.json"))
	claim, err := NewActiveClaim(mustAddress(t, filepath.Join(root, "elsewhere"), ""), binding.Owner())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry([]Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	present, err := PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	next, err := registry.ApplyDocumentBinding(binding.Owner().Key(), binding.Destination(), NoDocumentBinding(), present)
	if err != nil {
		t.Fatal(err)
	}
	if actual, ok := next.Exact(claim.Address()); !ok || !actual.Equal(claim) {
		t.Fatal("binding transition changed a claim")
	}
	if _, found := registry.DocumentBinding(binding.Owner().Key(), binding.Destination()); found {
		t.Fatal("binding transition mutated its input")
	}
	if _, err := next.ApplyDocumentBinding(binding.Owner().Key(), binding.Destination(), NoDocumentBinding(), present); err == nil {
		t.Fatal("stale expected absence was accepted")
	}
	foreign := testDocumentBinding(t, root, "foreign", "@pi-agent/settings.json", filepath.Join(root, "before.json"))
	foreignValue, _ := PresentDocumentBinding(foreign)
	if _, err := next.ApplyDocumentBinding(binding.Owner().Key(), binding.Destination(), present, foreignValue); err == nil {
		t.Fatal("binding transition changed its owner key")
	}
	cleared, err := next.ApplyDocumentBinding(binding.Owner().Key(), binding.Destination(), present, NoDocumentBinding())
	if err != nil || !cleared.Equal(registry) {
		t.Fatalf("binding release changed unrelated facts: %v", err)
	}
}

func TestClaimMutationAndConvergenceRetainDocumentBindings(t *testing.T) {
	root := t.TempDir()
	binding := testDocumentBinding(t, root, "owner", "@pi-agent/settings.json", filepath.Join(root, "config.json"))
	registry, err := NewRegistry(nil, binding)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := NewActiveClaim(mustAddress(t, filepath.Join(root, "other"), ""), binding.Owner())
	if err != nil {
		t.Fatal(err)
	}
	present, _ := PresentClaim(claim)
	added, err := registry.Apply(claim.Address(), NoClaim(), present)
	if err != nil {
		t.Fatal(err)
	}
	change, _ := NewClaimChange(claim.Address(), present, NoClaim())
	convergence, _ := NewClaimConvergence([]ClaimChange{change})
	removed, changed, err := convergence.Apply(added)
	if err != nil || !changed || !removed.Equal(registry) {
		t.Fatalf("claim convergence dropped bindings: %v", err)
	}
	unchanged, changed, err := convergence.Apply(removed)
	if err != nil || changed || !unchanged.Equal(registry) {
		t.Fatalf("no-op convergence changed binding evidence: %v", err)
	}
}

func TestAuthorityTransferMovesOnlyOwnedBindingsAndDetectsCollision(t *testing.T) {
	root := t.TempDir()
	old := testDocumentBinding(t, root, "old", "@pi-agent/settings.json", filepath.Join(root, "config.json"))
	foreign := testDocumentBinding(t, root, "foreign", "@pi-agent/settings.json", filepath.Join(root, "config.json"))
	nextOwner := mustAuthority(t, filepath.Join(root, "new", "state.json"), filepath.Join(root, "new.toml"))
	registry, err := NewRegistry(nil, old, foreign)
	if err != nil {
		t.Fatal(err)
	}
	transferred, err := registry.TransferAuthority(old.Owner(), nextOwner)
	if err != nil {
		t.Fatal(err)
	}
	actual, found := transferred.DocumentBinding(nextOwner.Key(), old.Destination())
	if !found || actual.Owner().ManifestPath() != old.Owner().ManifestPath() || !actual.PathAuthority().Equal(old.PathAuthority()) {
		t.Fatal("authority transfer changed physical identity or diagnostic provenance")
	}
	if _, found := transferred.DocumentBinding(old.Owner().Key(), old.Destination()); found {
		t.Fatal("authority transfer retained old authoritative key")
	}
	if actual, found := transferred.DocumentBinding(foreign.Owner().Key(), foreign.Destination()); !found || !actual.Equal(foreign) {
		t.Fatal("authority transfer changed a foreign binding")
	}
	if _, err := registry.TransferAuthority(old.Owner(), foreign.Owner()); err == nil {
		t.Fatal("authority transfer merged distinct document bindings")
	}
}
