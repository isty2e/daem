//go:build darwin

package mutation

import (
	"context"
	"os"
	"testing"
)

func TestLeaseSetAcceptsProvisionalToExactVisibilityChange(t *testing.T) {
	namespacePath := t.TempDir()
	namespace := mustMutationTestCanonicalPath(namespacePath)
	domain := provisionalMutationTestDomain(t, namespace, "Caf\u00e9")
	store := mutationTestStore(t)
	set, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()

	if err := os.WriteFile(domain.requestedPath, []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	if matches, err := set.DomainsMatchCurrent(context.Background()); err != nil || matches {
		t.Fatalf("strict DomainsMatchCurrent() = %t, %v; want false", matches, err)
	}
	if matches, err := set.VisibilityAuthorityMatchesCurrent(context.Background()); err != nil || !matches {
		t.Fatalf("VisibilityAuthorityMatchesCurrent() = %t, %v; want true", matches, err)
	}
	if accepted, err := set.AcceptVisibilityChanges(context.Background()); err != nil || !accepted {
		t.Fatalf("AcceptVisibilityChanges() = %t, %v; want true", accepted, err)
	}
	if matches, err := set.DomainsMatchCurrent(context.Background()); err != nil || !matches {
		t.Fatalf("rebound DomainsMatchCurrent() = %t, %v; want true", matches, err)
	}
}
