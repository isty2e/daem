//go:build darwin || linux || freebsd || netbsd || openbsd

package filesnapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func readRegularFileAtCounted(
	ctx context.Context,
	dir *os.File,
	name string,
	maximumBytes int64,
) (CountedContent, error) {
	return readRegularFileAtCountedWithHooks(ctx, dir, name, maximumBytes, readHooks{})
}

func readRegularFileAtCountedWithHooks(
	ctx context.Context,
	dir *os.File,
	name string,
	maximumBytes int64,
	hooks readHooks,
) (CountedContent, error) {
	if ctx == nil {
		return CountedContent{}, fmt.Errorf("file snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return CountedContent{}, err
	}
	if dir == nil {
		return CountedContent{}, fmt.Errorf("file snapshot directory descriptor is required")
	}
	if maximumBytes <= 0 {
		return CountedContent{}, fmt.Errorf("maximum file size must be positive")
	}
	if err := validDirentName(name); err != nil {
		return CountedContent{}, err
	}

	dirFD := int(dir.Fd())
	var before unix.Stat_t
	if err := unix.Fstatat(dirFD, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return CountedContent{}, nil
		}
		return CountedContent{}, err
	}
	if before.Mode&unix.S_IFMT == unix.S_IFLNK {
		return CountedContent{}, ErrSymlink
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return CountedContent{}, ErrNotRegular
	}
	if before.Size > maximumBytes {
		return CountedContent{}, limitError(maximumBytes)
	}
	if hooks.afterInspect != nil {
		hooks.afterInspect()
	}

	if err := ctx.Err(); err != nil {
		return CountedContent{}, err
	}
	openedFD, err := unix.Openat(
		dirFD,
		name,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return CountedContent{}, classifyDirentOpenFailure(dirFD, name, before, err)
	}
	file := os.NewFile(uintptr(openedFD), name)
	if file == nil {
		_ = unix.Close(openedFD)
		return CountedContent{}, errors.New("open regular file returned an invalid descriptor")
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil {
		return CountedContent{}, err
	}
	if !opened.Mode().IsRegular() {
		return CountedContent{}, ErrChanged
	}
	if !unixStatMatchesInfo(before, opened) {
		return CountedContent{}, ErrChanged
	}
	if opened.Size() > maximumBytes {
		return CountedContent{}, limitError(maximumBytes)
	}
	if err := ctx.Err(); err != nil {
		return CountedContent{}, err
	}

	content, attempted, err := readBoundedRegularFile(ctx, file, maximumBytes, opened.Size())
	if err != nil {
		return CountedContent{Attempted: attempted}, err
	}
	if err := ctx.Err(); err != nil {
		return CountedContent{Attempted: attempted}, err
	}

	afterOpen, err := file.Stat()
	if err != nil {
		return CountedContent{Attempted: attempted}, err
	}
	var after unix.Stat_t
	if err := unix.Fstatat(dirFD, name, &after, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return CountedContent{Attempted: attempted}, ErrChanged
		}
		return CountedContent{Attempted: attempted}, fmt.Errorf("reinspect file: %w", err)
	}
	if after.Mode&unix.S_IFMT != unix.S_IFREG ||
		!unixStatsMatch(before, after) ||
		!os.SameFile(opened, afterOpen) ||
		!sameFileVersion(opened, afterOpen) ||
		!unixStatMatchesInfo(after, afterOpen) ||
		int64(len(content)) != opened.Size() ||
		int64(len(content)) != afterOpen.Size() {
		return CountedContent{Attempted: attempted}, ErrChanged
	}
	return CountedContent{Content: content, Exists: true, Attempted: attempted}, nil
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
		unixStatCtime(stat) == fileInfoAtCtime(info)
}

func fileInfoMtime(info os.FileInfo) changeVersion {
	return changeVersion{seconds: info.ModTime().Unix(), nanoseconds: int64(info.ModTime().Nanosecond())}
}
