//go:build darwin || linux

package cache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

type rootedLockObject struct {
	device uint64
	inode  uint64
	kind   uint32
}

func rootedLockObjectFromStat(stat *unix.Stat_t) rootedLockObject {
	return rootedLockObject{
		device: uint64(stat.Dev),
		inode:  uint64(stat.Ino),
		kind:   uint32(stat.Mode & unix.S_IFMT),
	}
}

func (object rootedLockObject) equal(other rootedLockObject) bool {
	return object.device == other.device &&
		object.inode == other.inode &&
		object.kind == other.kind
}

type rootedLockDirectory struct {
	fd       int
	name     string
	identity rootedLockObject
}

type rootedAdvisoryLock struct {
	capability  rootedpath.CommitCapability
	rootFile    *os.File
	directories []rootedLockDirectory
	recordFD    int
	recordName  string
	record      rootedLockObject
}

func acquireRootedAdvisoryLock(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	pollInterval time.Duration,
) (_ lockReleaser, returnErr error) {
	if capability == nil {
		return nil, fmt.Errorf("rooted cache lock capability is required")
	}
	if err := ctx.Err(); err != nil {
		_ = capability.Close()
		return nil, err
	}
	lock := &rootedAdvisoryLock{
		capability: capability,
		recordFD:   -1,
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, lock.close())
		}
	}()

	rootFile, err := capability.OpenRootDirectory()
	if err != nil {
		return nil, err
	}
	lock.rootFile = rootFile
	var rootStat unix.Stat_t
	if err := unix.Fstat(int(rootFile.Fd()), &rootStat); err != nil {
		return nil, fmt.Errorf("inspect rooted cache lock root: %w", err)
	}
	lock.directories = append(lock.directories, rootedLockDirectory{
		fd:       int(rootFile.Fd()),
		identity: rootedLockObjectFromStat(&rootStat),
	})

	components := strings.Split(capability.Destination().Relative().Path(), "/")
	if len(components) == 0 {
		return nil, fmt.Errorf("rooted cache lock destination is empty")
	}
	for _, component := range components[:len(components)-1] {
		if err := lock.openDirectory(component); err != nil {
			return nil, err
		}
	}
	lock.recordName = components[len(components)-1]
	if err := lock.openRecord(); err != nil {
		return nil, err
	}
	if err := lock.tryAcquire(ctx, pollInterval); err != nil {
		return nil, err
	}
	if err := lock.Validate(); err != nil {
		return nil, err
	}
	return lock, nil
}

func (lock *rootedAdvisoryLock) openDirectory(name string) error {
	parent := lock.directories[len(lock.directories)-1]
	var before unix.Stat_t
	err := unix.Fstatat(parent.fd, name, &before, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		if err := unix.Mkdirat(parent.fd, name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("create rooted cache lock directory %q: %w", name, err)
		}
		err = unix.Fstatat(parent.fd, name, &before, unix.AT_SYMLINK_NOFOLLOW)
	}
	if err != nil {
		return fmt.Errorf("inspect rooted cache lock directory %q: %w", name, err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("rooted cache lock ancestor %q is not a non-symlink directory", name)
	}
	fd, err := unix.Openat(
		parent.fd,
		name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("open rooted cache lock directory %q: %w", name, err)
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("inspect opened rooted cache lock directory %q: %w", name, err)
	}
	beforeIdentity := rootedLockObjectFromStat(&before)
	openedIdentity := rootedLockObjectFromStat(&opened)
	if !beforeIdentity.equal(openedIdentity) {
		_ = unix.Close(fd)
		return fmt.Errorf("rooted cache lock directory %q changed while opening", name)
	}
	if err := unix.Fchmod(fd, 0o700); err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("set rooted cache lock directory %q mode: %w", name, err)
	}
	if err := capabilityValidatesDirectory(lock.capability, fd); err != nil {
		_ = unix.Close(fd)
		return err
	}
	lock.directories = append(lock.directories, rootedLockDirectory{
		fd:       fd,
		name:     name,
		identity: openedIdentity,
	})
	return nil
}

