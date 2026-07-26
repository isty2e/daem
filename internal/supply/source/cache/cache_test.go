package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func TestKeyPathComponentIsDeterministicAndSafe(t *testing.T) {
	first, err := NewKey("git-repo", "https://example.com/repo.git", "main")
	if err != nil {
		t.Fatalf("NewKey returned error: %v", err)
	}
	second, err := NewKey("git-repo", "https://example.com/repo.git", "main")
	if err != nil {
		t.Fatalf("NewKey returned error: %v", err)
	}
	different, err := NewKey("git-repo", "https://example.com/repo.git", "other")
	if err != nil {
		t.Fatalf("NewKey returned error: %v", err)
	}

	if first.PathComponent() != second.PathComponent() {
		t.Fatalf("same key materials produced %q and %q", first.PathComponent(), second.PathComponent())
	}
	if first.PathComponent() == different.PathComponent() {
		t.Fatalf("different key materials produced identical path component %q", first.PathComponent())
	}
	if !strings.HasPrefix(first.PathComponent(), "git-repo-") {
		t.Fatalf("PathComponent = %q, want namespace prefix", first.PathComponent())
	}
	for _, forbidden := range []string{"/", "\\", ":", " "} {
		if strings.Contains(first.PathComponent(), forbidden) {
			t.Fatalf("PathComponent = %q contains forbidden %q", first.PathComponent(), forbidden)
		}
	}
}

func TestNewKeyRejectsUnsafeNamespace(t *testing.T) {
	for _, namespace := range []string{"", " git", "git repo", "git/repo", "git\\repo"} {
		if _, err := NewKey(namespace, "value"); err == nil {
			t.Fatalf("NewKey(%q) returned nil error", namespace)
		}
	}
}

func TestKeyHashUsesFieldBoundaries(t *testing.T) {
	left, err := NewKey("git-repo", "ab", "c")
	if err != nil {
		t.Fatalf("NewKey left returned error: %v", err)
	}
	right, err := NewKey("git-repo", "a", "bc")
	if err != nil {
		t.Fatalf("NewKey right returned error: %v", err)
	}
	if left.PathComponent() == right.PathComponent() {
		t.Fatalf("field boundary ambiguity produced identical key %q", left.PathComponent())
	}
}

func TestZeroKeyRejectedByPublicOperations(t *testing.T) {
	locker := NewLocker(t.TempDir())
	if _, err := locker.Acquire(context.Background(), Key{}); err == nil {
		t.Fatalf("Locker.Acquire returned nil error for zero key")
	}
}

func TestNilFunctionsAreRejected(t *testing.T) {
	key := mustKey(t, "git-repo", "nil")
	lockRoot := t.TempDir()
	locker := NewLocker(lockRoot)
	if err := locker.Do(context.Background(), key, nil); err == nil {
		t.Fatalf("Locker.Do returned nil error for nil function")
	}
	if entries, err := os.ReadDir(lockRoot); err != nil {
		t.Fatalf("read lock root: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("nil Locker.Do created lock entries: %v", entries)
	}

	spec := testEntrySpec(t, "nil-build")
	if _, err := PublishDirectoryOnce(context.Background(), filepath.Join(t.TempDir(), "artifact"), spec, nil); err == nil {
		t.Fatalf("PublishDirectoryOnce returned nil error for nil build")
	}
}

func TestNilContextsAreRejected(t *testing.T) {
	key := mustKey(t, "git-repo", "nil-context")

	locker := NewLocker(t.TempDir())
	if _, err := locker.Acquire(nil, key); err == nil {
		t.Fatalf("Locker.Acquire returned nil error for nil context")
	}
	if err := locker.Do(nil, key, func() error {
		t.Fatal("lock function should not run for nil context")
		return nil
	}); err == nil {
		t.Fatalf("Locker.Do returned nil error for nil context")
	}

	spec := testEntrySpec(t, "nil-context")
	if _, err := PublishDirectoryOnce(nil, filepath.Join(t.TempDir(), "artifact"), spec, func(string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		t.Fatal("build should not run for nil context")
		return "", "", nil
	}); err == nil {
		t.Fatalf("PublishDirectoryOnce returned nil error for nil context")
	}
}

func TestLockerSerializesSameKeyAcrossInstances(t *testing.T) {
	key := mustKey(t, "git-repo", "shared")
	lockRoot := t.TempDir()
	firstLocker := NewLocker(lockRoot)
	secondLocker := NewLocker(lockRoot)

	lock, err := firstLocker.Acquire(context.Background(), key)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}

	started := make(chan struct{})
	joined := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		close(started)
		ctx := &cacheWaiterJoinContext{Context: context.Background(), joined: joined}
		done <- secondLocker.Do(ctx, key, func() error {
			return nil
		})
	}()
	<-started
	waitForSignals(t, joined, 1)

	select {
	case err := <-done:
		t.Fatalf("second locker completed before release: %v", err)
	default:
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("second locker returned error: %v", err)
	}
}

