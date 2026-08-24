//go:build windows

package commit

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/windows"
)

func ReadRootedSymlinkTarget(
	ctx context.Context,
	capability rootedpath.CommitCapability,
) (string, EntryIdentity, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return "", EntryIdentity{}, err
	}
	anchor, err := openWindowsDestinationAnchor(ctx, capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return "", EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	observed, err := openWindowsObservedEntry(ctx, anchor, false, false, false)
	if observed != nil {
		defer observed.close()
	}
	if err != nil {
		return "", EntryIdentity{}, windowsFailureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	if observed.identity.kind != entryKindSymlink {
		return "", EntryIdentity{}, windowsFailureBeforeVisibility(phaseValidate, path, fmt.Errorf("entry is not a symbolic link or junction"))
	}
	target, err := readWindowsLinkTarget(observed.handle.Handle())
	if err != nil {
		return "", EntryIdentity{}, windowsFailureBeforeVisibility(phaseReadPayload, path, windowsUnsupportedCause(err))
	}
	if err := revalidateWindowsObservedEntry(ctx, anchor, observed); err != nil {
		return "", EntryIdentity{}, windowsFailureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	return target, observed.identity, nil
}

func readWindowsLinkTarget(handle windows.Handle) (string, error) {
	buffer := make([]byte, windows.MAXIMUM_REPARSE_DATA_BUFFER_SIZE)
	var returned uint32
	if err := windows.DeviceIoControl(
		handle,
		windows.FSCTL_GET_REPARSE_POINT,
		nil,
		0,
		&buffer[0],
		uint32(len(buffer)),
		&returned,
		nil,
	); err != nil {
		return "", normalizeWindowsNativeError(windowsNativePhaseRead, err, false)
	}
	const headerSize = 8
	if returned < headerSize || int(returned) > len(buffer) {
		return "", windowsNativeUnsupported(windowsNativePhaseRead, "reparse record length is invalid", nil)
	}
	dataLength := int(binary.LittleEndian.Uint16(buffer[4:6]))
	if headerSize+dataLength > int(returned) {
		return "", windowsNativeUnsupported(windowsNativePhaseRead, "reparse data is truncated", nil)
	}
	data := buffer[headerSize : headerSize+dataLength]
	tag := binary.LittleEndian.Uint32(buffer[:4])
	metadataSize := 0
	switch tag {
	case windows.IO_REPARSE_TAG_SYMLINK:
		metadataSize = 12
	case windows.IO_REPARSE_TAG_MOUNT_POINT:
		metadataSize = 8
	default:
		return "", windowsNativeUnsupported(windowsNativePhaseRead, fmt.Sprintf("reparse tag 0x%08x is not a link", tag), nil)
	}
	if len(data) < metadataSize {
		return "", windowsNativeUnsupported(windowsNativePhaseRead, "reparse link metadata is truncated", nil)
	}
	if tag == windows.IO_REPARSE_TAG_SYMLINK {
		const relativeFlag = uint32(1)
		flags := binary.LittleEndian.Uint32(data[8:12])
		if flags&^relativeFlag != 0 {
			return "", windowsNativeUnsupported(windowsNativePhaseRead, "symbolic-link flags are unsupported", nil)
		}
	}
	substitute, err := decodeWindowsReparseSpan(
		data,
		metadataSize,
		binary.LittleEndian.Uint16(data[0:2]),
		binary.LittleEndian.Uint16(data[2:4]),
	)
	if err != nil {
		return "", err
	}
	printName, err := decodeWindowsReparseSpan(
		data,
		metadataSize,
		binary.LittleEndian.Uint16(data[4:6]),
		binary.LittleEndian.Uint16(data[6:8]),
	)
	if err != nil {
		return "", err
	}
	target := printName
	if target == "" {
		target = substitute
		if strings.HasPrefix(target, `\??\UNC\`) {
			target = `\\` + strings.TrimPrefix(target, `\??\UNC\`)
		} else {
			target = strings.TrimPrefix(target, `\??\`)
		}
	}
	if target == "" {
		return "", windowsNativeUnsupported(windowsNativePhaseRead, "reparse link target is empty", nil)
	}
	return target, nil
}

func decodeWindowsReparseSpan(data []byte, pathOffset int, offset uint16, length uint16) (string, error) {
	if offset%2 != 0 || length%2 != 0 {
		return "", windowsNativeUnsupported(windowsNativePhaseRead, "reparse UTF-16 span is unaligned", nil)
	}
	start := pathOffset + int(offset)
	end := start + int(length)
	if start < pathOffset || end < start || end > len(data) {
		return "", windowsNativeUnsupported(windowsNativePhaseRead, "reparse UTF-16 span is outside the record", nil)
	}
	units := make([]uint16, int(length)/2)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(data[start+index*2:])
	}
	value, err := decodeWindowsUTF16Units(units)
	if err != nil {
		return "", windowsNativeUnsupported(windowsNativePhaseRead, "reparse UTF-16 target is malformed", err)
	}
	return value, nil
}
