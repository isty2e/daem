//go:build darwin || linux

package filesnapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func readRegularFileAt(
	ctx context.Context,
	dir *os.File,
	name string,
	maximumBytes int64,
) (content []byte, exists bool, err error) {
	if ctx == nil {
		return nil, false, fmt.Errorf("file snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if dir == nil {
		return nil, false, fmt.Errorf("file snapshot directory descriptor is required")
	}
	if maximumBytes <= 0 {
		return nil, false, fmt.Errorf("maximum file size must be positive")
	}
	if err := validDirentName(name); err != nil {
		return nil, false, err
	}

	dirFD := int(dir.Fd())
	var before unix.Stat_t
	if err := unix.Fstatat(dirFD, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if before.Mode&unix.S_IFMT == unix.S_IFLNK {
		return nil, false, ErrSymlink
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, false, ErrNotRegular
	}
	if before.Size > maximumBytes {
		return nil, false, limitError(maximumBytes)
	}

	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	openedFD, err := unix.Openat(
		dirFD,
		name,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, false, classifyDirentOpenFailure(dirFD, name, before, err)
	}
	file := os.NewFile(uintptr(openedFD), name)
	if file == nil {
		_ = unix.Close(openedFD)
		return nil, false, errors.New("open regular file returned an invalid descriptor")
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !opened.Mode().IsRegular() {
		return nil, false, ErrChanged
	}
	if opened.Size() > maximumBytes {
		return nil, false, limitError(maximumBytes)
	}
	if !unixStatMatchesInfo(before, opened) {
		return nil, false, ErrChanged
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	buffer := make([]byte, 32*1024)
	content = make([]byte, 0, min(opened.Size(), int64(len(buffer))))
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		remaining := maximumBytes - int64(len(content))
		readSize := len(buffer)
		if remaining < int64(readSize) {
			readSize = int(remaining) + 1
		}
		count, readErr := file.Read(buffer[:readSize])
		if count > 0 {
			content = append(content, buffer[:count]...)
			if int64(len(content)) > maximumBytes {
				return nil, false, limitError(maximumBytes)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, false, readErr
			}
			break
		}
		if count == 0 {
			return nil, false, fmt.Errorf("read regular file: no progress")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	afterOpen, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	var after unix.Stat_t
	if err := unix.Fstatat(dirFD, name, &after, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, false, ErrChanged
		}
		return nil, false, fmt.Errorf("reinspect file: %w", err)
	}
	if after.Mode&unix.S_IFMT != unix.S_IFREG ||
		!unixStatsMatch(before, after) ||
		!os.SameFile(opened, afterOpen) ||
		!sameFileVersion(opened, afterOpen) ||
		!unixStatMatchesInfo(after, afterOpen) ||
		int64(len(content)) != opened.Size() ||
		int64(len(content)) != afterOpen.Size() {
		return nil, false, ErrChanged
	}
	return content, true, nil
}

func unixStatMtime(stat unix.Stat_t) changeVersion {
	return changeVersion{seconds: int64(stat.Mtim.Sec), nanoseconds: int64(stat.Mtim.Nsec)}
}

func unixStatCtime(stat unix.Stat_t) changeVersion {
	return changeVersion{seconds: int64(stat.Ctim.Sec), nanoseconds: int64(stat.Ctim.Nsec)}
}

func classifyDirentOpenFailure(dirFD int, name string, before unix.Stat_t, openErr error) error {
	if errors.Is(openErr, unix.ENOENT) {
		return ErrChanged
	}
	var after unix.Stat_t
	if err := unix.Fstatat(dirFD, name, &after, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return ErrChanged
		}
		return openErr
	}
	if !unixStatsMatch(before, after) {
		return ErrChanged
	}
	if errors.Is(openErr, unix.ELOOP) {
		return ErrChanged
	}
	return openErr
}

func unixStatsMatch(left unix.Stat_t, right unix.Stat_t) bool {
	return uint64(left.Dev) == uint64(right.Dev) &&
		uint64(left.Ino) == uint64(right.Ino) &&
		left.Size == right.Size &&
		unixStatMtime(left) == unixStatMtime(right) &&
		unixStatCtime(left) == unixStatCtime(right)
}

func unixStatMatchesInfo(stat unix.Stat_t, info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() {
		return false
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return uint64(stat.Dev) == uint64(sys.Dev) &&
		uint64(stat.Ino) == uint64(sys.Ino) &&
		stat.Size == sys.Size &&
		unixStatMtime(stat) == fileInfoMtime(info) &&
		unixStatCtime(stat) == fileInfoCtime(info)
}

func fileInfoMtime(info os.FileInfo) changeVersion {
	return changeVersion{seconds: info.ModTime().Unix(), nanoseconds: int64(info.ModTime().Nanosecond())}
}

func fileInfoCtime(info os.FileInfo) changeVersion {
	change, ok := fileChangeVersion(info)
	if !ok {
		return changeVersion{}
	}
	return change
}
