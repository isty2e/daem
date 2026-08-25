//go:build darwin || linux

package commit

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func openExpectedAt(parentFD int, name string, path string, expected EntryIdentity) (int, error) {
	observed, before, err := observeAt(parentFD, name, path)
	if err != nil {
		return -1, err
	}
	if !expected.sameEntry(observed) {
		return -1, fmt.Errorf("entry identity changed at %q", path)
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if observed.kind == entryKindDirectory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat(parentFD, name, flags, 0)
	if err != nil {
		return -1, err
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if !expected.sameEntry(identityFromStat(path, &opened)) {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("entry identity changed while opening %q", path)
	}
	if err := validateOwnedStat(path, &before); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}
