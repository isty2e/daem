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
	name   windowsComponent
	handle *windowsOwnedHandle
}

type windowsRelativeKind uint8

const (
	windowsRelativeAny windowsRelativeKind = iota + 1
	windowsRelativeFile
	windowsRelativeDirectory
)

func openWindowsRelativeChild(
	parent windows.Handle,
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
	parent windows.Handle,
	name string,
	access uint32,
	share uint32,
	disposition uint32,
	kind windowsRelativeKind,
	writeThrough bool,
	security *windows.SECURITY_DESCRIPTOR,
) (*windowsRelativeOpen, error) {
	if parent == 0 || parent == windows.InvalidHandle {
		return nil, fmt.Errorf("Windows parent handle is required")
	}
	component, err := parseWindowsComponent(name)
	if err != nil {
		return nil, err
	}
	parentFacts, err := queryWindowsEntryFacts(parent)
	if err != nil {
		return nil, err
	}
	if !parentFacts.standard.directory || parentFacts.attribute.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, fmt.Errorf("Windows relative-open parent must be a non-reparse directory")
	}
	if err := validateWindowsComponentForVolume(component, parentFacts.volume.maximumComponentUTF16); err != nil {
		return nil, err
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
	caseSensitive, err := queryWindowsCaseSensitive(parent)
	if err != nil {
		return nil, err
	}
	objectName, err := windows.NewNTUnicodeString(component.value)
	if err != nil {
		return nil, normalizeWindowsNativeError(operationPhase, err, false)
	}
	attributes := uint32(0)
	if !caseSensitive {
		attributes |= windows.OBJ_CASE_INSENSITIVE
	}
	objectAttributes := &windows.OBJECT_ATTRIBUTES{
		Length:             uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory:      parent,
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
	attributesOnCreate := uint32(windows.FILE_ATTRIBUTE_NORMAL)
	if kind == windowsRelativeDirectory {
		attributesOnCreate = windows.FILE_ATTRIBUTE_DIRECTORY
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
	return &windowsRelativeOpen{name: component, handle: owner}, nil
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
	parent windows.Handle,
	name string,
	access uint32,
	share uint32,
	disposition uint32,
	writeThrough bool,
) (*windowsRelativeOpen, error) {
	return openWindowsRelativeChild(parent, name, access, share, disposition, windowsRelativeFile, writeThrough)
}

func openWindowsRelativeDirectory(
	parent windows.Handle,
	name string,
	access uint32,
	share uint32,
	disposition uint32,
	writeThrough bool,
) (*windowsRelativeOpen, error) {
	return openWindowsRelativeChild(parent, name, access, share, disposition, windowsRelativeDirectory, writeThrough)
}

func createWindowsRelativeFile(
	parent windows.Handle,
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
	parent windows.Handle,
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
	parent windows.Handle,
	name string,
	access uint32,
	share uint32,
	disposition uint32,
	writeThrough bool,
) (*windowsRelativeOpen, error) {
	return openWindowsRelativeChild(parent, name, access, share, disposition, windowsRelativeAny, writeThrough)
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