func TestLockerWaiterCancellationReportsKeyAndPath(t *testing.T) {
	key := mustKey(t, "git-repo", "cancel")
	lockRoot := t.TempDir()
	firstLocker := NewLocker(lockRoot)
	secondLocker := NewLocker(lockRoot)

	lock, err := firstLocker.Acquire(context.Background(), key)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	defer lock.Release()

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err = secondLocker.Acquire(ctx, key)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire error = %v, want context deadline", err)
	}
	if !strings.Contains(err.Error(), key.PathComponent()) || !strings.Contains(err.Error(), lockRoot) {
		t.Fatalf("Acquire error = %q, want key and lock root context", err)
	}
}

func TestLockerReleasesAfterSuccessAndError(t *testing.T) {
	key := mustKey(t, "git-repo", "release")
	locker := NewLocker(t.TempDir())

	if err := locker.Do(context.Background(), key, func() error {
		return nil
	}); err != nil {
		t.Fatalf("Do success returned error: %v", err)
	}

	wantErr := errors.New("work failed")
	if err := locker.Do(context.Background(), key, func() error {
		return wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("Do error = %v, want %v", err, wantErr)
	}

	if err := locker.Do(context.Background(), key, func() error {
		return nil
	}); err != nil {
		t.Fatalf("Do after error returned error: %v", err)
	}
}

func TestLockReleaseIsIdempotent(t *testing.T) {
	key := mustKey(t, "git-repo", "idempotent")
	locker := NewLocker(t.TempDir())
	lock, err := locker.Acquire(context.Background(), key)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("first Release returned error: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("second Release returned error: %v", err)
	}
	if err := locker.Do(context.Background(), key, func() error {
		return nil
	}); err != nil {
		t.Fatalf("Do after idempotent release returned error: %v", err)
	}
}

func TestPublishDirectoryOnceCompleteRootFastPath(t *testing.T) {
	finalRoot := filepath.Join(t.TempDir(), "artifact")
	spec := testEntrySpec(t, "fast-path")
	writeCompleteEntry(t, finalRoot, spec, "old")

	called := false
	published, err := PublishDirectoryOnce(context.Background(), finalRoot, spec, func(string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		called = true
		return "", "", nil
	})
	if err != nil {
		t.Fatalf("PublishDirectoryOnce returned error: %v", err)
	}
	if published {
		t.Fatalf("published = true, want false for complete root fast path")
	}
	if called {
		t.Fatalf("build was called for complete root")
	}
	assertFileContent(t, filepath.Join(finalRoot, "content.txt"), "old")
}

func TestPublishDirectoryOncePublishesCompletionRecordAfterBuildSuccess(t *testing.T) {
	finalRoot := filepath.Join(t.TempDir(), "artifact")
	spec := testEntrySpec(t, "publish")

	published, err := PublishDirectoryOnce(context.Background(), finalRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		if _, err := os.Lstat(filepath.Join(tempRoot, completionRecordName)); !os.IsNotExist(err) {
			t.Fatalf("completion record exists before build returned: %v", err)
		}
		return writeTestContent(tempRoot, "new")
	})
	if err != nil {
		t.Fatalf("PublishDirectoryOnce returned error: %v", err)
	}
	if !published {
		t.Fatalf("published = false, want true after successful publish")
	}
	if valid, err := VerifyDirectory(context.Background(), finalRoot, spec); err != nil || !valid {
		t.Fatalf("VerifyDirectory = %v, %v, want valid", valid, err)
	}
	assertFileContent(t, filepath.Join(finalRoot, "content.txt"), "new")
}

