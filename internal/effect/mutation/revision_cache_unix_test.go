//go:build darwin || linux

package mutation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBoundedRevisionCacheRejectsAliasedRewriteWithRestoredMtime(t *testing.T) {
	realRoot := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(realRoot, "declaration")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	aliases := make([]string, 2)
	for index := range aliases {
		aliasRoot := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(realRoot, aliasRoot); err != nil {
			t.Fatal(err)
		}
		aliases[index] = filepath.Join(aliasRoot, "declaration")
	}
	requests, err := BoundedFileRevisionRequests(64, aliases...)
	if err != nil {
		t.Fatal(err)
	}
	referents := make([]RevisionRequest, 0, len(aliases))
	for _, request := range requests {
		if request.Effect == PathEffectReferent {
			referents = append(referents, request)
		}
	}
	if len(referents) != len(aliases) {
		t.Fatalf("referent request count = %d, want %d", len(referents), len(aliases))
	}

	cache := make(map[string]revisionFileCacheEntry)
	first, err := captureRevision(context.Background(), referents[0], cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	second, err := captureRevision(context.Background(), referents[1], cache)
	if err != nil {
		t.Fatal(err)
	}
	if first.Equal(second) {
		t.Fatal("aliased rewrite reused a stale bounded-file digest")
	}
}
