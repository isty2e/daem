//go:build windows

package rootedpath

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func validateCapturedRootPlatform(platform *capturedRootPlatform) error {
	if platform == nil || len(platform.directories) == 0 {
		return newFailure(FailureRootUnavailable, "", "captured root witness is not initialized", nil)
	}
	for index := range platform.directories {
		current := platform.directories[index]
		if current.handle == 0 || current.handle == windows.InvalidHandle {
			return newFailure(FailureRootUnavailable, current.path, "retained root handle is closed", nil)
		}
		facts, err := queryWindowsDirectoryFacts(current.handle)
		if err != nil {
			return windowsRootFailure(current.path, "inspect retained root handle", err)
		}
		if !facts.isDirectory {
			return newFailure(FailureRootReplaced, current.path, "retained root handle is no longer a directory", nil)
		}
		if facts.mount != current.mount {
			return newFailure(FailureMountChanged, current.path, "retained root handle changed volume", nil)
		}
		if facts.object != current.object {
			return newFailure(FailureRootReplaced, current.path, "retained root handle changed identity", nil)
		}
		if index == 0 {
			continue
		}
		parent := platform.directories[index-1]
		handle, facts, openErr := openWindowsChild(parent.handle, current.name)
		if openErr != nil {
			return newFailure(FailureRootReplaced, current.path, "physical root binding is unavailable", openErr)
		}
		if facts.reparse {
			_ = windows.CloseHandle(handle)
			return newFailure(FailureRootReplaced, current.path, "physical root binding became a reparse point", nil)
		}
		closeErr := windows.CloseHandle(handle)
		if closeErr != nil {
			return newFailure(FailureRootUnavailable, current.path, "close physical root binding probe", closeErr)
		}
		if facts.mount != current.mount || facts.object != current.object {
			return newFailure(FailureRootReplaced, current.path, "physical root binding changed", nil)
		}
	}
	return nil
}

func cloneCapturedRootPlatform(platform *capturedRootPlatform) (capturedRootPlatform, error) {
	if platform == nil || len(platform.directories) == 0 {
		return capturedRootPlatform{}, fmt.Errorf("captured root witness is not initialized")
	}
	cloned := capturedRootPlatform{
		directories:           make([]capturedDirectory, 0, len(platform.directories)),
		maximumComponentUTF16: platform.maximumComponentUTF16,
	}
	for _, directory := range platform.directories {
		duplicate, err := duplicateWindowsHandle(directory.handle)
		if err != nil {
			_ = closeCapturedRootPlatform(&cloned)
			return capturedRootPlatform{}, err
		}
		directory.handle = duplicate
		cloned.directories = append(cloned.directories, directory)
	}
	return cloned, nil
}

func duplicateWindowsHandle(handle windows.Handle) (windows.Handle, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return windows.InvalidHandle, fmt.Errorf("cannot duplicate an invalid Windows handle")
	}
	var duplicate windows.Handle
	if err := windows.DuplicateHandle(
		windows.CurrentProcess(),
		handle,
		windows.CurrentProcess(),
		&duplicate,
		0,
		false,
		windows.DUPLICATE_SAME_ACCESS,
	); err != nil {
		return windows.InvalidHandle, err
	}
	return duplicate, nil
}

func openCapturedRootDirectory(platform *capturedRootPlatform) (*os.File, error) {
	if platform == nil || len(platform.directories) == 0 {
		return nil, fmt.Errorf("captured root witness is not initialized")
	}
	root := platform.directories[len(platform.directories)-1]
	duplicate, err := duplicateWindowsHandle(root.handle)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(duplicate), root.path)
	if file == nil {
		_ = windows.CloseHandle(duplicate)
		return nil, fmt.Errorf("wrap duplicated root handle")
	}
	return file, nil
}

func openCapturedCommitRootDirectory(platform *capturedRootPlatform) (*os.File, error) {
	if platform == nil || len(platform.directories) < 2 {
		return nil, fmt.Errorf("captured commit root witness is not initialized")
	}
	root := platform.directories[len(platform.directories)-1]
	parent := platform.directories[len(platform.directories)-2]
	handle, facts, err := openWindowsChildWithAccess(
		parent.handle,
		root.name,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.FILE_TRAVERSE,
	)
	if err != nil {
		return nil, err
	}
	if facts.reparse || !facts.isDirectory || facts.mount != root.mount || facts.object != root.object {
		return nil, errors.Join(
			newFailure(FailureRootReplaced, root.path, "commit root binding changed", nil),
			closeWindowsHandle(handle),
		)
	}
	file := os.NewFile(uintptr(handle), root.path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("wrap commit root handle")
	}
	return file, nil
}

func validateCapturedDirectoryHandle(platform *capturedRootPlatform, handle uintptr) error {
	if platform == nil || len(platform.directories) == 0 {
		return newFailure(FailureRootUnavailable, "", "captured root witness is not initialized", nil)
	}
	root := platform.directories[len(platform.directories)-1]
	facts, err := queryWindowsDirectoryFacts(windows.Handle(handle))
	if err != nil {
		return windowsRootFailure(root.path, "inspect descendant directory volume", err)
	}
	if !facts.isDirectory {
		return newFailure(FailureRootUnavailable, "", "descendant handle is not a directory", nil)
	}
	if facts.mount != root.mount {
		return newFailure(FailureMountChanged, root.path, "destination crosses the captured root volume", nil)
	}
	return nil
}

func capturedRootChildExistsNoFollow(platform *capturedRootPlatform, name string) (bool, error) {
	if platform == nil || len(platform.directories) == 0 {
		return false, newFailure(FailureRootUnavailable, name, "captured root is unavailable", nil)
	}
	root := platform.directories[len(platform.directories)-1]
	probe, err := probeWindowsChild(root.handle, name)
	if err != nil {
		return false, windowsRootFailure(name, "observe retained-root child", err)
	}
	if !probe.exists {
		return false, nil
	}
	if probe.handle != 0 && probe.handle != windows.InvalidHandle {
		if err := windows.CloseHandle(probe.handle); err != nil {
			return false, newFailure(FailureRootUnavailable, name, "close retained-root child probe", err)
		}
	}
	return true, nil
}

func capturedRootValidationPathComponents(platform *capturedRootPlatform) (int, error) {
	if platform == nil || len(platform.directories) < 2 {
		return 0, newFailure(FailureRootUnavailable, "", "captured root witness is not initialized", nil)
	}
	return len(platform.directories) - 1, nil
}
