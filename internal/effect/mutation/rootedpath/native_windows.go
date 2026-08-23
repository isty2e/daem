//go:build windows

package rootedpath

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsVolumeNameDOS  = 0
	windowsVolumeNameGUID = 1
)

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
	_              uint32
}

type windowsFileCaseSensitiveInfo struct {
	Flags uint32
}

type windowsDirectoryFacts struct {
	object                identityToken
	mount                 identityToken
	recovery              identityToken
	path                  string
	isDirectory           bool
	reparse               bool
	caseSensitive         bool
	maximumComponentUTF16 uint32
}

type windowsVolumeFacts struct {
	operation             identityToken
	recovery              identityToken
	maximumComponentUTF16 uint32
}

func queryWindowsDirectoryFacts(handle windows.Handle) (windowsDirectoryFacts, error) {
	volume, err := queryWindowsVolumeFacts(handle)
	if err != nil {
		return windowsDirectoryFacts{}, err
	}
	basic, err := queryWindowsFileBasicInfo(handle)
	if err != nil {
		return windowsDirectoryFacts{}, err
	}
	id, err := queryWindowsFileID(handle)
	if err != nil {
		return windowsDirectoryFacts{}, err
	}
	object, err := windowsObjectTokenFromInfo(id, basic.CreationTime)
	if err != nil {
		return windowsDirectoryFacts{}, err
	}
	caseSensitive := false
	isDirectory := basic.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	isReparse := basic.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	if isDirectory && !isReparse {
		caseSensitive, err = queryWindowsCaseSensitive(handle)
		if err != nil {
			return windowsDirectoryFacts{}, err
		}
	}
	path, err := windowsHandlePath(handle, windowsVolumeNameDOS)
	if err != nil {
		return windowsDirectoryFacts{}, windowsCapabilityQueryError("inspect retained Windows path", err)
	}
	return windowsDirectoryFacts{
		object:                object,
		mount:                 volume.operation,
		recovery:              volume.recovery,
		path:                  normalizeWindowsFinalPath(path),
		isDirectory:           isDirectory,
		reparse:               isReparse,
		caseSensitive:         caseSensitive,
		maximumComponentUTF16: volume.maximumComponentUTF16,
	}, nil
}

func queryWindowsFileID(handle windows.Handle) (windowsFileIDInfo, error) {
	var info windowsFileIDInfo
	err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		return windowsFileIDInfo{}, windowsCapabilityQueryError("query FILE_ID_128", err)
	}
	if info.VolumeSerialNumber == 0 || info.FileID == [16]byte{} {
		return windowsFileIDInfo{}, fmt.Errorf("%w: FILE_ID_128 and volume serial are required", errMountIdentityUnsupported)
	}
	return info, nil
}

func windowsObjectTokenFromInfo(info windowsFileIDInfo, creationTime int64) (identityToken, error) {
	if info.VolumeSerialNumber == 0 || info.FileID == [16]byte{} || creationTime == 0 {
		return identityToken{}, fmt.Errorf("%w: FILE_ID_128, volume serial, and creation time are required", errMountIdentityUnsupported)
	}
	var serial [8]byte
	binary.BigEndian.PutUint64(serial[:], info.VolumeSerialNumber)
	var creation [8]byte
	binary.BigEndian.PutUint64(creation[:], uint64(creationTime))
	return identityTokenFromBytes(
		"windows-rooted-path-object-incarnation-v1",
		info.FileID[:],
		serial[:],
		creation[:],
	), nil
}

func queryWindowsFileBasicInfo(handle windows.Handle) (windowsFileBasicInfo, error) {
	var info windowsFileBasicInfo
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileBasicInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		return windowsFileBasicInfo{}, err
	}
	return info, nil
}

func queryWindowsCaseSensitive(handle windows.Handle) (bool, error) {
	var info windowsFileCaseSensitiveInfo
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileCaseSensitiveInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		return false, windowsCapabilityQueryError("query per-directory case sensitivity", err)
	}
	return info.Flags&windows.FILE_CS_FLAG_CASE_SENSITIVE_DIR != 0, nil
}

