//go:build windows

package filesnapshot

import (
	"errors"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsFileID struct {
	volume uint64
	file   [16]byte
}

type windowsFileIDInfo struct {
	VolumeSerialNumber uint64
	FileID             [16]byte
}

type windowsFileBasicInfo struct {
	CreationTime   int64
	LastAccessTime int64
	LastWriteTime  int64
	ChangeTime     int64
	FileAttributes uint32
}

func openEntryAt(dir *os.File, name string) (*os.File, error) {
	if strings.ContainsRune(name, ':') {
		return nil, ErrPathBlocked
	}
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	attributes := &windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(dir.Fd()),
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
	}
	var handle windows.Handle
	err = windows.NtCreateFile(
		&handle,
		windows.FILE_GENERIC_READ,
		attributes,
		&windows.IO_STATUS_BLOCK{},
		nil,
		windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN,
		windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if err != nil {
		return nil, windowsOpenError(err)
	}
	if _, err := queryWindowsFileID(handle); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	file := os.NewFile(uintptr(handle), name)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("open directory entry returned an invalid handle")
	}
	return file, nil
}

func windowsOpenError(err error) error {
	status, ok := err.(windows.NTStatus)
	if !ok {
		return err
	}
	if status == windows.STATUS_REPARSE_POINT_ENCOUNTERED {
		return ErrSymlink
	}
	return status.Errno()
}

func queryWindowsFileID(handle windows.Handle) (windowsFileID, error) {
	var info windowsFileIDInfo
	err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_FUNCTION) ||
			errors.Is(err, windows.ERROR_INVALID_PARAMETER) ||
			errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
			return windowsFileID{}, ErrUnsupported
		}
		return windowsFileID{}, err
	}
	return windowsFileIDFromInfo(info)
}

func windowsFileIDFromInfo(info windowsFileIDInfo) (windowsFileID, error) {
	if info.VolumeSerialNumber == 0 || info.FileID == [16]byte{} {
		return windowsFileID{}, ErrUnsupported
	}
	return windowsFileID{volume: info.VolumeSerialNumber, file: info.FileID}, nil
}

func queryWindowsFileBasicInfo(handle windows.Handle) (windowsFileBasicInfo, error) {
	var info windowsFileBasicInfo
	err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileBasicInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	return info, err
}
