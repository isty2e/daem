//go:build darwin || linux

package commit

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"

	"golang.org/x/sys/unix"
)

type platformIdentity struct {
	device           uint64
	inode            uint64
	changeTimeSecond int64
	changeTimeNano   int64
}

func (identity platformIdentity) valid() bool {
	return identity.device != 0 || identity.inode != 0
}

func (identity platformIdentity) matches(other platformIdentity) bool {
	return identity == other
}

func (identity platformIdentity) sameObject(other platformIdentity) bool {
	return identity.valid() && other.valid() && identity.device == other.device && identity.inode == other.inode
}

func (anchor *anchoredParent) observe(name string, path string) (EntryIdentity, unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(anchor.parentFD(), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return EntryIdentity{}, unix.Stat_t{}, err
	}
	identity := identityFromStat(path, &stat)
	if !identity.valid() || identity.kind == entryKindSpecial {
		return EntryIdentity{}, unix.Stat_t{}, unsupported(fmt.Sprintf("unsupported entry kind at %q", path), nil)
	}
	return identity, stat, nil
}

func (anchor *anchoredParent) requireExpected(
	name string,
	path string,
	expected EntryIdentity,
) (EntryIdentity, unix.Stat_t, error) {
	observed, stat, err := anchor.observe(name, path)
	if err != nil {
		return EntryIdentity{}, unix.Stat_t{}, err
	}
	if !expected.sameEntry(observed) {
		return EntryIdentity{}, unix.Stat_t{}, fmt.Errorf("entry identity changed at %q", path)
	}
	return observed, stat, nil
}

func (anchor *anchoredParent) openExpected(
	name string,
	path string,
	expected EntryIdentity,
) (int, unix.Stat_t, error) {
	observed, before, err := anchor.requireExpected(name, path, expected)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if observed.kind == entryKindDirectory {
		flags |= unix.O_DIRECTORY
	}
	if observed.kind == entryKindSymlink {
		return -1, unix.Stat_t{}, fmt.Errorf("symbolic link cannot be opened without following")
	}
	fd, err := unix.Openat(anchor.parentFD(), name, flags, 0)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, err
	}
	if !observed.sameEntry(identityFromStat(path, &opened)) {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, fmt.Errorf("entry identity changed while opening %q", path)
	}
	if err := validateOwnedStat(path, &before); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, err
	}
	return fd, opened, nil
}

func identityFromStat(path string, stat *unix.Stat_t) EntryIdentity {
	changeTimeSecond, changeTimeNano := statChangeTime(stat)
	return EntryIdentity{
		path: path,
		kind: kindFromStat(stat),
		platform: platformIdentity{
			device:           uint64(stat.Dev),
			inode:            uint64(stat.Ino),
			changeTimeSecond: changeTimeSecond,
			changeTimeNano:   changeTimeNano,
		},
	}
}

func refreshOpenedIdentity(fd int, path string) (EntryIdentity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return EntryIdentity{}, err
	}
	identity := identityFromStat(path, &stat)
	if !identity.valid() {
		return EntryIdentity{}, unsupported(fmt.Sprintf("unsupported entry kind at %q", path), nil)
	}
	return identity, nil
}

func kindFromStat(stat *unix.Stat_t) entryKind {
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		return entryKindRegular
	case unix.S_IFDIR:
		return entryKindDirectory
	case unix.S_IFLNK:
		return entryKindSymlink
	default:
		return entryKindSpecial
	}
}

func validateOwnedStat(path string, stat *unix.Stat_t) error {
	if int(stat.Uid) != unix.Geteuid() {
		return unsupported(fmt.Sprintf("entry %q is not owned by the invoking user", path), nil)
	}
	return nil
}

func readDirectoryNames(fd int, path string) ([]string, error) {
	duplicate, err := unix.Dup(fd)
	if err != nil {
		return nil, err
	}
	if _, err := unix.Seek(duplicate, 0, 0); err != nil {
		_ = unix.Close(duplicate)
		return nil, fmt.Errorf("rewind directory descriptor for %q: %w", path, err)
	}
	directory := os.NewFile(uintptr(duplicate), path)
	if directory == nil {
		_ = unix.Close(duplicate)
		return nil, fmt.Errorf("wrap directory descriptor for %q", path)
	}
	names, readErr := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sort.Strings(names)
	return names, nil
}

func randomSiblingName(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value[:]), nil
}

func entryExists(parentFD int, name string) (bool, error) {
	var stat unix.Stat_t
	err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.ENOENT):
		return false, nil
	default:
		return false, err
	}
}

func removalFlags(kind entryKind) int {
	if kind == entryKindDirectory {
		return unix.AT_REMOVEDIR
	}
	return 0
}

func unsupportedOperationError(detail string, err error) error {
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		return unsupported(detail, err)
	}
	return err
}

func observeAt(parentFD int, name string, path string) (EntryIdentity, unix.Stat_t, error) {
	identity, stat, err := observeAnyAt(parentFD, name, path)
	if err != nil {
		return EntryIdentity{}, unix.Stat_t{}, err
	}
	if identity.kind == entryKindSpecial {
		return EntryIdentity{}, unix.Stat_t{}, unsupported(fmt.Sprintf("unsupported entry kind at %q", path), nil)
	}
	return identity, stat, nil
}

func observeAnyAt(parentFD int, name string, path string) (EntryIdentity, unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return EntryIdentity{}, unix.Stat_t{}, err
	}
	identity := identityFromStat(path, &stat)
	if !identity.valid() {
		return EntryIdentity{}, unix.Stat_t{}, unsupported(fmt.Sprintf("unsupported entry kind at %q", path), nil)
	}
	return identity, stat, nil
}
