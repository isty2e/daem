//go:build darwin || linux

package mutation

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openRevisionDirectory(path string) (*os.File, error) {
	return openRevisionPath(
		path,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
	)
}

func openRevisionRegularFile(path string) (*os.File, error) {
	return openRevisionPath(
		path,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
	)
}

func openRevisionPath(path string, flags int) (*os.File, error) {
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open mutation revision path returned an invalid descriptor")
	}
	return file, nil
}
