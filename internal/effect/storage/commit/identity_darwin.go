//go:build darwin

package commit

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

func observationDirectoryOpenFlags(bool) int {
	return unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
}

func statChangeTime(stat *unix.Stat_t) (int64, int64) {
	return stat.Ctim.Sec, stat.Ctim.Nsec
}

func retainedDirectoryStillLinked(fd int, expected EntryIdentity, _ *unix.Stat_t) (bool, error) {
	path, err := darwinOpenedPath(fd)
	if err != nil {
		return false, err
	}
	linked := false
	for range 2 {
		current, observeErr := darwinPathNamesObject(path, expected)
		if observeErr != nil {
			return false, observeErr
		}
		linked = linked || current
		nextPath, pathErr := darwinOpenedPath(fd)
		if pathErr != nil {
			return false, pathErr
		}
		if nextPath != path {
			return false, fmt.Errorf(
				"retained directory path changed during link observation from %q to %q",
				path,
				nextPath,
			)
		}
	}
	return linked, nil
}

func darwinOpenedPath(fd int) (string, error) {
	buffer := make([]byte, unix.PathMax)
	_, _, errno := unix.Syscall(
		unix.SYS_FCNTL,
		uintptr(fd),
		uintptr(unix.F_GETPATH),
		uintptr(unsafe.Pointer(&buffer[0])),
	)
	runtime.KeepAlive(buffer)
	if errno != 0 {
		return "", errno
	}
	if index := bytes.IndexByte(buffer, 0); index >= 0 {
		buffer = buffer[:index]
	}
	path := string(buffer)
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("F_GETPATH returned invalid path %q", path)
	}
	return path, nil
}

func darwinPathNamesObject(path string, expected EntryIdentity) (bool, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return false, err
	}
	return expected.sameObject(identityFromStat(path, &stat)), nil
}
