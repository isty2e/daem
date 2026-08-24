//go:build windows

package commit

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsParentShareMode      = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
	windowsPublicationShareMode = windows.FILE_SHARE_READ | windows.FILE_SHARE_DELETE
)

type windowsOwnedHandle struct {
	handle windows.Handle
	closed bool
}

func newWindowsOwnedHandle(handle windows.Handle) (*windowsOwnedHandle, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return nil, fmt.Errorf("Windows returned an invalid handle")
	}
	return &windowsOwnedHandle{handle: handle}, nil
}

func (owner *windowsOwnedHandle) Handle() windows.Handle {
	if owner == nil || owner.closed {
		return windows.InvalidHandle
	}
	return owner.handle
}

func (owner *windowsOwnedHandle) Close() error {
	if owner == nil || owner.closed {
		return nil
	}
	owner.closed = true
	handle := owner.handle
	owner.handle = windows.InvalidHandle
	if handle == 0 || handle == windows.InvalidHandle {
		return nil
	}
	return windows.CloseHandle(handle)
}

type windowsRelativeOpen struct {
	name      windowsComponent
	handle    *windowsOwnedHandle
	directory windowsDirectoryHandle
}

type windowsDirectoryHandle struct {
	handle        windows.Handle
	caseSensitive bool
}

func captureWindowsDirectoryHandle(handle windows.Handle) (windowsDirectoryHandle, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return windowsDirectoryHandle{}, fmt.Errorf("Windows directory handle is required")
	}
	caseSensitive, err := queryWindowsCaseSensitive(handle)
	if err != nil {
		return windowsDirectoryHandle{}, err
	}
	return windowsDirectoryHandle{handle: handle, caseSensitive: caseSensitive}, nil
}

func (directory windowsDirectoryHandle) Handle() windows.Handle {
	if directory.handle == 0 {
		return windows.InvalidHandle
	}
	return directory.handle
}

func (opened *windowsRelativeOpen) Directory() (windowsDirectoryHandle, error) {
	if opened == nil || opened.handle == nil || windowsHandleIsClosed(opened.handle) {
		return windowsDirectoryHandle{}, fmt.Errorf("opened Windows directory is unavailable")
	}
	if opened.directory.Handle() == windows.InvalidHandle {
		facts, err := queryWindowsEntryFacts(opened.handle.Handle())
		if err != nil {
			return windowsDirectoryHandle{}, err
		}
		if !facts.standard.directory || facts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return windowsDirectoryHandle{}, fmt.Errorf("opened Windows entry is not a non-reparse directory")
		}
		opened.directory, err = captureWindowsDirectoryHandle(opened.handle.Handle())
		if err != nil {
			return windowsDirectoryHandle{}, err
		}
	}
	return opened.directory, nil
}

type windowsRelativeKind uint8

const (
	windowsRelativeAny windowsRelativeKind = iota + 1
	windowsRelativeFile
	windowsRelativeDirectory
)

func openWindowsRelativeChild(
	parent windowsDirectoryHandle,
	name string,
	access uint32,
	share uint32,
	disposition uint32,
	kind windowsRelativeKind,
	writeThrough bool,
) (*windowsRelativeOpen, error) {
	return openWindowsRelativeChildWithSecurity(
		parent,
		name,
		access,
		share,
		disposition,
		kind,
		writeThrough,
		nil,
	)
}

