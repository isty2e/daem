package ownership

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

func TestRegistryRejectsOverlappingClaimsAndAcceptsDisjointProjections(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	alpha, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "config.json"), "/mcp/alpha"), owner)
	beta, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "config.json"), "/mcp/beta"), owner)
	if _, err := NewRegistry([]Claim{beta, alpha}); err != nil {
		t.Fatalf("NewRegistry rejected disjoint projections: %v", err)
	}

	whole, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "config.json"), ""), owner)
	if _, err := NewRegistry([]Claim{alpha, whole}); err == nil {
		t.Fatal("NewRegistry accepted overlapping claims")
	} else {
		var conflict *ConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("error = %T, want *ConflictError", err)
		}
	}
}

func TestRegistryApplyPreservesUnrelatedClaims(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	left, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "left"), ""), owner)
	right, _ := NewActiveClaim(mustAddress(t, filepath.Join(root, "right"), ""), owner)
	registry, _ := NewRegistry([]Claim{left})
	replacement, _ := PresentClaim(right)
	next, err := registry.Apply(right.Address(), NoClaim(), replacement)
	if err != nil {
		t.Fatalf("Registry.Apply returned error: %v", err)
	}
	if got, ok := next.Exact(left.Address()); !ok || !got.Equal(left) {
		t.Fatal("Registry.Apply changed an unrelated claim")
	}
	if got, ok := next.Exact(right.Address()); !ok || !got.Equal(right) {
		t.Fatal("Registry.Apply omitted the replacement claim")
	}
}

func TestRegistryApplyRejectsStaleExpectedClaim(t *testing.T) {
	root := t.TempDir()
	address := mustAddress(t, filepath.Join(root, "AGENTS.md"), "")
	leftOwner := mustAuthority(t, filepath.Join(root, "left", "state.json"), filepath.Join(root, "left.toml"))
	rightOwner := mustAuthority(t, filepath.Join(root, "right", "state.json"), filepath.Join(root, "right.toml"))
	left, _ := NewActiveClaim(address, leftOwner)
	right, _ := NewActiveClaim(address, rightOwner)
	registry, _ := NewRegistry([]Claim{left})
	expected, _ := PresentClaim(right)

	_, err := registry.Apply(address, expected, NoClaim())
	var stale *StaleClaimError
	if !errors.As(err, &stale) {
		t.Fatalf("Registry.Apply error = %v, want *StaleClaimError", err)
	}
}

func TestRegistryFindsExactAncestorOfProvisionalCandidate(t *testing.T) {
	root := t.TempDir()
	namespace := filepath.Join(root, "skills")
	candidate := filepath.Join(namespace, "Caf\u00e9")
	provisional, err := pathauthority.NewProvisional(
		candidate,
		pathtest.DarwinCaseSensitive(candidate).Witness(),
		namespace,
		pathtest.DarwinCaseSensitive(namespace).Witness(),
	)
	if err != nil {
		t.Fatal(err)
	}
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	ancestor, _ := NewActiveClaim(mustAddress(t, root, ""), owner)
	disjoint, _ := NewActiveClaim(mustAddress(t, root+"-other", ""), owner)
	registry, err := NewRegistry([]Claim{disjoint, ancestor})
	if err != nil {
		t.Fatal(err)
	}
	claim, present := registry.ProvisionalAncestorConflict(provisional)
	if !present || !claim.Equal(ancestor) {
		t.Fatalf("provisional conflict = %#v, present=%t; want ancestor", claim, present)
	}
}
