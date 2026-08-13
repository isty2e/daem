//go:build darwin || linux

package mutation

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

type revisionNativeEntry struct {
	identity revisionNativeIdentity
	mode     uint32
	size     int64
}

func (entry revisionNativeEntry) isSymlink() bool {
	return entry.mode&unix.S_IFMT == unix.S_IFLNK
}

func (entry revisionNativeEntry) isDirectory() bool {
	return entry.mode&unix.S_IFMT == unix.S_IFDIR
}

func (entry revisionNativeEntry) isRegular() bool {
	return entry.mode&unix.S_IFMT == unix.S_IFREG
}

func (entry revisionNativeEntry) executable() bool {
	return entry.mode&0o111 != 0
}

func observeRevisionChild(directory *os.File, name string) (revisionNativeEntry, error) {
	if directory == nil {
		return revisionNativeEntry{}, fmt.Errorf("mutation revision parent directory is required")
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(int(directory.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return revisionNativeEntry{}, err
	}
	return revisionNativeEntryFromStat(&stat), nil
}

func openRevisionChild(
	directory *os.File,
	name string,
	expected revisionNativeEntry,
) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if expected.isDirectory() {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat(int(directory.Fd()), name, flags, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open mutation revision child returned an invalid descriptor")
	}
	opened, err := observeRevisionOpened(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !expected.identity.equal(opened.identity) {
		_ = file.Close()
		return nil, fmt.Errorf("mutation revision child %q changed while opening", name)
	}
	return file, nil
}

func verifyRevisionChild(
	directory *os.File,
	name string,
	opened *os.File,
	expected revisionNativeEntry,
) error {
	currentOpen, err := observeRevisionOpened(opened)
	if err != nil {
		return err
	}
	currentBinding, err := observeRevisionChild(directory, name)
	if err != nil {
		return err
	}
	if !expected.identity.equal(currentOpen.identity) ||
		!expected.identity.equal(currentBinding.identity) {
		return fmt.Errorf("mutation revision child %q changed while observed", name)
	}
	return nil
}

func observeRevisionOpened(file *os.File) (revisionNativeEntry, error) {
	if file == nil {
		return revisionNativeEntry{}, fmt.Errorf("open mutation revision entry is required")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return revisionNativeEntry{}, err
	}
	return revisionNativeEntryFromStat(&stat), nil
}

func readRevisionSymlink(
	directory *os.File,
	name string,
	expected revisionNativeEntry,
) (string, error) {
	const maximumRevisionSymlinkBytes = 1 << 20

	if expected.size < 0 || expected.size > maximumRevisionSymlinkBytes {
		return "", fmt.Errorf("mutation revision symlink %q target exceeds 1 MiB", name)
	}
	bufferSize := int(expected.size) + 1
	if bufferSize < 256 {
		bufferSize = 256
	}
	for {
		buffer := make([]byte, bufferSize)
		count, err := unix.Readlinkat(int(directory.Fd()), name, buffer)
		if err != nil {
			return "", err
		}
		if count < len(buffer) {
			current, err := observeRevisionChild(directory, name)
			if err != nil {
				return "", err
			}
			if !expected.identity.equal(current.identity) {
				return "", fmt.Errorf("mutation revision symlink %q changed while reading", name)
			}
			return string(buffer[:count]), nil
		}
		if bufferSize > maximumRevisionSymlinkBytes {
			return "", fmt.Errorf("mutation revision symlink %q target exceeds 1 MiB", name)
		}
		bufferSize *= 2
	}
}