func openWindowsRelativeChildWithSecurity(
	parent windowsDirectoryHandle,
	name string,
	access uint32,
	share uint32,
	disposition uint32,
	kind windowsRelativeKind,
	writeThrough bool,
	security *windows.SECURITY_DESCRIPTOR,
) (*windowsRelativeOpen, error) {
	if parent.Handle() == windows.InvalidHandle {
		return nil, fmt.Errorf("Windows parent handle is required")
	}
	access |= windows.SYNCHRONIZE
	component, err := parseWindowsComponent(name)
	if err != nil {
		return nil, err
	}
	parentFacts, err := queryWindowsEntryFacts(parent.Handle())
	if err != nil {
		return nil, err
	}
	if !parentFacts.standard.directory || parentFacts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, fmt.Errorf("Windows relative-open parent must be a non-reparse directory")
	}
	if err := validateWindowsComponentForVolume(component, parentFacts.volume.maximumComponentUTF16); err != nil {
		return nil, err
	}
	currentCaseSensitive, err := queryWindowsCaseSensitive(parent.Handle())
	if err != nil {
		return nil, err
	}
	if currentCaseSensitive != parent.caseSensitive {
		return nil, fmt.Errorf("Windows parent lookup case semantics changed")
	}
	if kind == windowsRelativeAny && disposition != windows.FILE_OPEN {
		return nil, fmt.Errorf("neutral Windows entry opens cannot create entries")
	}
	if disposition == windows.FILE_OPEN && security != nil {
		return nil, fmt.Errorf("Windows security descriptors are accepted only when creating entries")
	}
	operationPhase := windowsNativePhaseOpen
	if disposition != windows.FILE_OPEN {
		operationPhase = windowsNativePhaseCreate
	}
	objectName, err := windows.NewNTUnicodeString(component.value)
	if err != nil {
		return nil, normalizeWindowsNativeError(operationPhase, err, false)
	}
	attributes := uint32(0)
	if !parent.caseSensitive {
		attributes |= windows.OBJ_CASE_INSENSITIVE
	}
	objectAttributes := &windows.OBJECT_ATTRIBUTES{
		Length:             uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory:      parent.Handle(),
		ObjectName:         objectName,
		Attributes:         attributes,
		SecurityDescriptor: security,
	}
	options := uint32(
		windows.FILE_OPEN_FOR_BACKUP_INTENT |
			windows.FILE_SYNCHRONOUS_IO_NONALERT |
			windows.FILE_OPEN_REPARSE_POINT,
	)
	switch kind {
	case windowsRelativeAny:
	case windowsRelativeFile:
		options |= windows.FILE_NON_DIRECTORY_FILE
	case windowsRelativeDirectory:
		options |= windows.FILE_DIRECTORY_FILE
	default:
		return nil, fmt.Errorf("Windows relative-open entry kind is invalid")
	}
	options |= windowsWriteThroughOption(writeThrough)
	attributesOnCreate := uint32(0)
	if disposition != windows.FILE_OPEN {
		attributesOnCreate = windows.FILE_ATTRIBUTE_NORMAL
		if kind == windowsRelativeDirectory {
			attributesOnCreate = windows.FILE_ATTRIBUTE_DIRECTORY
		}
	}
	var handle windows.Handle
	if err := windows.NtCreateFile(
		&handle,
		access,
		objectAttributes,
		&windows.IO_STATUS_BLOCK{},
		nil,
		attributesOnCreate,
		share,
		disposition,
		options,
		0,
		0,
	); err != nil {
		return nil, normalizeWindowsNativeError(operationPhase, err, false)
	}
	owner, err := newWindowsOwnedHandle(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, normalizeWindowsNativeError(operationPhase, err, false)
	}
	opened := &windowsRelativeOpen{name: component, handle: owner}
	if kind == windowsRelativeDirectory {
		directory, directoryErr := captureWindowsDirectoryHandle(owner.Handle())
		if directoryErr != nil {
			_ = owner.Close()
			return nil, directoryErr
		}
		opened.directory = directory
	}
	return opened, nil
}

func queryWindowsCaseSensitive(handle windows.Handle) (bool, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return false, fmt.Errorf("Windows directory handle is required")
	}
	var buffer [4]byte
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileCaseSensitiveInfo,
		&buffer[0],
		uint32(len(buffer)),
	); err != nil {
		return false, normalizeWindowsNativeError(windowsNativePhaseIdentity, err, false)
	}
	return buffer[0]&1 != 0, nil
}

func windowsWriteThroughOption(enabled bool) uint32 {
	if enabled {
		return windows.FILE_WRITE_THROUGH
	}
	return 0
}

func openWindowsRelativeFile(
	parent windowsDirectoryHandle,
	name string,
	access uint32,
	share uint32,
	disposition uint32,
	writeThrough bool,
) (*windowsRelativeOpen, error) {
	return openWindowsRelativeChild(parent, name, access, share, disposition, windowsRelativeFile, writeThrough)
}

