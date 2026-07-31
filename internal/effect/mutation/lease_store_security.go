//go:build !darwin && !linux

package mutation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

type fallbackLeaseNamespace struct {
	root string
}

type fallbackLeaseRecord struct {
	file *flock.Flock
}

func initialLeaseRootIdentity(_ string, root string) (canonicalPath, error) {
	return canonicalPathIdentity(root, PathEffectReferent)
}

func (store Store) openLeaseNamespace() (preparedLeaseNamespace, error) {
	if err := store.preparePath(); err != nil {
		return nil, err
	}
	return &fallbackLeaseNamespace{root: store.root}, nil
}

func (store Store) prepare() error {
	namespace, err := store.openLeaseNamespace()
	if err != nil {
		return err
	}
	return namespace.Close()
}

func (store Store) preparePath() error {
	if strings.TrimSpace(store.root) == "" || store.maximum <= 0 || store.interval <= 0 {
		return fmt.Errorf("mutation lease store is not initialized")
	}
	dataDir := filepath.Dir(filepath.Dir(filepath.Dir(store.root)))
	if err := ensureBaseDirectory(dataDir); err != nil {
		return err
	}
	for _, directory := range []string{
		filepath.Dir(filepath.Dir(store.root)),
		filepath.Dir(store.root),
		store.root,
	} {
		if err := ensurePrivateDirectory(directory); err != nil {
			return err
		}
	}
	identity, err := canonicalPathIdentity(store.root, PathEffectReferent)
	if err != nil {
		return fmt.Errorf("verify mutation lease store: %w", err)
	}
	if !store.matchesRootIdentity(identity) {
		return fmt.Errorf("mutation lease store identity changed")
	}
	return nil
}

func (namespace *fallbackLeaseNamespace) Acquire(
	ctx context.Context,
	name string,
	access AccessMode,
	interval time.Duration,
) (leaseRecord, bool, error) {
	lockPath := filepath.Join(namespace.root, name)
	if err := validateLockRecord(lockPath); err != nil {
		return nil, false, err
	}
	file := flock.New(lockPath, flock.SetPermissions(0o600))
	locked, err := acquireFallbackFlock(ctx, file, access, interval)
	if err != nil || !locked {
		return nil, locked, err
	}
	if err := secureAcquiredLockRecord(lockPath, file); err != nil {
		return nil, false, errors.Join(err, file.Unlock())
	}
	return fallbackLeaseRecord{file: file}, true, nil
}

func (namespace *fallbackLeaseNamespace) ValidateCurrent() error {
	if namespace == nil || namespace.root == "" {
		return fmt.Errorf("mutation lease namespace is not initialized")
	}
	return nil
}

func (namespace *fallbackLeaseNamespace) Close() error {
	return nil
}

func (record fallbackLeaseRecord) Unlock() error {
	if record.file == nil {
		return nil
	}
	return record.file.Unlock()
}

func acquireFallbackFlock(
	ctx context.Context,
	file *flock.Flock,
	access AccessMode,
	interval time.Duration,
) (bool, error) {
	if access == AccessShared {
		return file.TryRLockContext(ctx, interval)
	}
	return file.TryLockContext(ctx, interval)
}

func ensureBaseDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("inspect mutation data directory %q: %w", path, err)
		}
		if err := validateLeaseCreationAnchor(path); err != nil {
			return err
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create mutation data directory %q: %w", path, err)
		}
		info, err = os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect created mutation data directory %q: %w", path, err)
		}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("mutation data path %q is not a regular directory", path)
	}
	if err := validateLeaseEntryOwner(path, info); err != nil {
		return err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("mutation data directory %q is group/world-writable with mode %04o", path, info.Mode().Perm())
	}
	return validateLeaseParent(path)
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("inspect mutation lease directory %q: %w", path, err)
		}
		if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
			return fmt.Errorf("create mutation lease directory %q: %w", path, err)
		}
		info, err = os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect created mutation lease directory %q: %w", path, err)
		}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("mutation lease directory %q is not a regular directory", path)
	}
	if err := validateLeaseEntryOwner(path, info); err != nil {
		return err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("mutation lease directory %q is group/world-writable with mode %04o", path, info.Mode().Perm())
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure mutation lease directory %q: %w", path, err)
	}
	secured, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("verify secured mutation lease directory %q: %w", path, err)
	}
	if !os.SameFile(info, secured) || secured.Mode().Perm() != 0o700 {
		return fmt.Errorf("mutation lease directory %q changed while securing it", path)
	}
	return nil
}

func validateLockRecord(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect mutation lock record: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("mutation lock record has unsupported file mode %s", info.Mode())
	}
	if err := validateLeaseEntryOwner(path, info); err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("mutation lock record permissions %04o are not private", info.Mode().Perm())
	}
	return nil
}

func validateLeaseCreationAnchor(path string) error {
	candidate := filepath.Dir(path)
	for {
		info, err := os.Lstat(candidate)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("mutation data ancestor %q is not a regular directory", candidate)
			}
			if err := validateLeaseEntryOwner(candidate, info); err != nil {
				return err
			}
			if info.Mode().Perm()&0o022 != 0 {
				return fmt.Errorf(
					"mutation data ancestor %q is group/world-writable with mode %04o",
					candidate,
					info.Mode().Perm(),
				)
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("inspect mutation data ancestor %q: %w", candidate, err)
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return fmt.Errorf("mutation data directory %q has no inspectable parent", path)
		}
		candidate = parent
	}
}

func validateLeaseParent(path string) error {
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect mutation data parent %q: %w", parent, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("mutation data parent %q is not a regular directory", parent)
	}
	if err := validateLeaseEntryOwner(parent, info); err != nil {
		return err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("mutation data parent %q is group/world-writable with mode %04o", parent, info.Mode().Perm())
	}
	return nil
}

func secureAcquiredLockRecord(path string, lockFile *flock.Flock) error {
	lockedInfo, err := lockFile.Stat()
	if err != nil {
		return fmt.Errorf("inspect acquired lock descriptor: %w", err)
	}
	if !lockedInfo.Mode().IsRegular() {
		return fmt.Errorf("acquired mutation lock record has unsupported file mode %s", lockedInfo.Mode())
	}
	if err := validateLeaseEntryOwner(path, lockedInfo); err != nil {
		return err
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect acquired mutation lock record: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(lockedInfo, pathInfo) {
		return fmt.Errorf("mutation lock record changed while acquiring it")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set private lock record mode: %w", err)
	}
	securedInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("verify secured mutation lock record: %w", err)
	}
	if !os.SameFile(lockedInfo, securedInfo) || securedInfo.Mode().Perm() != 0o600 {
		return fmt.Errorf("mutation lock record changed while securing it")
	}
	return nil
}
