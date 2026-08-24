//go:build windows

package commit

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

type windowsDirectoryEntry struct {
	name       string
	identity   windowsEntryIdentityNative
	attributes uint32
	reparseTag uint32
	eaSize     uint32
}

const windowsExtendedDirectoryInfoHeaderSize = 88

func enumerateWindowsDirectoryOnce(
	ctx context.Context,
	handle windows.Handle,
	maximumEntries int,
) ([]windowsDirectoryEntry, error) {
	if ctx == nil {
		return nil, fmt.Errorf("Windows directory enumeration context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maximumEntries <= 0 {
		return nil, fmt.Errorf("Windows directory entry limit must be positive")
	}
	if _, err := queryWindowsVolumeFactsNative(handle); err != nil {
		return nil, err
	}
	parentID, err := queryWindowsFileID128(handle)
	if err != nil {
		return nil, err
	}
	entries := make([]windowsDirectoryEntry, 0)
	class := uint32(windows.FileIdExtdDirectoryRestartInfo)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		buffer, queryErr := queryWindowsDirectoryInfo(handle, class)
		class = windows.FileIdExtdDirectoryInfo
		if queryErr != nil {
			if windowsDirectoryEnd(queryErr) {
				break
			}
			return nil, queryErr
		}
		batch, parseErr := parseWindowsExtendedDirectoryInformation(buffer)
		if parseErr != nil {
			return nil, parseErr
		}
		if len(batch) == 0 {
			break
		}
		for index := range batch {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			batch[index].identity.volumeSerial = parentID.volumeSerial
			if !batch[index].identity.valid() {
				return nil, windowsNativeUnsupported(windowsNativePhaseEnumerate, "directory entry lacks stable FILE_ID_128 identity", nil)
			}
			if len(entries) == maximumEntries {
				return nil, fmt.Errorf("Windows directory exceeds %d entries", maximumEntries)
			}
			entries = append(entries, batch[index])
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func queryWindowsDirectoryInfo(handle windows.Handle, class uint32) ([]byte, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return nil, fmt.Errorf("Windows directory handle is required")
	}
	for size := 64 * 1024; size <= 1<<20; size *= 2 {
		buffer := make([]byte, size)
		err := windows.GetFileInformationByHandleEx(handle, class, &buffer[0], uint32(len(buffer)))
		if err == nil {
			return buffer, nil
		}
		if errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || errors.Is(err, windows.ERROR_MORE_DATA) {
			continue
		}
		return nil, normalizeWindowsNativeError(windowsNativePhaseEnumerate, err, false)
	}
	return nil, windowsNativeUnsupported(windowsNativePhaseEnumerate, "directory information exceeds the bounded buffer", nil)
}

func windowsDirectoryEnd(err error) bool {
	return errors.Is(err, windows.ERROR_NO_MORE_FILES) || errors.Is(err, windows.ERROR_HANDLE_EOF)
}

func parseWindowsExtendedDirectoryInformation(buffer []byte) ([]windowsDirectoryEntry, error) {
	entries := make([]windowsDirectoryEntry, 0)
	for offset := 0; offset < len(buffer); {
		remaining := len(buffer) - offset
		if remaining == 0 || allZeroBytes(buffer[offset:]) {
			break
		}
		if remaining < windowsExtendedDirectoryInfoHeaderSize {
			return nil, fmt.Errorf("FILE_ID_EXTD_DIR_INFO header is truncated")
		}
		next := binary.LittleEndian.Uint32(buffer[offset : offset+4])
		creationTime := int64(binary.LittleEndian.Uint64(buffer[offset+8 : offset+16]))
		changeTime := int64(binary.LittleEndian.Uint64(buffer[offset+32 : offset+40]))
		attributes := binary.LittleEndian.Uint32(buffer[offset+56 : offset+60])
		nameLength := binary.LittleEndian.Uint32(buffer[offset+60 : offset+64])
		eaSize := binary.LittleEndian.Uint32(buffer[offset+64 : offset+68])
		reparseTag := binary.LittleEndian.Uint32(buffer[offset+68 : offset+72])
		var fileID [16]byte
		copy(fileID[:], buffer[offset+72:offset+88])
		if nameLength%2 != 0 {
			return nil, fmt.Errorf("directory entry name is not UTF-16 aligned")
		}
		nameStart := offset + windowsExtendedDirectoryInfoHeaderSize
		nameEnd := nameStart + int(nameLength)
		if nameEnd < nameStart || nameEnd > len(buffer) {
			return nil, fmt.Errorf("directory entry name exceeds FILE_ID_EXTD_DIR_INFO")
		}
		units := make([]uint16, int(nameLength)/2)
		for index := range units {
			units[index] = binary.LittleEndian.Uint16(buffer[nameStart+index*2:])
		}
		name, err := decodeWindowsUTF16Units(units)
		if err != nil {
			return nil, err
		}
		if name != "." && name != ".." {
			if err := validateWindowsComponentName(name); err != nil {
				return nil, windowsNativeUnsupported(windowsNativePhaseEnumerate, "directory entry name is not safely representable", err)
			}
			entries = append(entries, windowsDirectoryEntry{
				name: name,
				identity: windowsEntryIdentityNative{
					volumeSerial: 0,
					fileID:       fileID,
					creationTime: creationTime,
					changeTime:   changeTime,
				},
				attributes: attributes,
				reparseTag: reparseTag,
				eaSize:     eaSize,
			})
		}
		if next == 0 {
			break
		}
		if next < uint32(nameEnd-offset) || int(next) > remaining {
			return nil, fmt.Errorf("directory entry offset is invalid")
		}
		offset += int(next)
	}
	return entries, nil
}
