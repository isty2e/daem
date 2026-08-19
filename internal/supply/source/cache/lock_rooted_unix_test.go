//go:build darwin || linux

package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

const rootedLockTestWatchdog = 5 * time.Second

func TestRootedLockerWaiterCancellationReportsPathAfterWaitEntered(t *testing.T) {
	cacheRoot := physicalTestRoot(t, t.TempDir())
	firstRoot := mustCaptureRootedLockRoot(t, cacheRoot)
	defer firstRoot.Close()
	secondRoot := mustCaptureRootedLockRoot(t, cacheRoot)
	defer secondRoot.Close()
	lockRoot := filepath.Join(cacheRoot, "locks", "git-repo")
	locker := NewLocker(lockRoot)
	key := mustKey(t, "git-repo", "wait-entered")

	owner, err := locker.acquireRooted(t.Context(), firstRoot, key)
	if err != nil {
		t.Fatalf("owner acquireRooted returned error: %v", err)
	}
	defer func() {
		if err := owner.Release(); err != nil {
			t.Errorf("owner Release returned error: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waitEntered := make(chan struct{})
	waiter := make(chan rootedLockWaiterResult, 1)
	locker = locker.WithAfterWaitBlocked(func() {
		close(waitEntered)
		cancel()
	})
	go func() {
		lock, err := locker.acquireRooted(ctx, secondRoot, key)
		waiter <- rootedLockWaiterResult{lock: lock, err: err}
	}()

	result := awaitRootedLockWaiterAfterWaitEntered(
		t,
		cancel,
		owner,
		waitEntered,
		waiter,
		"timed out waiting for waiter cancellation",
		"timed out waiting for rooted wait-entered",
	)
	defer releaseRootedLockWaiterResult(t, result)
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("waiter acquireRooted error = %v, want context.Canceled", result.err)
	}
	if !strings.Contains(result.err.Error(), "wait for rooted cache lock") ||
		!strings.Contains(result.err.Error(), key.PathComponent()) ||
		!strings.Contains(result.err.Error(), lockRoot) {
		t.Fatalf("waiter error = %q, want rooted wait diagnostic", result.err)
	}
}

func TestRootedLockerWaitCallbackAbortClosesRecordBeforeHandoff(t *testing.T) {
	t.Run("panic", func(t *testing.T) {
		testRootedLockerWaitCallbackAbortClosesRecord(t, func() {
			panic("rooted wait-blocked test panic")
		})
	})
	t.Run("goexit", func(t *testing.T) {
		testRootedLockerWaitCallbackAbortClosesRecord(t, runtime.Goexit)
	})
}

func testRootedLockerWaitCallbackAbortClosesRecord(t *testing.T, abort func()) {
	t.Helper()
	cacheRoot := physicalTestRoot(t, t.TempDir())
	firstRoot := mustCaptureRootedLockRoot(t, cacheRoot)
	defer firstRoot.Close()
	secondRoot := mustCaptureRootedLockRoot(t, cacheRoot)
	defer secondRoot.Close()
	lockRoot := filepath.Join(cacheRoot, "locks", "git-repo")
	locker := NewLocker(lockRoot)
	key := mustKey(t, "git-repo", "abort-handoff")
	lockRecord := filepath.Join(lockRoot, key.PathComponent()+".lock")

	owner, err := locker.acquireRooted(t.Context(), firstRoot, key)
	if err != nil {
		t.Fatalf("owner acquireRooted returned error: %v", err)
	}
	defer func() {
		if err := owner.Release(); err != nil {
			t.Errorf("owner Release returned error: %v", err)
		}
	}()
	ownerDescriptors := countOpenDescriptorsFor(t, lockRecord)
	if ownerDescriptors == 0 {
		t.Fatal("owner holds no descriptors on the lock record")
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waitEntered := make(chan struct{})
	waiter := make(chan rootedLockWaiterResult, 1)
	locker = locker.WithAfterWaitBlocked(func() {
		close(waitEntered)
		abort()
	})
	go func() {
		var result rootedLockWaiterResult
		defer func() {
			_ = recover()
			waiter <- result
		}()
		result.lock, result.err = locker.acquireRooted(ctx, secondRoot, key)
	}()

	result := awaitRootedLockWaiterAfterWaitEntered(
		t,
		cancel,
		owner,
		waitEntered,
		waiter,
		"timed out waiting for aborted waiter",
		"timed out waiting for aborted waiter wait-entered",
	)
	defer releaseRootedLockWaiterResult(t, result)
	if result.lock != nil {
		t.Fatal("aborted waiter returned a lock")
	}
	if got := countOpenDescriptorsFor(t, lockRecord); got != ownerDescriptors {
		t.Fatalf(
			"open descriptors for %q = %d after aborted waiter, want %d owner descriptors",
			lockRecord,
			got,
			ownerDescriptors,
		)
	}
	if err := owner.Release(); err != nil {
		t.Fatalf("owner Release returned error: %v", err)
	}
	if got := countOpenDescriptorsFor(t, lockRecord); got != 0 {
		t.Fatalf("open descriptors for %q = %d after owner Release, want 0", lockRecord, got)
	}
}

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
	if err == nil ||
		!errors.Is(err, ErrRootedLockAuthority) ||
		!strings.Contains(err.Error(), "binding changed") {
		t.Fatalf("rooted lock run error = %v, want classified rebound rejection", err)
	}
	if called {
		t.Fatal("protected operation ran after rooted lock rebinding")
	}
}

func TestRootedLockerClassifiesAuthorityEstablishmentFailures(t *testing.T) {
	rootPath := t.TempDir()
	root := mustCaptureCacheRoot(t, rootPath)
	defer root.Close()
	key := mustKey(t, "rooted-authority", "outside")
	locker := NewLocker(filepath.Join(t.TempDir(), "locks"))
	called := false

	err := locker.DoRooted(t.Context(), root, key, func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrRootedLockAuthority) {
		t.Fatalf("DoRooted error = %v, want rooted lock authority failure", err)
	}
	if called {
		t.Fatal("DoRooted ran the operation without a confined lock namespace")
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

type rootedLockWaiterResult struct {
	lock *Lock
	err  error
}

func awaitRootedLockWaiterResult(
	waiter <-chan rootedLockWaiterResult,
	timeout time.Duration,
) (rootedLockWaiterResult, bool) {
	select {
	case result := <-waiter:
		return result, true
	case <-time.After(timeout):
		return rootedLockWaiterResult{}, false
	}
}

func releaseRootedLockWaiterResult(t *testing.T, result rootedLockWaiterResult) {
	t.Helper()
	if result.lock == nil {
		return
	}
	if err := result.lock.Release(); err != nil {
		t.Errorf("waiter Release returned error: %v", err)
	}
}

func failRootedLockWaiterWatchdog(
	t *testing.T,
	cancel context.CancelFunc,
	owner *Lock,
	waiter <-chan rootedLockWaiterResult,
	message string,
) {
	t.Helper()
	if cancel != nil {
		cancel()
	}
	if result, ok := awaitRootedLockWaiterResult(waiter, rootedLockTestWatchdog); ok {
		releaseRootedLockWaiterResult(t, result)
		t.Fatalf("%s (waiter returned after cancel: %v)", message, result.err)
	}
	if owner != nil {
		_ = owner.Release()
	}
	if result, ok := awaitRootedLockWaiterResult(waiter, rootedLockTestWatchdog); ok {
		releaseRootedLockWaiterResult(t, result)
		t.Fatalf("%s (waiter returned after owner release: %v)", message, result.err)
	}
	t.Fatalf("%s (waiter still running)", message)
}

func awaitRootedLockWaiterAfterWaitEntered(
	t *testing.T,
	cancel context.CancelFunc,
	owner *Lock,
	waitEntered <-chan struct{},
	waiter <-chan rootedLockWaiterResult,
	waitMessage string,
	enteredMessage string,
) rootedLockWaiterResult {
	t.Helper()
	select {
	case <-waitEntered:
		result, ok := awaitRootedLockWaiterResult(waiter, rootedLockTestWatchdog)
		if !ok {
			failRootedLockWaiterWatchdog(t, cancel, owner, waiter, waitMessage)
		}
		return result
	case result := <-waiter:
		select {
		case <-waitEntered:
		default:
			releaseRootedLockWaiterResult(t, result)
			t.Fatalf("waiter returned before wait-entered: %v", result.err)
		}
		return result
	case <-time.After(rootedLockTestWatchdog):
		failRootedLockWaiterWatchdog(t, cancel, owner, waiter, enteredMessage)
	}
	return rootedLockWaiterResult{}
}

func countOpenDescriptorsFor(t *testing.T, path string) int {
	t.Helper()
	var want unix.Stat_t
	if err := unix.Stat(path, &want); err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	count := 0
	for fd := 0; fd < 4096; fd++ {
		var got unix.Stat_t
		if err := unix.Fstat(fd, &got); err != nil {
			continue
		}
		if uint64(got.Dev) == uint64(want.Dev) && uint64(got.Ino) == uint64(want.Ino) {
			count++
		}
	}
	return count
}