func TestPublishDirectoryOnceCreatesNestedParent(t *testing.T) {
	finalRoot := filepath.Join(t.TempDir(), "deep", "nested", "artifact")
	spec := testEntrySpec(t, "nested")

	published, err := PublishDirectoryOnce(context.Background(), finalRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		return writeTestContent(tempRoot, "nested")
	})
	if err != nil {
		t.Fatalf("PublishDirectoryOnce returned error: %v", err)
	}
	if !published {
		t.Fatalf("published = false, want true for nested publish")
	}
	assertFileContent(t, filepath.Join(finalRoot, "content.txt"), "nested")
}

func TestPublishDirectoryOnceRebuildsDirectoryCompletionRecord(t *testing.T) {
	finalRoot := filepath.Join(t.TempDir(), "artifact")
	spec := testEntrySpec(t, "directory-record")
	if err := os.MkdirAll(filepath.Join(finalRoot, completionRecordName), 0o700); err != nil {
		t.Fatalf("create completion-record directory: %v", err)
	}

	published, err := PublishDirectoryOnce(context.Background(), finalRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		return writeTestContent(tempRoot, "rebuilt")
	})
	if err != nil || !published {
		t.Fatalf("PublishDirectoryOnce = %v, %v, want rebuilt publication", published, err)
	}
	assertFileContent(t, filepath.Join(finalRoot, "content.txt"), "rebuilt")
}

func TestPublishDirectoryOnceRebuildsSymlinkCompletionRecord(t *testing.T) {
	root := t.TempDir()
	finalRoot := filepath.Join(root, "artifact")
	spec := testEntrySpec(t, "symlink-record")
	if err := os.MkdirAll(finalRoot, 0o700); err != nil {
		t.Fatalf("create final root: %v", err)
	}
	target := filepath.Join(root, "completion-record-target")
	if err := os.WriteFile(target, []byte("complete\n"), 0o600); err != nil {
		t.Fatalf("write completion-record target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(finalRoot, completionRecordName)); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	published, err := PublishDirectoryOnce(context.Background(), finalRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		return writeTestContent(tempRoot, "rebuilt")
	})
	if err != nil || !published {
		t.Fatalf("PublishDirectoryOnce = %v, %v, want rebuilt publication", published, err)
	}
	assertFileContent(t, filepath.Join(finalRoot, "content.txt"), "rebuilt")
}

func TestPublishDirectoryOnceCleansTempAfterBuildError(t *testing.T) {
	parent := t.TempDir()
	finalRoot := filepath.Join(parent, "artifact")
	spec := testEntrySpec(t, "build-error")
	wantErr := errors.New("build failed")

	_, err := PublishDirectoryOnce(context.Background(), finalRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		if err := os.WriteFile(filepath.Join(tempRoot, "content.txt"), []byte("partial"), 0o600); err != nil {
			t.Fatalf("write temp content: %v", err)
		}
		return "", "", wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("PublishDirectoryOnce error = %v, want build error", err)
	}
	assertNoTempDirs(t, parent)
	if _, err := os.Lstat(finalRoot); !os.IsNotExist(err) {
		t.Fatalf("final root stat error = %v, want not exist", err)
	}
}

func TestPublishDirectoryOnceCleansTempAfterContextCancellation(t *testing.T) {
	parent := t.TempDir()
	finalRoot := filepath.Join(parent, "artifact")
	spec := testEntrySpec(t, "cancel")
	ctx, cancel := context.WithCancel(context.Background())

	_, err := PublishDirectoryOnce(ctx, finalRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		hash, kind, err := writeTestContent(tempRoot, "partial")
		cancel()
		return hash, kind, err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PublishDirectoryOnce error = %v, want context.Canceled", err)
	}
	assertNoTempDirs(t, parent)
	if _, err := os.Lstat(finalRoot); !os.IsNotExist(err) {
		t.Fatalf("final root stat error = %v, want not exist", err)
	}
}

