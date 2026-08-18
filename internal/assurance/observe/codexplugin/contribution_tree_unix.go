//go:build darwin || linux

package codexplugin

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("wrap Codex plugin directory descriptor")
	}
	return file, nil
}

func openChildDirectoryNoFollow(parent *os.File, name string) (*os.File, error) {
	if parent == nil {
		return nil, errors.New("Codex plugin directory descriptor is required")
	}
	if !validDirentComponent(name) {
		return nil, unix.ELOOP
	}
	fd, err := unix.Openat(
		int(parent.Fd()),
		name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		if kind, classifyErr := classifyChild(parent, name); classifyErr == nil && kind == childSymlink {
			return nil, unix.ELOOP
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("wrap Codex plugin directory descriptor")
	}
	return file, nil
}

func classifyChild(parent *os.File, name string) (childKind, error) {
	if parent == nil {
		return childMissing, errors.New("Codex plugin directory descriptor is required")
	}
	if !validDirentComponent(name) {
		return childSymlink, unix.ELOOP
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return childMissing, nil
		}
		return childMissing, err
	}
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFLNK:
		return childSymlink, nil
	case unix.S_IFDIR:
		return childDirectory, nil
	case unix.S_IFREG:
		return childFile, nil
	default:
		return childOther, nil
	}
}

func directoryPathBlocked(err error) bool {
	return errors.Is(err, unix.ELOOP)
}