func queryWindowsVolumeFacts(handle windows.Handle) (windowsVolumeFacts, error) {
	var (
		volumeLabel [256]uint16
		filesystem  [64]uint16
		serial      uint32
		maximum     uint32
		flags       uint32
	)
	if err := windows.GetVolumeInformationByHandle(
		handle,
		&volumeLabel[0],
		uint32(len(volumeLabel)),
		&serial,
		&maximum,
		&flags,
		&filesystem[0],
		uint32(len(filesystem)),
	); err != nil {
		return windowsVolumeFacts{}, windowsCapabilityQueryError("inspect volume information", err)
	}
	if serial == 0 || flags&windows.FILE_PERSISTENT_ACLS == 0 || flags&windows.FILE_READ_ONLY_VOLUME != 0 {
		return windowsVolumeFacts{}, fmt.Errorf("%w: volume lacks persistent writable identity", errMountIdentityUnsupported)
	}
	filesystemName := windows.UTF16ToString(filesystem[:])
	if !strings.EqualFold(filesystemName, "NTFS") {
		return windowsVolumeFacts{}, fmt.Errorf("%w: filesystem %q is not NTFS", errMountIdentityUnsupported, filesystemName)
	}

	dosPath, err := windowsHandlePath(handle, windowsVolumeNameDOS)
	if err != nil {
		return windowsVolumeFacts{}, windowsCapabilityQueryError("inspect volume DOS path", err)
	}
	if windowsRemotePath(dosPath) {
		return windowsVolumeFacts{}, fmt.Errorf("%w: remote volume is not admitted", errMountIdentityUnsupported)
	}
	guidPath, err := windowsHandlePath(handle, windowsVolumeNameGUID)
	if err != nil {
		return windowsVolumeFacts{}, windowsCapabilityQueryError("inspect stable volume GUID", err)
	}
	guid, err := windowsVolumeGUID(guidPath)
	if err != nil {
		return windowsVolumeFacts{}, err
	}
	rootPath := windowsVolumeRootPath(normalizeWindowsFinalPath(dosPath), guidPath)
	if rootPath == "" {
		return windowsVolumeFacts{}, fmt.Errorf("%w: volume root cannot be established", errMountIdentityUnsupported)
	}
	rootName, err := windows.UTF16PtrFromString(rootPath)
	if err != nil {
		return windowsVolumeFacts{}, fmt.Errorf("%w: encode volume root: %v", errMountIdentityUnsupported, err)
	}
	if driveType := windows.GetDriveType(rootName); driveType != windows.DRIVE_FIXED {
		return windowsVolumeFacts{}, fmt.Errorf("%w: drive type %d is not fixed", errMountIdentityUnsupported, driveType)
	}

	var serialBytes [4]byte
	binary.BigEndian.PutUint32(serialBytes[:], serial)
	filesystemName = strings.ToLower(filesystemName)
	operation := identityTokenFromBytes(
		"windows-rooted-path-operation-volume-v1",
		[]byte(strings.ToLower(guid)),
		serialBytes[:],
		[]byte(filesystemName),
	)
	recovery := identityTokenFromBytes(
		"windows-rooted-path-recovery-volume-v1",
		[]byte(strings.ToLower(guid)),
		serialBytes[:],
		[]byte(filesystemName),
	)
	if maximum == 0 {
		return windowsVolumeFacts{}, fmt.Errorf("%w: volume component-length evidence is unavailable", errMountIdentityUnsupported)
	}
	return windowsVolumeFacts{
		operation:             operation,
		recovery:              recovery,
		maximumComponentUTF16: maximum,
	}, nil
}

func windowsCapabilityQueryError(operation string, err error) error {
	var status windows.NTStatus
	if errors.As(err, &status) {
		err = status.Errno()
	}
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) ||
		errors.Is(err, windows.ERROR_INVALID_PARAMETER) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
		return fmt.Errorf("%w: %s: %w", errMountIdentityUnsupported, operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func nativeMountToken(handle windows.Handle) (identityToken, error) {
	facts, err := queryWindowsVolumeFacts(handle)
	if err != nil {
		return identityToken{}, err
	}
	return facts.operation, nil
}

func nativeRecoveryMountToken(handle windows.Handle) (identityToken, error) {
	facts, err := queryWindowsVolumeFacts(handle)
	if err != nil {
		return identityToken{}, err
	}
	return facts.recovery, nil
}

func nativeMountTokenAt(parent windows.Handle, name string) (identityToken, error) {
	handle, _, err := openWindowsChild(parent, name)
	if err != nil {
		return identityToken{}, err
	}
	defer windows.CloseHandle(handle)
	return nativeMountToken(handle)
}

func windowsHandlePath(handle windows.Handle, flags uint32) (string, error) {
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
	return "", errors.New("Windows handle path exceeds 32 KiB")
}

func normalizeWindowsFinalPath(value string) string {
	if strings.HasPrefix(strings.ToUpper(value), `\\?\`) {
		return value[4:]
	}
	return value
}

func windowsRemotePath(value string) bool {
	upper := strings.ToUpper(value)
	if strings.HasPrefix(upper, `\\?\UNC\`) || strings.HasPrefix(upper, `\\?\GLOBALROOT\`) {
		return true
	}
	if strings.HasPrefix(upper, `\\?\`) {
		return false
	}
	return strings.HasPrefix(upper, `\\`) || strings.HasPrefix(upper, `\DEVICE\`)
}

func windowsVolumeGUID(value string) (string, error) {
	upper := strings.ToUpper(value)
	const prefix = `\\?\VOLUME{`
	if !strings.HasPrefix(upper, prefix) {
		return "", fmt.Errorf("%w: volume GUID path is unavailable", errMountIdentityUnsupported)
	}
	end := strings.IndexByte(value[len(prefix):], '}')
	if end < 0 {
		return "", fmt.Errorf("%w: volume GUID path is malformed", errMountIdentityUnsupported)
	}
	guid := value[len(prefix)-1 : len(prefix)+end+1]
	if len(guid) != len("{00000000-0000-0000-0000-000000000000}") || !validWindowsVolumeGUID(guid) {
		return "", fmt.Errorf("%w: volume GUID is malformed", errMountIdentityUnsupported)
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

func windowsVolumeRootPath(dosPath string, guidPath string) string {
	if len(dosPath) >= 3 && isASCIIAlpha(dosPath[0]) && dosPath[1] == ':' && dosPath[2] == '\\' {
		return dosPath[:3]
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
