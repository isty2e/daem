//go:build windows

package commit

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsVolumeNameDOS  = 0
	windowsVolumeNameGUID = 1
)

type windowsFileID128 struct {
	volumeSerial uint64
	fileID       [16]byte
}

type windowsEntryIdentityNative struct {
	volumeSerial uint64
	fileID       [16]byte
	creationTime int64
	changeTime   int64
}

func (identity windowsEntryIdentityNative) valid() bool {
	return identity.volumeSerial != 0 && identity.fileID != [16]byte{} && identity.creationTime != 0 && identity.changeTime != 0
}

func (identity windowsEntryIdentityNative) equal(other windowsEntryIdentityNative) bool {
	return identity.valid() && other.valid() && identity == other
}

func (identity windowsEntryIdentityNative) sameObject(other windowsEntryIdentityNative) bool {
	return identity.valid() && other.valid() && identity.volumeSerial == other.volumeSerial &&
		identity.fileID == other.fileID && identity.creationTime == other.creationTime
}

type windowsVolumeFactsNative struct {
	serial                uint32
	guid                  string
	filesystem            string
	maximumComponentUTF16 uint32
	fixed                 bool
	persistentACLs        bool
	readOnly              bool
}

type windowsBasicFacts struct {
	creationTime   int64
	lastAccessTime int64
	lastWriteTime  int64
	changeTime     int64
	attributes     uint32
}

type windowsStandardFacts struct {
	allocationSize int64
	endOfFile      int64
	numberOfLinks  uint32
	deletePending  bool
	directory      bool
}

type windowsAttributeTagFacts struct {
	attributes uint32
	reparseTag uint32
}

type windowsEntryFactsNative struct {
	identity  windowsEntryIdentityNative
	volume    windowsVolumeFactsNative
	basic     windowsBasicFacts
	standard  windowsStandardFacts
	attribute windowsAttributeTagFacts
}

func queryWindowsFileID128(handle windows.Handle) (windowsFileID128, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return windowsFileID128{}, fmt.Errorf("Windows entry handle is required")
	}
	var info struct {
		VolumeSerialNumber uint64
		FileID             [16]byte
	}
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		return windowsFileID128{}, normalizeWindowsNativeError(windowsNativePhaseIdentity, err, false)
	}
	if info.VolumeSerialNumber == 0 || info.FileID == [16]byte{} {
		return windowsFileID128{}, windowsNativeUnsupported(
			windowsNativePhaseIdentity,
			"FILE_ID_128 and volume serial are required",
			nil,
		)
	}
	return windowsFileID128{volumeSerial: info.VolumeSerialNumber, fileID: info.FileID}, nil
}

func queryWindowsBasicFacts(handle windows.Handle) (windowsBasicFacts, error) {
	buffer, err := queryWindowsHandleInfo(handle, windows.FileBasicInfo, 40, windowsNativePhaseIdentity)
	if err != nil {
		return windowsBasicFacts{}, err
	}
	if len(buffer) < 40 {
		return windowsBasicFacts{}, windowsNativeUnsupported(windowsNativePhaseIdentity, "FILE_BASIC_INFO is truncated", nil)
	}
	return windowsBasicFacts{
		creationTime:   int64(binary.LittleEndian.Uint64(buffer[0:8])),
		lastAccessTime: int64(binary.LittleEndian.Uint64(buffer[8:16])),
		lastWriteTime:  int64(binary.LittleEndian.Uint64(buffer[16:24])),
		changeTime:     int64(binary.LittleEndian.Uint64(buffer[24:32])),
		attributes:     binary.LittleEndian.Uint32(buffer[32:36]),
	}, nil
}

func queryWindowsStandardFacts(handle windows.Handle) (windowsStandardFacts, error) {
	buffer, err := queryWindowsHandleInfo(handle, windows.FileStandardInfo, 24, windowsNativePhaseIdentity)
	if err != nil {
		return windowsStandardFacts{}, err
	}
	if len(buffer) < 24 {
		return windowsStandardFacts{}, windowsNativeUnsupported(windowsNativePhaseIdentity, "FILE_STANDARD_INFO is truncated", nil)
	}
	return windowsStandardFacts{
		allocationSize: int64(binary.LittleEndian.Uint64(buffer[0:8])),
		endOfFile:      int64(binary.LittleEndian.Uint64(buffer[8:16])),
		numberOfLinks:  binary.LittleEndian.Uint32(buffer[16:20]),
		deletePending:  buffer[20] != 0,
		directory:      buffer[21] != 0,
	}, nil
}

