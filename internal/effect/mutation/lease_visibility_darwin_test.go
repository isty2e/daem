//go:build darwin

package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLeaseSetRetainsNamespaceExclusionAcrossExactToProvisionalRemoval(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Caf\u00e9")
	if err := os.WriteFile(path, []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	domain, err := NewLogicalPathDomain(LogicalPathRequest{
		Path: path, Access: AccessExclusive, Effect: PathEffectDirectoryEntry,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := mutationTestStore(t)
	store.interval = 5 * time.Millisecond
	set, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := set.Release(); err != nil {
			t.Errorf("release holder: %v", err)
		}
	})

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if matches, matchErr := set.DomainsMatchCurrent(context.Background()); matchErr != nil || matches {
		t.Fatalf("strict DomainsMatchCurrent() = %t, %v; want false", matches, matchErr)
	}
	if matches, matchErr := set.VisibilityAuthorityMatchesCurrent(context.Background()); matchErr != nil || !matches {
		t.Fatalf("VisibilityAuthorityMatchesCurrent() = %t, %v; want true", matches, matchErr)
	}

	aliasDomain, err := NewLogicalPathDomain(LogicalPathRequest{
		Path:   filepath.Join(root, "Cafe\u0301"),
		Access: AccessExclusive, Effect: PathEffectDirectoryEntry,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.maximum = 100 * time.Millisecond
	if aliasSet, acquireErr := store.Acquire(context.Background(), aliasDomain); acquireErr == nil {
		_ = aliasSet.Release()
		t.Fatal("normalization alias acquired while the original namespace lease remained active")
	} else {
		var contention ContentionError
		if !errors.As(acquireErr, &contention) {
			t.Fatalf("alias acquire error = %v, want ContentionError", acquireErr)
		}
	}

	if accepted, acceptErr := set.AcceptVisibilityChanges(context.Background()); acceptErr != nil || !accepted {
		t.Fatalf("AcceptVisibilityChanges() = %t, %v; want true", accepted, acceptErr)
	}
	if matches, matchErr := set.DomainsMatchCurrent(context.Background()); matchErr != nil || !matches {
		t.Fatalf("rebound DomainsMatchCurrent() = %t, %v; want true", matches, matchErr)
	}
}

func TestPhysicalPathPairRetainsAuthorityAfterNonASCIIFileRemoval(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Café.toml")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := PhysicalAuthorityRequest{Path: path, Target: "codex", Scope: "project"}
	authority, err := NewPhysicalAuthoritySet(request)
	if err != nil {
		t.Fatal(err)
	}
	store := mutationTestStore(t)
	set, err := store.Acquire(t.Context(), authority.domains...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Release() })

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if matches, err := set.MatchCurrentPhysicalRequests(t.Context(), request); err != nil || matches {
		t.Fatalf("strict validation must detect the change before acceptance: %t %v", matches, err)
	}
	if matches, err := set.VisibilityAuthorityMatchesCurrent(t.Context()); err != nil || !matches {
		t.Errorf("the held namespace must still cover compensation after removal: %t %v", matches, err)
	}
	if accepted, err := set.AcceptVisibilityChanges(t.Context()); err != nil || !accepted {
		t.Fatalf("same-entry absence was not accepted under the held namespace: %t %v", accepted, err)
	}
	if matches, err := set.MatchCurrentPhysicalRequests(t.Context(), request); err != nil || !matches {
		t.Fatalf("the next operation could not qualify the accepted absence: %t %v", matches, err)
	}

	other := filepath.Join(root, "other.toml")
	if err := os.WriteFile(other, []byte("independent"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(other), path); err != nil {
		t.Fatal(err)
	}
	if accepted, err := set.AcceptVisibilityChanges(t.Context()); err != nil || accepted {
		t.Fatalf("a different referent under the same parent was accepted: %t %v", accepted, err)
	}
	if matches, err := set.VisibilityAuthorityMatchesCurrent(t.Context()); err != nil || matches {
		t.Fatalf("compensation accepted the retargeted route: %t %v", matches, err)
	}
}

func TestUnpairedReferentDoesNotAcquireVisibilityContinuation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Café.toml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	referent, err := NewPhysicalPathDomain(PhysicalPathRequest{
		Path: path, Target: "codex", Scope: "project", Effect: PathEffectReferent, Access: AccessExclusive,
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := mutationTestStore(t).Acquire(t.Context(), referent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Release() })

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if accepted, err := set.AcceptVisibilityChanges(t.Context()); err != nil || accepted {
		t.Fatalf("referent without the entry's namespace lease accepted removal: %t %v", accepted, err)
	}
}
