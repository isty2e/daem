//go:build windows

package filesnapshot

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func openRegularFile(path string, _ bool) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	handle, err := windows.CreateFile(
		name,
		windows.FILE_GENERIC_READ,
		windowsSnapshotShareMode,
		nil,
		windows.OPEN_EXISTING,
		flags,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("open regular file returned an invalid handle")
	}
	return file, nil
}
