package cache

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
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
		t.Fatal("Locker.Acquire returned nil error for zero key")
	}
}

func TestNilLockFunctionIsRejectedWithoutCreatingEntries(t *testing.T) {
	key := mustKey(t, "git-repo", "nil")
	locker := NewLocker(t.TempDir())
	if err := locker.Do(context.Background(), key, nil); err == nil {
		t.Fatal("Locker.Do returned nil error for nil function")
	}
}

func TestNilLockContextsAreRejected(t *testing.T) {
	key := mustKey(t, "git-repo", "nil-context")
	locker := NewLocker(t.TempDir())
	if _, err := locker.Acquire(nil, key); err == nil {
		t.Fatal("Locker.Acquire returned nil error for nil context")
	}
	if err := locker.Do(nil, key, func() error {
		t.Fatal("lock function should not run for nil context")
		return nil
	}); err == nil {
		t.Fatal("Locker.Do returned nil error for nil context")
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
		done <- secondLocker.Do(ctx, key, func() error { return nil })
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
	if err := locker.Do(context.Background(), key, func() error { return nil }); err != nil {
		t.Fatalf("Do success returned error: %v", err)
	}
	wantErr := errors.New("work failed")
	if err := locker.Do(context.Background(), key, func() error { return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("Do error = %v, want %v", err, wantErr)
	}
	if err := locker.Do(context.Background(), key, func() error { return nil }); err != nil {
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
	if err := locker.Do(context.Background(), key, func() error { return nil }); err != nil {
		t.Fatalf("Do after idempotent release returned error: %v", err)
	}
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
	ctx.once.Do(func() { ctx.joined <- struct{}{} })
	return ctx.Context.Done()
}
