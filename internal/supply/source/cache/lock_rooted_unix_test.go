//go:build darwin || linux

package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func TestRootedLockerSerializesOneRetainedCacheRoot(t *testing.T) {
	cacheRoot := physicalTestRoot(t, t.TempDir())
	firstRoot := mustCaptureRootedLockRoot(t, cacheRoot)
	defer firstRoot.Close()
	secondRoot := mustCaptureRootedLockRoot(t, cacheRoot)
	defer secondRoot.Close()
	locker := NewLocker(filepath.Join(cacheRoot, "locks", "git-artifact"))
	key := mustKey(t, "rooted-lock", "same-key")

	first, err := locker.acquireRooted(t.Context(), firstRoot, key)
	if err != nil {
		t.Fatalf("first AcquireRooted returned error: %v", err)
	}
	defer first.Release()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := locker.acquireRooted(ctx, secondRoot, key); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second AcquireRooted error = %v, want deadline", err)
	}
}

func TestRootedLockerRejectsSymlinkedAncestorWithoutExternalRecord(t *testing.T) {
	cacheRoot := physicalTestRoot(t, t.TempDir())
	root := mustCaptureRootedLockRoot(t, cacheRoot)
	defer root.Close()
	external := physicalTestRoot(t, t.TempDir())
	if err := os.Symlink(external, filepath.Join(cacheRoot, "locks")); err != nil {
		t.Fatalf("create lock-ancestor symlink: %v", err)
	}
	locker := NewLocker(filepath.Join(cacheRoot, "locks", "git-repo"))

	_, err := locker.acquireRooted(t.Context(), root, mustKey(t, "rooted-lock", "redirected"))
	if err == nil || !strings.Contains(err.Error(), "non-symlink directory") {
		t.Fatalf("AcquireRooted error = %v, want symlink rejection", err)
	}
	entries, err := os.ReadDir(external)
	if err != nil {
		t.Fatalf("read external lock directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("external lock entries = %v, want none", entries)
	}
}

func TestRootedLockerRejectsSymlinkedRecordWithoutExternalMutation(t *testing.T) {
	cacheRoot := physicalTestRoot(t, t.TempDir())
	root := mustCaptureRootedLockRoot(t, cacheRoot)
	defer root.Close()
	lockRoot := filepath.Join(cacheRoot, "locks", "git-repo")
	if err := os.MkdirAll(lockRoot, 0o700); err != nil {
		t.Fatalf("create rooted lock directory: %v", err)
	}
	externalRecord := filepath.Join(physicalTestRoot(t, t.TempDir()), "external-lock")
	if err := os.WriteFile(externalRecord, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write external lock record: %v", err)
	}
	key := mustKey(t, "rooted-lock", "symlinked-record")
	if err := os.Symlink(
		externalRecord,
		filepath.Join(lockRoot, key.PathComponent()+".lock"),
	); err != nil {
		t.Fatalf("create lock-record symlink: %v", err)
	}
	locker := NewLocker(lockRoot)

	_, err := locker.acquireRooted(t.Context(), root, key)
	if err == nil || !strings.Contains(err.Error(), "open rooted cache lock record") {
		t.Fatalf("AcquireRooted error = %v, want lock-record symlink rejection", err)
	}
	content, err := os.ReadFile(externalRecord)
	if err != nil {
		t.Fatalf("read external lock record: %v", err)
	}
	if string(content) != "keep" {
		t.Fatalf("external lock record content = %q, want keep", content)
	}
}

func TestRootedLockerRejectsReboundLockBeforeOperation(t *testing.T) {
	cacheRoot := physicalTestRoot(t, t.TempDir())
	root := mustCaptureRootedLockRoot(t, cacheRoot)
	defer root.Close()
	external := physicalTestRoot(t, t.TempDir())
	locker := NewLocker(filepath.Join(cacheRoot, "locks", "git-repo"))
	lock, err := locker.acquireRooted(
		t.Context(),
		root,
		mustKey(t, "rooted-lock", "rebound"),
	)
	if err != nil {
		t.Fatalf("AcquireRooted returned error: %v", err)
	}

	lockRoot := filepath.Join(cacheRoot, "locks")
	movedRoot := lockRoot + ".moved"
	if err := os.Rename(lockRoot, movedRoot); err != nil {
		t.Fatalf("move rooted lock tree: %v", err)
	}
	if err := os.Symlink(external, lockRoot); err != nil {
		t.Fatalf("replace rooted lock tree: %v", err)
	}
	called := false
	err = lock.run(func() error {
		called = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "binding changed") {
		t.Fatalf("rooted lock run error = %v, want rebound rejection", err)
	}
	if called {
		t.Fatal("protected operation ran after rooted lock rebinding")
	}
}

func physicalTestRoot(t *testing.T, root string) string {
	t.Helper()
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve physical test root: %v", err)
	}
	return physical
}

func mustCaptureRootedLockRoot(t *testing.T, root string) *rootedpath.CapturedRoot {
	t.Helper()
	captured, err := rootedpath.CaptureRootNoFollow(root)
	if err != nil {
		t.Fatalf("CaptureRootNoFollow returned error: %v", err)
	}
	return captured
}