func (lock *rootedAdvisoryLock) openRecord() error {
	parent := lock.directories[len(lock.directories)-1]
	for range 4 {
		var before unix.Stat_t
		inspectErr := unix.Fstatat(parent.fd, lock.recordName, &before, unix.AT_SYMLINK_NOFOLLOW)
		created := errors.Is(inspectErr, unix.ENOENT)
		if inspectErr != nil && !created {
			return fmt.Errorf("inspect rooted cache lock record: %w", inspectErr)
		}
		flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if created {
			flags |= unix.O_CREAT | unix.O_EXCL
		}
		fd, err := unix.Openat(parent.fd, lock.recordName, flags, 0o600)
		if (created && errors.Is(err, unix.EEXIST)) ||
			(!created && errors.Is(err, unix.ENOENT)) {
			continue
		}
		if err != nil {
			return fmt.Errorf("open rooted cache lock record: %w", err)
		}
		lock.recordFD = fd
		var opened unix.Stat_t
		if err := unix.Fstat(fd, &opened); err != nil {
			return fmt.Errorf("inspect opened rooted cache lock record: %w", err)
		}
		if opened.Mode&unix.S_IFMT != unix.S_IFREG {
			return fmt.Errorf("rooted cache lock record is not a regular file")
		}
		if created {
			if err := unix.Fchmod(fd, 0o600); err != nil {
				return fmt.Errorf("set rooted cache lock record mode: %w", err)
			}
			if err := unix.Fstat(fd, &opened); err != nil {
				return fmt.Errorf("verify rooted cache lock record mode: %w", err)
			}
		}
		if fs.FileMode(opened.Mode).Perm() != 0o600 {
			return fmt.Errorf(
				"rooted cache lock record permissions are %04o, want 0600",
				fs.FileMode(opened.Mode).Perm(),
			)
		}
		lock.record = rootedLockObjectFromStat(&opened)
		if !created && !lock.record.equal(rootedLockObjectFromStat(&before)) {
			return fmt.Errorf("rooted cache lock record changed while opening")
		}
		return nil
	}
	return fmt.Errorf("rooted cache lock record changed repeatedly while opening")
}

func (lock *rootedAdvisoryLock) tryAcquire(
	ctx context.Context,
	pollInterval time.Duration,
) error {
	if pollInterval <= 0 {
		pollInterval = defaultLockPollInterval
	}
	for {
		err := unix.Flock(lock.recordFD, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return fmt.Errorf("lock rooted cache record: %w", err)
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (lock *rootedAdvisoryLock) Validate() error {
	if err := lock.validate(); err != nil {
		return fmt.Errorf("%w: validate rooted cache lock: %w", ErrRootedLockAuthority, err)
	}
	return nil
}

func (lock *rootedAdvisoryLock) validate() error {
	if lock == nil || lock.capability == nil || lock.rootFile == nil || lock.recordFD < 0 {
		return fmt.Errorf("rooted cache lock is not initialized")
	}
	if err := lock.capability.Validate(); err != nil {
		return err
	}
	for index, directory := range lock.directories {
		var opened unix.Stat_t
		if err := unix.Fstat(directory.fd, &opened); err != nil {
			return fmt.Errorf("inspect retained rooted cache lock directory: %w", err)
		}
		if !directory.identity.equal(rootedLockObjectFromStat(&opened)) {
			return fmt.Errorf("retained rooted cache lock directory identity changed")
		}
		if err := capabilityValidatesDirectory(lock.capability, directory.fd); err != nil {
			return err
		}
		if index == 0 {
			continue
		}
		parent := lock.directories[index-1]
		var bound unix.Stat_t
		if err := unix.Fstatat(parent.fd, directory.name, &bound, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return fmt.Errorf("revalidate rooted cache lock directory %q: %w", directory.name, err)
		}
		if !directory.identity.equal(rootedLockObjectFromStat(&bound)) {
			return fmt.Errorf("rooted cache lock directory %q binding changed", directory.name)
		}
	}
	parent := lock.directories[len(lock.directories)-1]
	var opened unix.Stat_t
	if err := unix.Fstat(lock.recordFD, &opened); err != nil {
		return fmt.Errorf("inspect retained rooted cache lock record: %w", err)
	}
	if !lock.record.equal(rootedLockObjectFromStat(&opened)) {
		return fmt.Errorf("retained rooted cache lock record identity changed")
	}
	var bound unix.Stat_t
	if err := unix.Fstatat(parent.fd, lock.recordName, &bound, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("revalidate rooted cache lock record: %w", err)
	}
	if !lock.record.equal(rootedLockObjectFromStat(&bound)) {
		return fmt.Errorf("rooted cache lock record binding changed")
	}
	return nil
}

func (lock *rootedAdvisoryLock) Unlock() error {
	if lock == nil {
		return nil
	}
	validationErr := lock.Validate()
	unlockErr := error(nil)
	if lock.recordFD >= 0 {
		unlockErr = unix.Flock(lock.recordFD, unix.LOCK_UN)
	}
	return errors.Join(validationErr, unlockErr, lock.close())
}

func (lock *rootedAdvisoryLock) close() error {
	if lock == nil {
		return nil
	}
	var closeErr error
	if lock.recordFD >= 0 {
		closeErr = errors.Join(closeErr, unix.Close(lock.recordFD))
		lock.recordFD = -1
	}
	for index := len(lock.directories) - 1; index >= 1; index-- {
		closeErr = errors.Join(closeErr, unix.Close(lock.directories[index].fd))
		lock.directories[index].fd = -1
	}
	lock.directories = nil
	if lock.rootFile != nil {
		closeErr = errors.Join(closeErr, lock.rootFile.Close())
		lock.rootFile = nil
	}
	if lock.capability != nil {
		closeErr = errors.Join(closeErr, lock.capability.Close())
		lock.capability = nil
	}
	return closeErr
}

func capabilityValidatesDirectory(
	capability rootedpath.CommitCapability,
	fd int,
) error {
	if err := capability.ValidateDirectoryHandle(uintptr(fd)); err != nil {
		return fmt.Errorf("validate rooted cache lock directory capability: %w", err)
	}
	return nil
}
