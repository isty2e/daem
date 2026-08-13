//go:build darwin || linux

package filesnapshot

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openRegularFile(path string, followFinalSymlink bool) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK
	if !followFinalSymlink {
		flags |= unix.O_NOFOLLOW
	}
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, ErrChanged
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open regular file returned an invalid descriptor")
	}
	return file, nil
}
