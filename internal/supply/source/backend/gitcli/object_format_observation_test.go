package gitcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestCleanupPublishedObservationProbeDoesNotRecapture(t *testing.T) {
	cacheRoot, destination, path := publishedObservationDestination(t)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	canary := filepath.Join(path, "canary")
	if err := os.WriteFile(canary, []byte("keep\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	outcome, err := mutationfs.NewCommitOutcome(mutationfs.CommitOutcomeIndeterminate, nil)
	if err != nil {
		t.Fatalf("NewCommitOutcome returned error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = cleanupPublishedObservationProbe(ctx, cacheRoot, destination, outcome, storagecommit.EntryIdentity{})
	if err == nil || !strings.Contains(err.Error(), "retained residue") {
		t.Fatalf("cleanup error = %v, want retained residue", err)
	}
	content, readErr := os.ReadFile(canary)
	if readErr != nil {
		t.Fatalf("foreign observation path was removed: %v", readErr)
	}
	if string(content) != "keep\n" {
		t.Fatalf("canary = %q, want preserved", content)
	}
}

func TestCleanupPublishedObservationProbeUsesTransferredIdentityWithoutCancel(t *testing.T) {
	cacheRoot, destination, path := publishedObservationDestination(t)
	identity, err := createObservationProbe(context.Background(), cacheRoot, destination, nil)
	if err != nil {
		t.Fatalf("createObservationProbe returned error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome, err := mutationfs.NewCommitOutcome(mutationfs.CommitOutcomeComplete, nil)
	if err != nil {
		t.Fatalf("NewCommitOutcome returned error: %v", err)
	}
	if err := cleanupPublishedObservationProbe(ctx, cacheRoot, destination, outcome, identity); err != nil {
		t.Fatalf("cleanupPublishedObservationProbe returned error: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("published probe = %v, want removed", err)
	}
}

func publishedObservationDestination(t *testing.T) (
	*rootedpath.CapturedRoot,
	rootedpath.Destination,
	string,
) {
	t.Helper()
	resolver, err := NewResolver(t.TempDir())
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	cacheRoot, err := resolver.captureCacheRoot(context.Background())
	if err != nil {
		t.Fatalf("captureCacheRoot returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := cacheRoot.Close(); err != nil {
			t.Errorf("close cache root: %v", err)
		}
	})
	destination, err := newObservationProbeDestination(cacheRoot)
	if err != nil {
		t.Fatalf("newObservationProbeDestination returned error: %v", err)
	}
	path, err := destination.LexicalPath()
	if err != nil {
		t.Fatalf("LexicalPath returned error: %v", err)
	}
	return cacheRoot, destination, path
}
