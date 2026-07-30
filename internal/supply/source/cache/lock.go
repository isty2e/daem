package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

const defaultLockPollInterval = 10 * time.Millisecond

// Locker serializes exact-key cache mutations across processes using persistent
// OS advisory-lock records.
type Locker struct {
	root         string
	pollInterval time.Duration
}

type lockReleaser interface {
	Unlock() error
}

type lockValidator interface {
	Validate() error
}

// NewLocker constructs an advisory cache locker rooted at root.
func NewLocker(root string) Locker {
	cleanRoot := root
	if cleanRoot != "" {
		cleanRoot = filepath.Clean(cleanRoot)
	}
	return Locker{
		root:         cleanRoot,
		pollInterval: defaultLockPollInterval,
	}
}

// Lock represents an acquired cache lock.
type Lock struct {
	path string
	file lockReleaser
	once sync.Once
	err  error
}

// Acquire waits until key's persistent lock record grants OS advisory
// ownership. Record presence alone never represents ownership.
func (locker Locker) Acquire(ctx context.Context, key Key) (*Lock, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	if err := validateContext(ctx, "lock acquisition"); err != nil {
		return nil, err
	}
	if locker.root == "" {
		return nil, fmt.Errorf("cache lock root is required for key %q", key)
	}
	if err := prepareLockRoot(locker.root); err != nil {
		return nil, fmt.Errorf("prepare cache lock root %q for key %q: %w", locker.root, key, err)
	}

	lockPath := filepath.Join(locker.root, key.PathComponent()+".lock")
	if err := prepareLockRecord(lockPath); err != nil {
		return nil, fmt.Errorf("prepare cache lock %q at %q: %w", key, lockPath, err)
	}
	lockFile := flock.New(lockPath, flock.SetPermissions(0o600))
	locked, err := lockFile.TryLockContext(ctx, locker.pollInterval)
	if err != nil || !locked {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, fmt.Errorf("wait for cache lock %q at %q: %w", key, lockPath, contextErr)
		}
		if err == nil {
			err = fmt.Errorf("advisory lock attempt ended without ownership")
		}
		return nil, fmt.Errorf("acquire cache lock %q at %q: %w", key, lockPath, err)
	}
	return &Lock{path: lockPath, file: lockFile}, nil
}

func (locker Locker) acquireRooted(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	key Key,
) (*Lock, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	if err := validateContext(ctx, "rooted lock acquisition"); err != nil {
		return nil, err
	}
	if locker.root == "" {
		return nil, fmt.Errorf("cache lock root is required for key %q", key)
	}
	if root == nil {
		return nil, fmt.Errorf("rooted cache lock authority is required for key %q", key)
	}
	authority, err := root.Authority()
	if err != nil {
		return nil, err
	}
	relativeRoot, err := filepath.Rel(authority.PhysicalRoot(), locker.root)
	if err != nil ||
		filepath.IsAbs(relativeRoot) ||
		relativeRoot == ".." ||
		strings.HasPrefix(relativeRoot, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf(
			"cache lock root %q is outside retained cache root %q",
			locker.root,
			authority.PhysicalRoot(),
		)
	}
	lockRelative := key.PathComponent() + ".lock"
	if relativeRoot != "." {
		lockRelative = filepath.Join(relativeRoot, lockRelative)
	}
	relative, err := rootedpath.NewRelativeDestination(filepath.ToSlash(lockRelative))
	if err != nil {
		return nil, fmt.Errorf("bind rooted cache lock %q: %w", key, err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		return nil, err
	}
	capability, err := root.Acquire(destination)
	if err != nil {
		return nil, err
	}
	lockPath, err := destination.LexicalPath()
	if err != nil {
		_ = capability.Close()
		return nil, err
	}
	lockFile, err := acquireRootedAdvisoryLock(ctx, capability, locker.pollInterval)
	if err != nil {
		return nil, fmt.Errorf("acquire rooted cache lock %q at %q: %w", key, lockPath, err)
	}
	return &Lock{path: lockPath, file: lockFile}, nil
}

// Do runs fn while holding key's advisory lock.
func (locker Locker) Do(ctx context.Context, key Key, fn func() error) error {
	if fn == nil {
		return fmt.Errorf("cache lock function is required for key %q", key)
	}

	lock, err := locker.Acquire(ctx, key)
	if err != nil {
		return err
	}
	return lock.run(fn)
}

// DoRooted runs fn while holding a descriptor-backed lock below root.
func (locker Locker) DoRooted(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	key Key,
	fn func() error,
) error {
	if fn == nil {
		return fmt.Errorf("rooted cache lock function is required for key %q", key)
	}
	lock, err := locker.acquireRooted(ctx, root, key)
	if err != nil {
		return err
	}
	return lock.run(fn)
}

// Release drops OS ownership without deleting the persistent lock record. It is
// safe to call more than once.
func (lock *Lock) Release() error {
	if lock == nil {
		return nil
	}

	lock.once.Do(func() {
		if lock.file == nil {
			lock.err = fmt.Errorf("release cache lock %q: lock is not initialized", lock.path)
			return
		}
		lock.err = lock.file.Unlock()
		if lock.err != nil {
			lock.err = fmt.Errorf("release cache lock %q: %w", lock.path, lock.err)
		}
	})

	return lock.err
}

func (lock *Lock) run(fn func() error) (returnErr error) {
	defer func() {
		returnErr = errors.Join(returnErr, lock.Release())
	}()
	if validator, ok := lock.file.(lockValidator); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("validate cache lock %q before operation: %w", lock.path, err)
		}
		defer func() {
			returnErr = errors.Join(
				returnErr,
				wrapLockValidation(lock.path, validator.Validate()),
			)
		}()
	}
	return fn()
}

func wrapLockValidation(path string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("validate cache lock %q after operation: %w", path, err)
}

func prepareLockRoot(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("lock root is not a non-symlink directory")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return err
	}
	return nil
}

func validateLockRecord(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("lock record has unsupported file mode %s", info.Mode())
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("lock record permissions are %04o, want 0600", info.Mode().Perm())
	}
	return nil
}

func prepareLockRecord(path string) (returnErr error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return validateLockRecord(path)
		}
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, file.Close())
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	return nil
}
