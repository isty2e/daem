//go:build darwin || linux

package mutation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit/fsclock"
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
	budget, err := newRevisionCaptureBudget(defaultRevisionCaptureLimits())
	if err != nil {
		t.Fatal(err)
	}
	first, err := captureRevision(context.Background(), referents[0], cache, budget)
	if err != nil {
		t.Fatal(err)
	}
	fsclock.WaitForTick(t, path)
	if err := os.WriteFile(path, []byte("after!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	second, err := captureRevision(context.Background(), referents[1], cache, budget)
	if err != nil {
		t.Fatal(err)
	}
	if first.Equal(second) {
		t.Fatal("aliased rewrite reused a stale bounded-file digest")
	}
}

func TestIncrementalRevisionCaptureResolvesRetargetedAncestor(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, directory := range []string{first, second} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "value"), []byte("same"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	request, err := NewBoundedFileRevisionRequest(64, filepath.Join(alias, "value"), PathEffectReferent)
	if err != nil {
		t.Fatal(err)
	}
	pass := NewRevisionObservationPass()
	before, err := pass.Capture(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	after, err := pass.Capture(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := NewRevisionObservationPass().Capture(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if before.Equal(after) || !after.Equal(fresh) {
		t.Fatal("incremental capture retained an earlier ancestor resolution")
	}
}

func TestRevisionBatchKeepsAliasEntryAndReferentDistinct(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "value"), []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	ancestorAlias := filepath.Join(root, "ancestor-alias")
	if err := os.Symlink(real, ancestorAlias); err != nil {
		t.Fatal(err)
	}
	finalAlias := filepath.Join(root, "final-alias")
	if err := os.Symlink(filepath.Join(real, "value"), finalAlias); err != nil {
		t.Fatal(err)
	}
	requests, err := BoundedFileRevisionRequests(64, filepath.Join(ancestorAlias, "value"), finalAlias)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := CaptureRevisionSet(t.Context(), requests...)
	if err != nil {
		t.Fatal(err)
	}
	for index, request := range requests {
		fresh, err := NewRevisionObservationPass().Capture(t.Context(), request)
		if err != nil || !batch.observations[index].revision.Equal(fresh) {
			t.Fatalf("aliased batch request %d differs from fresh capture: %v", index, err)
		}
	}
	if batch.observations[2].revision.Equal(batch.observations[3].revision) {
		t.Fatal("final alias entry and referent revisions were conflated")
	}
}