func openWindowsRelativeDirectory(
	parent windowsDirectoryHandle,
	name string,
	access uint32,
	share uint32,
	disposition uint32,
	writeThrough bool,
) (*windowsRelativeOpen, error) {
	return openWindowsRelativeChild(parent, name, access, share, disposition, windowsRelativeDirectory, writeThrough)
}

func createWindowsRelativeFile(
	parent windowsDirectoryHandle,
	name string,
	access uint32,
	share uint32,
	writeThrough bool,
	security *windows.SECURITY_DESCRIPTOR,
) (*windowsRelativeOpen, error) {
	if security == nil {
		return nil, fmt.Errorf("canonical Windows file security is required at creation")
	}
	return openWindowsRelativeChildWithSecurity(
		parent,
		name,
		access,
		share,
		windows.FILE_CREATE,
		windowsRelativeFile,
		writeThrough,
		security,
	)
}

func createWindowsRelativeDirectory(
	parent windowsDirectoryHandle,
	name string,
	access uint32,
	share uint32,
	writeThrough bool,
	security *windows.SECURITY_DESCRIPTOR,
) (*windowsRelativeOpen, error) {
	if security == nil {
		return nil, fmt.Errorf("canonical Windows directory security is required at creation")
	}
	return openWindowsRelativeChildWithSecurity(
		parent,
		name,
		access,
		share,
		windows.FILE_CREATE,
		windowsRelativeDirectory,
		writeThrough,
		security,
	)
}

func openWindowsRelativeEntry(
	parent windowsDirectoryHandle,
	name string,
	access uint32,
	share uint32,
	disposition uint32,
	writeThrough bool,
) (*windowsRelativeOpen, error) {
	opened, neutralErr := openWindowsRelativeChild(
		parent,
		name,
		access,
		share,
		disposition,
		windowsRelativeAny,
		writeThrough,
	)
	if neutralErr == nil {
		return opened, nil
	}
	file, fileErr := openWindowsRelativeChild(
		parent,
		name,
		access,
		share,
		disposition,
		windowsRelativeFile,
		writeThrough,
	)
	if fileErr == nil {
		return file, nil
	}
	directory, directoryErr := openWindowsRelativeChild(
		parent,
		name,
		access,
		share,
		disposition,
		windowsRelativeDirectory,
		writeThrough,
	)
	if directoryErr == nil {
		return directory, nil
	}
	for _, candidate := range []error{fileErr, directoryErr} {
		class := windowsNativeErrorClassOf(candidate)
		if class == windowsNativeErrorNotFound || class == windowsNativeErrorSharing {
			return nil, candidate
		}
	}
	if windowsEntryTypeMismatch(fileErr) {
		return nil, directoryErr
	}
	if windowsEntryTypeMismatch(directoryErr) {
		return nil, fileErr
	}
	return nil, neutralErr
}

func windowsEntryTypeMismatch(err error) bool {
	var status windows.NTStatus
	if errors.As(err, &status) {
		return status == windows.STATUS_FILE_IS_A_DIRECTORY || status == windows.STATUS_NOT_A_DIRECTORY
	}
	return errors.Is(err, windows.ERROR_DIRECTORY)
}

func duplicateWindowsOwnedHandle(handle windows.Handle) (*windowsOwnedHandle, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return nil, fmt.Errorf("cannot duplicate an invalid Windows handle")
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
		return nil, normalizeWindowsNativeError(windowsNativePhaseOpen, err, false)
	}
	owner, err := newWindowsOwnedHandle(duplicate)
	if err != nil {
		_ = windows.CloseHandle(duplicate)
		return nil, err
	}
	return owner, nil
}

func windowsHandleIsClosed(owner *windowsOwnedHandle) bool {
	return owner == nil || owner.closed || owner.handle == 0 || owner.handle == windows.InvalidHandle
}

func closeWindowsOwnedHandles(owners ...*windowsOwnedHandle) error {
	var failures []error
	for _, owner := range owners {
		if err := owner.Close(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
