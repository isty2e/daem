package ownership

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/executionidentity"
)

func testPendingProtection(t *testing.T, bindings ...DocumentBinding) PendingProtection {
	t.Helper()
	document, err := NewProtectedDocument(bindings, 501, 20, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := executionidentity.NewInvocation(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	correlation, err := NewProtectionCorrelation("operation-1", strings.Repeat("b", 64), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	protection, err := NewArmedProtection(bindings[0].Owner(), invocation, correlation, []ProtectedDocument{document})
	if err != nil {
		t.Fatal(err)
	}
	return protection
}

func testProtectionExecution(t *testing.T, invocation executionidentity.Invocation) executionidentity.Binding {
	t.Helper()
	context, err := executionidentity.NewContext(executionidentity.Darwin, strings.Repeat("d", 64), "12345678-1234-1234-1234-123456789abc")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := executionidentity.NewBinding(invocation, context, 123)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestProtectionRetainsCanonicalImmutableScope(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "settings.json")
	alpha := testDocumentBinding(t, root, "alpha", "@pi-agent/settings.json", physical)
	beta := testDocumentBinding(t, root, "beta", ".pi/settings.json", physical)
	protection := testPendingProtection(t, alpha, beta)
	reorderedDocument, err := NewProtectedDocument([]DocumentBinding{beta, alpha}, 501, 20, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := NewArmedProtection(alpha.Owner(), protection.Invocation(), protection.Correlation(), []ProtectedDocument{reorderedDocument})
	if err != nil || !protection.Equal(reordered) || protection.Phase() != ProtectionArmed {
		t.Fatal("armed protection lost its scope")
	}
	documents := protection.Documents()
	bindings := documents[0].Bindings()
	bindings[0] = DocumentBinding{}
	documents[0] = ProtectedDocument{}
	if err := protection.Validate(); err != nil || len(protection.Documents()[0].Bindings()) != 2 {
		t.Fatalf("caller mutated the frozen obligation: %v", err)
	}
	bound, err := protection.Bind(testProtectionExecution(t, protection.Invocation()))
	if err != nil || bound.Phase() != ProtectionBound || protection.Phase() != ProtectionArmed || bound.Equal(protection) {
		t.Fatalf("binding did not retain a distinct immutable phase: %v", err)
	}
	if !bound.Documents()[0].Equal(protection.Documents()[0]) || bound.Correlation() != protection.Correlation() {
		t.Fatal("binding changed frozen targets or correlation")
	}
	if _, err := bound.Bind(testProtectionExecution(t, protection.Invocation())); err == nil {
		t.Fatal("bound protection accepted rebinding")
	}
	otherInvocation, _ := executionidentity.NewInvocation(strings.Repeat("e", 32))
	if _, err := protection.Bind(testProtectionExecution(t, otherInvocation)); err == nil {
		t.Fatal("another invocation's process was bound")
	}
}

func TestPermissionRestrictionNeverRestoresRemovedRights(t *testing.T) {
	root := t.TempDir()
	binding := testDocumentBinding(t, root, "owner", "@pi-agent/settings.json", filepath.Join(root, "settings.json"))
	for _, test := range []struct {
		cap, observed, want uint32
		changed             bool
	}{
		{0o600, 0o644, 0o600, true},
		{0o640, 0o604, 0o600, true},
		{0o644, 0o600, 0o600, false},
		{0o640, 0o640, 0o640, false},
		{0o400, 0o444, 0o400, true},
		{0o700, 0o744, 0o700, true},
		{0o000, 0o044, 0o000, true},
	} {
		document, err := NewProtectedDocument([]DocumentBinding{binding}, 501, 20, test.cap)
		if err != nil {
			t.Fatal(err)
		}
		got, changed, err := document.Restriction(501, 20, test.observed)
		if err != nil || got != test.want || changed != test.changed || got & ^test.observed != 0 {
			t.Fatalf("restriction of %04o under %04o = %04o, %v: %v", test.observed, test.cap, got, changed, err)
		}
	}
	document := testPendingProtection(t, binding).Documents()[0]
	for _, test := range []struct{ uid, gid, mode uint32 }{{502, 20, 0o644}, {501, 21, 0o644}, {501, 20, 0o440}, {501, 20, 0o740}, {501, 20, 0o4640}} {
		if _, _, err := document.Restriction(test.uid, test.gid, test.mode); err == nil {
			t.Fatal("unqualified owner or special/owner mode change admitted chmod")
		}
	}
}

func TestProtectionRejectsIncompleteOrOverlappingScope(t *testing.T) {
	root := t.TempDir()
	binding := testDocumentBinding(t, root, "owner", "@pi-agent/settings.json", filepath.Join(root, "config"))
	foreign := testDocumentBinding(t, root, "foreign", ".pi/settings.json", filepath.Join(root, "other"))
	for _, bindings := range [][]DocumentBinding{nil, {DocumentBinding{}}, {binding, binding}, {binding, foreign}} {
		if _, err := NewProtectedDocument(bindings, 0, 0, 0o600); err == nil {
			t.Fatal("incomplete or contradictory binding cohort was admitted")
		}
	}
	for _, mode := range []uint32{0o1000, 0o1600, 0o4600} {
		if _, err := NewProtectedDocument([]DocumentBinding{binding}, 0, 0, mode); err == nil {
			t.Fatal("unsupported metadata ceiling was admitted")
		}
	}
	protection := testPendingProtection(t, binding)
	document := protection.Documents()[0]
	if _, err := NewArmedProtection(foreign.Owner(), protection.Invocation(), protection.Correlation(), []ProtectedDocument{document}); err == nil {
		t.Fatal("protection without its owner's binding was admitted")
	}
	if _, err := NewArmedProtection(binding.Owner(), executionidentity.Invocation{}, protection.Correlation(), []ProtectedDocument{document}); err == nil {
		t.Fatal("protection without invocation correlation was admitted")
	}
	if _, err := NewArmedProtection(binding.Owner(), protection.Invocation(), protection.Correlation(), nil); err == nil {
		t.Fatal("empty protection scope was admitted")
	}
	if _, err := NewArmedProtection(binding.Owner(), protection.Invocation(), protection.Correlation(), []ProtectedDocument{document, document}); err == nil {
		t.Fatal("duplicate physical target was admitted")
	}
	sibling := testDocumentBinding(t, root, "owner", ".pi/settings.json", filepath.Join(root, "config-extra"))
	child := testDocumentBinding(t, root, "owner", ".codex/config.toml", filepath.Join(root, "config", "child"))
	siblingTarget, _ := NewProtectedDocument([]DocumentBinding{sibling}, 501, 20, 0o600)
	childTarget, _ := NewProtectedDocument([]DocumentBinding{child}, 501, 20, 0o600)
	if _, err := NewArmedProtection(binding.Owner(), protection.Invocation(), protection.Correlation(), []ProtectedDocument{document, siblingTarget, childTarget}); err == nil {
		t.Fatal("nonadjacent ancestor target overlap was admitted")
	}
	stateTarget := testDocumentBinding(t, root, "owner", "@pi-agent/settings.json", binding.Owner().StatefileKey())
	if _, err := NewProtectedDocument([]DocumentBinding{stateTarget}, 501, 20, 0o600); err == nil {
		t.Fatal("state authority became a chmod target")
	}
}

func TestProtectionTransitionsAreOwnerCheckedExactAndIdempotent(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "config.json")
	alpha := testDocumentBinding(t, root, "alpha", "@pi-agent/settings.json", physical)
	beta := testDocumentBinding(t, root, "beta", ".pi/settings.json", physical)
	registry, err := NewRegistry(nil, alpha, beta)
	if err != nil {
		t.Fatal(err)
	}
	protection := testPendingProtection(t, alpha, beta)
	arm, _ := NewArmProtection(protection)
	armed, changed, err := registry.ApplyProtection(alpha.Owner(), arm)
	if err != nil || !changed || len(armed.PendingProtections()) != 1 || registry.Equal(armed) {
		t.Fatalf("arm failed or lost identity: %v", err)
	}
	if next, changed, err := armed.ApplyProtection(alpha.Owner(), arm); err != nil || changed || !next.Equal(armed) {
		t.Fatalf("exact arm retry changed state: %v", err)
	}
	if _, _, err := armed.ApplyProtection(beta.Owner(), arm); err == nil {
		t.Fatal("foreign owner changed a protection")
	}
	if rows := armed.ProtectionsForOwner(alpha.Owner().Key()); len(rows) != 1 || !rows[0].Equal(protection) || len(armed.ProtectionsForOwner(beta.Owner().Key())) != 0 {
		t.Fatal("owner lookup lost pending-only authority")
	}
	bind, _ := NewBindProtection(protection, testProtectionExecution(t, protection.Invocation()))
	if _, _, err := registry.ApplyProtection(alpha.Owner(), bind); err == nil {
		t.Fatal("bind manufactured an unarmed protection")
	}
	bound, changed, err := armed.ApplyProtection(alpha.Owner(), bind)
	if err != nil || !changed {
		t.Fatalf("bind failed: %v", err)
	}
	if next, changed, err := bound.ApplyProtection(alpha.Owner(), bind); err != nil || changed || !next.Equal(bound) {
		t.Fatalf("bind retry changed state: %v", err)
	}
	staleClear, _ := NewClearProtection(protection)
	if _, _, err := bound.ApplyProtection(alpha.Owner(), staleClear); err == nil {
		t.Fatal("stale Armed observation cleared Bound protection")
	}
	actual, present := bound.Protection(protection.Invocation())
	if !present {
		t.Fatal("bound protection disappeared")
	}
	clear, _ := NewClearProtection(actual)
	cleared, changed, err := bound.ApplyProtection(alpha.Owner(), clear)
	if err != nil || !changed || !cleared.Equal(registry) {
		t.Fatalf("clear changed ordinary metadata: %v", err)
	}
	if retry, changed, err := cleared.ApplyProtection(alpha.Owner(), clear); err != nil || changed || !retry.Equal(cleared) {
		t.Fatalf("clear retry changed state: %v", err)
	}
	if _, _, err := armed.ApplyProtection(alpha.Owner(), ProtectionTransition{}); err == nil {
		t.Fatal("zero transition was accepted")
	}
}

func TestProtectionArmingRequiresCurrentCompleteCohort(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "config.json")
	alpha := testDocumentBinding(t, root, "alpha", "@pi-agent/settings.json", physical)
	beta := testDocumentBinding(t, root, "beta", ".pi/settings.json", physical)
	protection := testPendingProtection(t, alpha)
	arm, _ := NewArmProtection(protection)
	for _, bindings := range [][]DocumentBinding{nil, {alpha, beta}, {beta}} {
		registry, _ := NewRegistry(nil, bindings...)
		if _, _, err := registry.ApplyProtection(alpha.Owner(), arm); err == nil {
			t.Fatal("arming silently changed the recorded cohort")
		}
	}
}

func TestProtectionFencesAllOwnersAndPreservesDisjointFacts(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "config.json")
	binding := testDocumentBinding(t, root, "owner", "@pi-agent/settings.json", physical)
	protection := testPendingProtection(t, binding)
	registry, err := NewRegistryWithProtections(nil, []DocumentBinding{binding}, []PendingProtection{protection})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{physical, root, filepath.Join(physical, "child")} {
		var pending *ProtectionPendingError
		if err := registry.RequireWritable(mustPathAuthority(t, path)); !errors.As(err, &pending) || !pending.Protection.Equal(protection) {
			t.Fatalf("pending physical scope was writable: %v", err)
		}
	}
	for _, owner := range []string{"owner", "foreign"} {
		claimOwner := mustAuthority(t, filepath.Join(root, owner, "state.json"), filepath.Join(root, owner, "daem.toml"))
		claim, _ := NewActiveClaim(mustAddress(t, physical, "/other-key"), claimOwner)
		value, _ := PresentClaim(claim)
		if _, err := registry.Apply(claim.Address(), NoClaim(), value); err == nil {
			t.Fatal("disjoint key footprint bypassed document protection")
		}
	}
	presentBinding, _ := PresentDocumentBinding(binding)
	if _, err := registry.ApplyDocumentBinding(binding.Owner().Key(), binding.Destination(), presentBinding, NoDocumentBinding()); err == nil {
		t.Fatal("binding release orphaned protection")
	}
	if _, err := registry.TransferAuthority(binding.Owner(), mustAuthority(t, filepath.Join(root, "new-state"), filepath.Join(root, "daem.toml"))); err == nil {
		t.Fatal("authority transfer orphaned protection")
	}
	claim, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "disjoint"), ""), binding.Owner())
	value, _ := PresentClaim(claim)
	added, err := registry.Apply(claim.Address(), NoClaim(), value)
	if err != nil || len(added.PendingProtections()) != 1 || !added.PendingProtections()[0].Equal(protection) {
		t.Fatalf("disjoint claim change lost protection: %v", err)
	}
	change, _ := NewClaimChange(claim.Address(), value, NoClaim())
	convergence, _ := NewClaimConvergence([]ClaimChange{change})
	removed, changed, err := convergence.Apply(added)
	if err != nil || !changed || !removed.Equal(registry) {
		t.Fatalf("claim convergence lost protection: %v", err)
	}
	if _, overlap := registry.Conflict(mustAddress(t, physical, "")); overlap {
		t.Fatal("protection was conflated with an exclusive claim")
	}
}