func TestPublishDirectoryOnceLostRaceAgainstCompleteRoot(t *testing.T) {
	parent := t.TempDir()
	finalRoot := filepath.Join(parent, "artifact")
	spec := testEntrySpec(t, "lost-race")

	published, err := PublishDirectoryOnce(context.Background(), finalRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		hash, kind, err := writeTestContent(tempRoot, "loser")
		if err != nil {
			return "", "", err
		}
		writeCompleteEntry(t, finalRoot, spec, "winner")
		return hash, kind, nil
	})
	if err != nil {
		t.Fatalf("PublishDirectoryOnce returned error: %v", err)
	}
	if published {
		t.Fatalf("published = true, want false after losing publish race")
	}
	assertNoTempDirs(t, parent)
	assertFileContent(t, filepath.Join(finalRoot, "content.txt"), "winner")
}

func TestPublishDirectoryOnceRetiresIncompleteFinalRootBeforeRebuild(t *testing.T) {
	finalRoot := filepath.Join(t.TempDir(), "artifact")
	spec := testEntrySpec(t, "incomplete")
	if err := os.MkdirAll(finalRoot, 0o700); err != nil {
		t.Fatalf("create incomplete final root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(finalRoot, "partial.txt"), []byte("partial"), 0o600); err != nil {
		t.Fatalf("write partial file: %v", err)
	}

	called := false
	published, err := PublishDirectoryOnce(context.Background(), finalRoot, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		called = true
		return writeTestContent(tempRoot, "rebuilt")
	})
	if err != nil || !published {
		t.Fatalf("PublishDirectoryOnce = %v, %v, want rebuilt publication", published, err)
	}
	if !called {
		t.Fatalf("build was not called for incomplete final root")
	}
	if _, err := os.Lstat(filepath.Join(finalRoot, "partial.txt")); !os.IsNotExist(err) {
		t.Fatalf("retired partial content remains: %v", err)
	}
	assertFileContent(t, filepath.Join(finalRoot, "content.txt"), "rebuilt")
}

func mustKey(t *testing.T, namespace string, materials ...string) Key {
	t.Helper()
	key, err := NewKey(namespace, materials...)
	if err != nil {
		t.Fatalf("NewKey returned error: %v", err)
	}
	return key
}

func waitForSignals(t *testing.T, signals <-chan struct{}, count int) {
	t.Helper()
	for range count {
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-signals:
			timer.Stop()
		case <-timer.C:
			t.Fatalf("timed out waiting for signal")
		}
	}
}

type cacheWaiterJoinContext struct {
	context.Context
	joined chan<- struct{}
	once   sync.Once
}

func (ctx *cacheWaiterJoinContext) Done() <-chan struct{} {
	// Flock first calls Done after the caller has entered the wait path.
	ctx.once.Do(func() { ctx.joined <- struct{}{} })
	return ctx.Context.Done()
}

func testEntrySpec(t *testing.T, material string) EntrySpec {
	t.Helper()
	spec, err := NewEntrySpec(mustKey(t, "test-entry", material), "content.txt", "", "")
	if err != nil {
		t.Fatalf("NewEntrySpec returned error: %v", err)
	}
	return spec
}

func writeCompleteEntry(t *testing.T, root string, spec EntrySpec, content string) {
	t.Helper()
	published, err := PublishDirectoryOnce(context.Background(), root, spec, func(tempRoot string) (artifact.ContentHash, artifact.ArtifactKind, error) {
		return writeTestContent(tempRoot, content)
	})
	if err != nil || !published {
		t.Fatalf("PublishDirectoryOnce fixture = %v, %v", published, err)
	}
}

func writeTestContent(root string, content string) (artifact.ContentHash, artifact.ArtifactKind, error) {
	path := filepath.Join(root, "content.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", "", err
	}
	return access.HashPath(context.Background(), path)
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("content %q = %q, want %q", path, content, want)
	}
}

func assertNoTempDirs(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read parent %q: %v", parent, err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatalf("temporary entry %q was not cleaned", entry.Name())
		}
	}
}
