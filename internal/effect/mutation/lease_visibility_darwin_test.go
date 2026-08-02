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
