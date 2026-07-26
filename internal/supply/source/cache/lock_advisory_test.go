package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAdvisoryLockRecordPersistsPrivatelyAfterRelease(t *testing.T) {
	root := t.TempDir()
	key := mustKey(t, "cache-lock", "persistent-record")
	locker := NewLocker(root)

	lock, err := locker.Acquire(context.Background(), key)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}

	recordPath := filepath.Join(root, key.PathComponent()+".lock")
	info, err := os.Lstat(recordPath)
	if err != nil {
		t.Fatalf("inspect persistent lock record: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("lock record mode = %s, want regular 0600", info.Mode())
	}

	next, err := locker.Acquire(context.Background(), key)
	if err != nil {
		t.Fatalf("Acquire with stale persistent record returned error: %v", err)
	}
	if err := next.Release(); err != nil {
		t.Fatalf("release reacquired lock: %v", err)
	}
}

func TestAdvisoryLockerAllowsDifferentKeys(t *testing.T) {
	locker := NewLocker(t.TempDir())
	first, err := locker.Acquire(context.Background(), mustKey(t, "cache-lock", "first"))
	if err != nil {
		t.Fatalf("acquire first key: %v", err)
	}
	defer first.Release()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	second, err := locker.Acquire(ctx, mustKey(t, "cache-lock", "second"))
	if err != nil {
		t.Fatalf("acquire independent key: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("release independent key: %v", err)
	}
}

func TestAdvisoryLockerRejectsUnsafeRecordShapes(t *testing.T) {
	for _, test := range []struct {
		name            string
		skipUnavailable bool
		create          func(string) error
	}{
		{name: "directory", create: func(path string) error { return os.Mkdir(path, 0o700) }},
		{name: "permissive regular file", create: func(path string) error {
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				return err
			}
			return os.Chmod(path, 0o644)
		}},
		{name: "symlink", skipUnavailable: true, create: func(path string) error {
			target := filepath.Join(filepath.Dir(path), "target")
			if err := os.WriteFile(target, nil, 0o600); err != nil {
				return err
			}
			return os.Symlink(target, path)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			key := mustKey(t, "cache-lock", test.name)
			path := filepath.Join(root, key.PathComponent()+".lock")
			if err := test.create(path); err != nil {
				if test.skipUnavailable {
					t.Skipf("create %s lock record: %v", test.name, err)
				}
				t.Fatalf("create %s lock record: %v", test.name, err)
			}

			if _, err := NewLocker(root).Acquire(context.Background(), key); err == nil {
				t.Fatal("Acquire returned nil error for unsafe lock record")
			}
		})
	}
}

func TestLockRunPreservesOperationAndReleaseErrors(t *testing.T) {
	operationErr := errors.New("operation failed")
	releaseErr := errors.New("unlock failed")
	lock := &Lock{path: "fault-lock", file: &faultAdvisoryLock{releaseErr: releaseErr}}
	err := lock.run(func() error {
		return operationErr
	})
	if !errors.Is(err, operationErr) || !errors.Is(err, releaseErr) {
		t.Fatalf("run error = %v, want operation and release errors", err)
	}
}

type faultAdvisoryLock struct {
	releaseErr error
}

func (lock *faultAdvisoryLock) Unlock() error {
	return lock.releaseErr
}
