//go:build windows

package codexplugin

import (
	"errors"
	"os"
	"unsafe"

	"github.com/isty2e/daem/internal/filesnapshot"
	"golang.org/x/sys/windows"
)

type windowsTreeFileIDInfo struct {
	VolumeSerialNumber uint64
	FileID             [16]byte
}

func openDirectory(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("wrap Codex plugin directory handle")
	}
	info, err := file.Stat()
	if err != nil || !info.IsDir() {
		return nil, errors.Join(errors.New("not a directory"), err, file.Close())
	}
	if err := validateWindowsTreeIdentity(handle); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func openChildDirectoryNoFollow(parent *os.File, name string) (*os.File, error) {
	if parent == nil {
		return nil, errors.New("Codex plugin directory descriptor is required")
	}
	if !validDirentComponent(name) {
		return nil, filesnapshot.ErrSymlink
	}
	file, err := filesnapshot.OpenEntryAt(parent, name)
	if errors.Is(err, filesnapshot.ErrUnsupported) {
		return nil, errDescriptorRelativeTreeUnsupported
	}
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.IsDir() {
		return nil, errors.Join(errors.New("not a directory"), err, file.Close())
	}
	return file, nil
}

func classifyChild(parent *os.File, name string) (childKind, error) {
	if parent == nil {
		return childMissing, errors.New("Codex plugin directory descriptor is required")
	}
	if !validDirentComponent(name) {
		return childSymlink, filesnapshot.ErrSymlink
	}
	file, err := filesnapshot.OpenEntryAt(parent, name)
	if errors.Is(err, os.ErrNotExist) {
		return childMissing, nil
	}
	if errors.Is(err, filesnapshot.ErrSymlink) {
		return childSymlink, nil
	}
	if errors.Is(err, filesnapshot.ErrUnsupported) {
		return childMissing, errDescriptorRelativeTreeUnsupported
	}
	if err != nil {
		return childMissing, err
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil {
		return childMissing, errors.Join(statErr, closeErr)
	}
	switch {
	case info.IsDir():
		return childDirectory, nil
	case info.Mode().IsRegular():
		return childFile, nil
	default:
		return childOther, nil
	}
}

func directoryPathBlocked(err error) bool {
	return errors.Is(err, filesnapshot.ErrSymlink)
}

func validateWindowsTreeIdentity(handle windows.Handle) error {
	var info windowsTreeFileIDInfo
	err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil || info.VolumeSerialNumber == 0 || info.FileID == [16]byte{} {
		return errDescriptorRelativeTreeUnsupported
	}
	return nil
}