func queryWindowsAttributeTagFacts(handle windows.Handle) (windowsAttributeTagFacts, error) {
	buffer, err := queryWindowsHandleInfo(handle, windows.FileAttributeTagInfo, 8, windowsNativePhaseIdentity)
	if err != nil {
		return windowsAttributeTagFacts{}, err
	}
	if len(buffer) < 8 {
		return windowsAttributeTagFacts{}, windowsNativeUnsupported(windowsNativePhaseIdentity, "FILE_ATTRIBUTE_TAG_INFO is truncated", nil)
	}
	return windowsAttributeTagFacts{
		attributes: binary.LittleEndian.Uint32(buffer[0:4]),
		reparseTag: binary.LittleEndian.Uint32(buffer[4:8]),
	}, nil
}

func queryWindowsEntryFacts(handle windows.Handle) (windowsEntryFactsNative, error) {
	volume, err := queryWindowsVolumeFactsNative(handle)
	if err != nil {
		return windowsEntryFactsNative{}, err
	}
	basic, err := queryWindowsBasicFacts(handle)
	if err != nil {
		return windowsEntryFactsNative{}, err
	}
	standard, err := queryWindowsStandardFacts(handle)
	if err != nil {
		return windowsEntryFactsNative{}, err
	}
	attribute, err := queryWindowsAttributeTagFacts(handle)
	if err != nil {
		return windowsEntryFactsNative{}, err
	}
	fileID, err := queryWindowsFileID128(handle)
	if err != nil {
		return windowsEntryFactsNative{}, err
	}
	if fileID.volumeSerial != uint64(volume.serial) {
		return windowsEntryFactsNative{}, windowsNativeUnsupported(
			windowsNativePhaseIdentity,
			"entry and volume serial evidence disagree",
			nil,
		)
	}
	identity := windowsEntryIdentityNative{
		volumeSerial: fileID.volumeSerial,
		fileID:       fileID.fileID,
		creationTime: basic.creationTime,
		changeTime:   basic.changeTime,
	}
	if !identity.valid() {
		return windowsEntryFactsNative{}, windowsNativeUnsupported(windowsNativePhaseIdentity, "entry identity is incomplete", nil)
	}
	return windowsEntryFactsNative{
		identity:  identity,
		volume:    volume,
		basic:     basic,
		standard:  standard,
		attribute: attribute,
	}, nil
}

func queryWindowsVolumeFactsNative(handle windows.Handle) (windowsVolumeFactsNative, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return windowsVolumeFactsNative{}, fmt.Errorf("Windows volume handle is required")
	}
	var (
		volumeName [256]uint16
		filesystem [64]uint16
		serial     uint32
		maximum    uint32
		flags      uint32
	)
	if err := windows.GetVolumeInformationByHandle(
		handle,
		&volumeName[0],
		uint32(len(volumeName)),
		&serial,
		&maximum,
		&flags,
		&filesystem[0],
		uint32(len(filesystem)),
	); err != nil {
		return windowsVolumeFactsNative{}, normalizeWindowsNativeError(windowsNativePhaseIdentity, err, false)
	}
	filesystemName := windows.UTF16ToString(filesystem[:])
	if serial == 0 || maximum == 0 || !strings.EqualFold(filesystemName, "NTFS") ||
		flags&windows.FILE_PERSISTENT_ACLS == 0 || flags&windows.FILE_READ_ONLY_VOLUME != 0 {
		return windowsVolumeFactsNative{}, windowsNativeUnsupported(
			windowsNativePhaseIdentity,
			"volume is not a writable fixed NTFS volume with persistent ACLs",
			nil,
		)
	}
	dosPath, err := windowsHandlePathNative(handle, windowsVolumeNameDOS)
	if err != nil {
		return windowsVolumeFactsNative{}, normalizeWindowsNativeError(windowsNativePhaseIdentity, err, false)
	}
	if windowsRemotePathNative(dosPath) {
		return windowsVolumeFactsNative{}, windowsNativeUnsupported(windowsNativePhaseIdentity, "remote volume is not admitted", nil)
	}
	guidPath, err := windowsHandlePathNative(handle, windowsVolumeNameGUID)
	if err != nil {
		return windowsVolumeFactsNative{}, normalizeWindowsNativeError(windowsNativePhaseIdentity, err, false)
	}
	guid, err := parseWindowsVolumeGUID(guidPath)
	if err != nil {
		return windowsVolumeFactsNative{}, err
	}
	rootPath := windowsVolumeRootNative(dosPath, guidPath)
	if rootPath == "" {
		return windowsVolumeFactsNative{}, windowsNativeUnsupported(windowsNativePhaseIdentity, "fixed volume root cannot be established", nil)
	}
	rootName, err := windows.UTF16PtrFromString(rootPath)
	if err != nil {
		return windowsVolumeFactsNative{}, windowsNativeUnsupported(windowsNativePhaseIdentity, "fixed volume root cannot be encoded", err)
	}
	fixed := windows.GetDriveType(rootName) == windows.DRIVE_FIXED
	if !fixed {
		return windowsVolumeFactsNative{}, windowsNativeUnsupported(windowsNativePhaseIdentity, "volume is not fixed", nil)
	}
	return windowsVolumeFactsNative{
		serial:                serial,
		guid:                  strings.ToLower(guid),
		filesystem:            strings.ToUpper(filesystemName),
		maximumComponentUTF16: maximum,
		fixed:                 fixed,
		persistentACLs:        flags&windows.FILE_PERSISTENT_ACLS != 0,
		readOnly:              flags&windows.FILE_READ_ONLY_VOLUME != 0,
	}, nil
}

