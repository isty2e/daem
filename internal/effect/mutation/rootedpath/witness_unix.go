//go:build darwin || linux

package rootedpath

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func validateCapturedRootPlatform(platform *capturedRootPlatform) error {
	if platform == nil || len(platform.directories) < 2 {
		return newFailure(FailureRootUnavailable, "", "captured root witness is not initialized", nil)
	}
	for index := range platform.directories {
		current := platform.directories[index]
		var opened unix.Stat_t
		if err := unix.Fstat(current.fd, &opened); err != nil {
			return newFailure(FailureRootUnavailable, current.path, "inspect retained root descriptor", err)
		}
		if opened.Mode&unix.S_IFMT != unix.S_IFDIR || current.device != uint64(opened.Dev) || current.inode != uint64(opened.Ino) {
			return newFailure(FailureRootReplaced, current.path, "retained root descriptor changed identity", nil)
		}
		mount, err := nativeMountToken(current.fd)
		if err != nil {
			kind := FailureRootUnavailable
			if errors.Is(err, errMountIdentityUnsupported) {
				kind = FailureUnsupportedPlatform
			}
			return newFailure(kind, current.path, "revalidate root mount identity", err)
		}
		if mount != current.mount {
			return newFailure(FailureMountChanged, current.path, "captured root path changed mounts", nil)
		}
		if index == 0 {
			continue
		}
		parent := platform.directories[index-1]
		var linked unix.Stat_t
		if err := unix.Fstatat(parent.fd, current.name, &linked, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return newFailure(FailureRootReplaced, current.path, "physical root binding is unavailable", err)
		}
		if linked.Mode&unix.S_IFMT != unix.S_IFDIR || current.device != uint64(linked.Dev) || current.inode != uint64(linked.Ino) {
			return newFailure(FailureRootReplaced, current.path, "physical root binding changed", nil)
		}
	}
	return nil
}

func cloneCapturedRootPlatform(platform *capturedRootPlatform) (capturedRootPlatform, error) {
	if platform == nil || len(platform.directories) == 0 {
		return capturedRootPlatform{}, fmt.Errorf("captured root witness is not initialized")
	}
	cloned := capturedRootPlatform{directories: make([]capturedDirectory, 0, len(platform.directories))}
	for _, directory := range platform.directories {
		fd, err := unix.FcntlInt(uintptr(directory.fd), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			_ = closeCapturedRootPlatform(&cloned)
			return capturedRootPlatform{}, err
		}
		directory.fd = fd
		cloned.directories = append(cloned.directories, directory)
	}
	return cloned, nil
}

func closeCapturedRootPlatform(platform *capturedRootPlatform) error {
	if platform == nil {
		return nil
	}
	var failures []error
	for index := len(platform.directories) - 1; index >= 0; index-- {
		if platform.directories[index].fd >= 0 {
			if err := unix.Close(platform.directories[index].fd); err != nil {
				failures = append(failures, err)
			}
			platform.directories[index].fd = -1
		}
	}
	platform.directories = nil
	return errors.Join(failures...)
}

func openCapturedRootDirectory(platform *capturedRootPlatform) (*os.File, error) {
	if platform == nil || len(platform.directories) == 0 {
		return nil, fmt.Errorf("captured root witness is not initialized")
	}
	root := platform.directories[len(platform.directories)-1]
	fd, err := unix.FcntlInt(uintptr(root.fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), root.path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap duplicated root descriptor")
	}
	return file, nil
}

func validateCapturedDirectoryHandle(platform *capturedRootPlatform, handle uintptr) error {
	if platform == nil || len(platform.directories) == 0 {
		return newFailure(FailureRootUnavailable, "", "captured root witness is not initialized", nil)
	}
	mount, err := nativeMountToken(int(handle))
	if err != nil {
		kind := FailureRootUnavailable
		if errors.Is(err, errMountIdentityUnsupported) {
			kind = FailureUnsupportedPlatform
		}
		return newFailure(kind, platform.directories[len(platform.directories)-1].path, "inspect descendant mount identity", err)
	}
	root := platform.directories[len(platform.directories)-1]
	if mount != root.mount {
		return newFailure(FailureMountChanged, root.path, "destination crosses the captured root mount", nil)
	}
	return nil
}
