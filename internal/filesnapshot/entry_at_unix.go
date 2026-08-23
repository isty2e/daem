//go:build darwin || linux || freebsd || netbsd || openbsd

package filesnapshot

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func openEntryAt(dir *os.File, name string) (*os.File, error) {
	dirFD := int(dir.Fd())
	var before unix.Stat_t
	if err := unix.Fstatat(dirFD, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, err
	}
	if before.Mode&unix.S_IFMT == unix.S_IFLNK {
		return nil, ErrSymlink
	}
	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, ErrSymlink
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open directory entry returned an invalid descriptor")
	}
	info, err := file.Stat()
	if err != nil || !unixStatSameObject(before, info) {
		return nil, errors.Join(ErrChanged, err, file.Close())
	}
	return file, nil
}

func unixStatSameObject(stat unix.Stat_t, info os.FileInfo) bool {
	if info == nil {
		return false
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint64(stat.Dev) == uint64(sys.Dev) && uint64(stat.Ino) == uint64(sys.Ino)
}