func windowsHandlePathNative(handle windows.Handle, flags uint32) (string, error) {
	for size := uint32(256); size <= 1<<15; size *= 2 {
		buffer := make([]uint16, size)
		length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], size, flags)
		if err != nil {
			return "", err
		}
		if length < size {
			return windows.UTF16ToString(buffer[:length]), nil
		}
	}
	return "", fmt.Errorf("Windows handle path exceeds 32 KiB")
}

func parseWindowsVolumeGUID(value string) (string, error) {
	upper := strings.ToUpper(value)
	const prefix = `\\?\VOLUME{`
	if !strings.HasPrefix(upper, prefix) {
		return "", windowsNativeUnsupported(windowsNativePhaseIdentity, "volume GUID path is unavailable", nil)
	}
	end := strings.IndexByte(value[len(prefix):], '}')
	if end < 0 {
		return "", windowsNativeUnsupported(windowsNativePhaseIdentity, "volume GUID path is malformed", nil)
	}
	guid := value[len(prefix)-1 : len(prefix)+end+1]
	if !validWindowsVolumeGUID(guid) {
		return "", windowsNativeUnsupported(windowsNativePhaseIdentity, "volume GUID is malformed", nil)
	}
	return strings.ToLower(guid), nil
}

func validWindowsVolumeGUID(value string) bool {
	if len(value) != 38 || value[0] != '{' || value[37] != '}' {
		return false
	}
	for index := 0; index < 36; index++ {
		character := value[index+1]
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func normalizeWindowsFinalPathNative(value string) string {
	if strings.HasPrefix(strings.ToUpper(value), `\\?\`) {
		return value[4:]
	}
	return value
}

func windowsRemotePathNative(value string) bool {
	upper := strings.ToUpper(value)
	if strings.HasPrefix(upper, `\\?\UNC\`) || strings.HasPrefix(upper, `\\?\GLOBALROOT\`) {
		return true
	}
	if strings.HasPrefix(upper, `\\?\`) {
		return false
	}
	return strings.HasPrefix(value, `\\`) || strings.HasPrefix(upper, `\DEVICE\`)
}

func windowsVolumeRootNative(dosPath string, guidPath string) string {
	normalized := normalizeWindowsFinalPathNative(dosPath)
	if len(normalized) >= 3 && ((normalized[0] >= 'A' && normalized[0] <= 'Z') || (normalized[0] >= 'a' && normalized[0] <= 'z')) &&
		normalized[1] == ':' && normalized[2] == '\\' {
		return normalized[:3]
	}
	upper := strings.ToUpper(guidPath)
	const prefix = `\\?\VOLUME{`
	if !strings.HasPrefix(upper, prefix) {
		return ""
	}
	end := strings.IndexByte(guidPath[len(prefix):], '}')
	if end < 0 {
		return ""
	}
	return guidPath[:len(prefix)+end+1] + `\`
}

func queryWindowsHandleInfo(
	handle windows.Handle,
	class uint32,
	initialSize int,
	phase windowsNativePhase,
) ([]byte, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return nil, fmt.Errorf("Windows handle is required")
	}
	if initialSize < 1 {
		initialSize = 256
	}
	for size := initialSize; size <= 1<<20; size *= 2 {
		buffer := make([]byte, size)
		err := windows.GetFileInformationByHandleEx(handle, class, &buffer[0], uint32(len(buffer)))
		if err == nil {
			return buffer, nil
		}
		if errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || errors.Is(err, windows.ERROR_MORE_DATA) {
			continue
		}
		if errors.Is(err, windows.ERROR_HANDLE_EOF) && class == windows.FileStreamInfo {
			return buffer[:0], nil
		}
		return nil, normalizeWindowsNativeError(phase, err, false)
	}
	return nil, windowsNativeUnsupported(phase, "Windows file-information response exceeds the bounded buffer", nil)
}

func decodeWindowsUTF16Units(units []uint16) (string, error) {
	for index, unit := range units {
		switch {
		case unit >= 0xd800 && unit <= 0xdbff:
			if index+1 >= len(units) || units[index+1] < 0xdc00 || units[index+1] > 0xdfff {
				return "", fmt.Errorf("Windows UTF-16 contains an unpaired high surrogate")
			}
		case unit >= 0xdc00 && unit <= 0xdfff:
			if index == 0 || units[index-1] < 0xd800 || units[index-1] > 0xdbff {
				return "", fmt.Errorf("Windows UTF-16 contains an unpaired low surrogate")
			}
		}
	}
	value := string(utf16.Decode(units))
	if strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("Windows UTF-16 contains NUL")
	}
	return value, nil
}

func equalWindowsVolumeFacts(left, right windowsVolumeFactsNative) bool {
	return left.serial == right.serial && strings.EqualFold(left.guid, right.guid) &&
		strings.EqualFold(left.filesystem, right.filesystem) &&
		left.maximumComponentUTF16 == right.maximumComponentUTF16 && left.fixed == right.fixed &&
		left.persistentACLs == right.persistentACLs && left.readOnly == right.readOnly
}

const windowsAllowedStructuralAttributes = windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_ATTRIBUTE_ARCHIVE | windows.FILE_ATTRIBUTE_DIRECTORY

func windowsUnsupportedAttributes(attributes uint32) uint32 {
	return attributes &^ windowsAllowedStructuralAttributes
}

func validateWindowsObservedEntryAttributes(attributes uint32, directory bool) error {
	if unsupported := windowsUnsupportedAttributes(attributes); unsupported != 0 {
		return windowsNativeUnsupported(windowsNativePhaseMetadata, fmt.Sprintf("unsupported file attributes 0x%x", unsupported), nil)
	}
	if directory {
		if attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || attributes&windows.FILE_ATTRIBUTE_NORMAL != 0 {
			return windowsNativeUnsupported(windowsNativePhaseMetadata, "directory attributes are not canonical structural attributes", nil)
		}
		return nil
	}
	if attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 ||
		attributes&windows.FILE_ATTRIBUTE_NORMAL != 0 && attributes&windows.FILE_ATTRIBUTE_ARCHIVE != 0 ||
		attributes&windows.FILE_ATTRIBUTE_NORMAL == 0 && attributes&windows.FILE_ATTRIBUTE_ARCHIVE == 0 {
		return windowsNativeUnsupported(windowsNativePhaseMetadata, "file attributes are not canonical structural attributes", nil)
	}
	return nil
}

func validateWindowsCanonicalEntryAttributes(attributes uint32, directory bool) error {
	return validateWindowsObservedEntryAttributes(attributes, directory)
}

func ensureWindowsEntryMetadataSupported(facts windowsEntryFactsNative) error {
	return validateWindowsCanonicalEntryAttributes(facts.attribute.attributes, facts.standard.directory)
}

func bytesEqualWindowsID(left, right windowsFileID128) bool {
	return left.volumeSerial == right.volumeSerial && bytes.Equal(left.fileID[:], right.fileID[:])
}
