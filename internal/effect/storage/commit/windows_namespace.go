//go:build windows

package commit

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsRenameMode uint8

const (
	windowsRenameNoReplace windowsRenameMode = iota
	windowsRenameReplace
)

type windowsRenameMethod uint8

const (
	windowsRenameMethodEx windowsRenameMethod = iota + 1
)

func renameWindowsByHandle(
	source windows.Handle,
	parent windows.Handle,
	targetName string,
	mode windowsRenameMode,
) (windowsRenameMethod, error) {
	if source == 0 || source == windows.InvalidHandle || parent == 0 || parent == windows.InvalidHandle {
		return 0, fmt.Errorf("Windows rename handles are required")
	}
	component, err := parseWindowsComponent(targetName)
	if err != nil {
		return 0, err
	}
	if mode != windowsRenameNoReplace && mode != windowsRenameReplace {
		return 0, fmt.Errorf("Windows rename mode is invalid")
	}
	flags := uint32(windows.FILE_RENAME_POSIX_SEMANTICS)
	if mode == windowsRenameReplace {
		flags |= windows.FILE_RENAME_REPLACE_IF_EXISTS
	}
	if err := setWindowsRenameInformationEx(source, parent, component, flags); err != nil {
		if windowsRenameCompatibilityError(err) {
			return 0, windowsNativeUnsupported(
				windowsNativePhaseRename,
				"FILE_RENAME_INFO_EX is required for commit visibility",
				err,
			)
		}
		return 0, normalizeWindowsNativeError(windowsNativePhaseRename, err, false)
	}
	return windowsRenameMethodEx, nil
}

func setWindowsRenameInformationEx(
	source windows.Handle,
	parent windows.Handle,
	component windowsComponent,
	flags uint32,
) error {
	buffer := windowsRenameInformationBuffer(component, flags, parent)
	return windows.SetFileInformationByHandle(
		source,
		windows.FileRenameInfoEx,
		&buffer[0],
		uint32(len(buffer)),
	)
}

func windowsRenameInformationBuffer(
	component windowsComponent,
	flags uint32,
	parent windows.Handle,
) []byte {
	units := component.utf16()
	pointerSize := int(unsafe.Sizeof(parent))
	rootOffset := alignWindowsOffset(4, pointerSize)
	lengthOffset := rootOffset + pointerSize
	nameOffset := lengthOffset + 4
	buffer := make([]byte, nameOffset+len(units)*2)
	binary.LittleEndian.PutUint32(buffer[0:4], flags)
	putWindowsHandle(buffer[rootOffset:rootOffset+pointerSize], parent)
	binary.LittleEndian.PutUint32(buffer[lengthOffset:lengthOffset+4], uint32(len(units)*2))
	for index, unit := range units {
		binary.LittleEndian.PutUint16(buffer[nameOffset+index*2:], unit)
	}
	return buffer
}

func putWindowsHandle(buffer []byte, handle windows.Handle) {
	switch len(buffer) {
	case 4:
		binary.LittleEndian.PutUint32(buffer, uint32(handle))
	case 8:
		binary.LittleEndian.PutUint64(buffer, uint64(handle))
	default:
		panic("unsupported Windows handle width")
	}
}

func alignWindowsOffset(offset int, alignment int) int {
	return (offset + alignment - 1) &^ (alignment - 1)
}

func windowsRenameCompatibilityError(err error) bool {
	if err == nil {
		return false
	}
	class := windowsNativeErrorClassOf(err)
	if class == windowsNativeErrorCollision || class == windowsNativeErrorNotFound || class == windowsNativeErrorSharing {
		return false
	}
	var status windows.NTStatus
	if errors.As(err, &status) {
		return status == windows.STATUS_INVALID_INFO_CLASS || status == windows.STATUS_NOT_IMPLEMENTED ||
			status == windows.STATUS_NOT_SUPPORTED
	}
	return errors.Is(err, windows.ERROR_INVALID_FUNCTION) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_CALL_NOT_IMPLEMENTED)
}

func disposeWindowsByHandle(handle windows.Handle, ignoreReadOnly bool) (windowsRenameMethod, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return 0, fmt.Errorf("Windows disposition handle is required")
	}
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS)
	if ignoreReadOnly {
		flags |= windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE
	}
	var ex struct {
		Flags uint32
	}
	binary.LittleEndian.PutUint32((*[4]byte)(unsafe.Pointer(&ex))[:], flags)
	if err := windows.SetFileInformationByHandle(
		handle,
		windows.FileDispositionInfoEx,
		(*byte)(unsafe.Pointer(&ex)),
		uint32(unsafe.Sizeof(ex)),
	); err != nil {
		if windowsDispositionCompatibilityError(err) {
			return 0, windowsNativeUnsupported(
				windowsNativePhaseDisposition,
				"FILE_DISPOSITION_INFO_EX is required for commit visibility",
				err,
			)
		}
		return 0, normalizeWindowsNativeError(windowsNativePhaseDisposition, err, false)
	}
	return windowsRenameMethodEx, nil
}

func windowsDispositionCompatibilityError(err error) bool {
	return windowsRenameCompatibilityError(err)
}
