//go:build windows

package commit

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsStreamFacts struct {
	namedStreams bool
	streams      []string
}

type windowsExtendedAttributeFacts struct {
	size uint32
}

type windowsSecurityFacts struct {
	ownerSID               string
	groupSID               string
	daclSDDL               string
	selfRelativeDescriptor []byte
	control                windows.SECURITY_DESCRIPTOR_CONTROL
	daclPresent            bool
	daclNull               bool
	daclDefaulted          bool
	ownerDefaulted         bool
	groupDefaulted         bool
	dacl                   windowsDACLFact
}

type windowsMetadataFacts struct {
	streams  windowsStreamFacts
	ea       windowsExtendedAttributeFacts
	security windowsSecurityFacts
}

func queryWindowsStreamFacts(handle windows.Handle) (windowsStreamFacts, error) {
	buffer, err := queryWindowsHandleInfo(handle, windows.FileStreamInfo, 256, windowsNativePhaseMetadata)
	if err != nil {
		return windowsStreamFacts{}, err
	}
	facts := windowsStreamFacts{}
	for offset := 0; offset < len(buffer); {
		remaining := len(buffer) - offset
		if remaining == 0 || offset > 0 && allZeroBytes(buffer[offset:]) {
			break
		}
		if remaining < 24 {
			return windowsStreamFacts{}, windowsNativeUnsupported(windowsNativePhaseMetadata, "FILE_STREAM_INFORMATION is truncated", nil)
		}
		next := binary.LittleEndian.Uint32(buffer[offset : offset+4])
		nameLength := binary.LittleEndian.Uint32(buffer[offset+4 : offset+8])
		if nameLength%2 != 0 {
			return windowsStreamFacts{}, windowsNativeUnsupported(windowsNativePhaseMetadata, "stream name is not UTF-16 aligned", nil)
		}
		nameStart := offset + 24
		nameEnd := nameStart + int(nameLength)
		if nameStart < offset || nameEnd < nameStart || nameEnd > len(buffer) {
			return windowsStreamFacts{}, windowsNativeUnsupported(windowsNativePhaseMetadata, "stream name exceeds FILE_STREAM_INFORMATION", nil)
		}
		units := make([]uint16, int(nameLength)/2)
		for index := range units {
			units[index] = binary.LittleEndian.Uint16(buffer[nameStart+index*2:])
		}
		name, decodeErr := decodeWindowsUTF16Units(units)
		if decodeErr != nil {
			return windowsStreamFacts{}, windowsNativeUnsupported(windowsNativePhaseMetadata, "stream name is malformed", decodeErr)
		}
		if name != "::$DATA" {
			facts.namedStreams = true
		}
		facts.streams = append(facts.streams, name)
		if next == 0 {
			break
		}
		if next < uint32(nameEnd-offset) || int(next) > remaining {
			return windowsStreamFacts{}, windowsNativeUnsupported(windowsNativePhaseMetadata, "stream entry offset is invalid", nil)
		}
		offset += int(next)
	}
	return facts, nil
}

var windowsNtQueryInformationFile = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQueryInformationFile")

const windowsFileEaInformationClass = 7

func queryWindowsExtendedAttributeFacts(handle windows.Handle) (windowsExtendedAttributeFacts, error) {
	if handle == 0 || handle == windows.InvalidHandle {
		return windowsExtendedAttributeFacts{}, fmt.Errorf("Windows EA handle is required")
	}
	var (
		statusBlock windows.IO_STATUS_BLOCK
		buffer      [4]byte
	)
	result, _, _ := windowsNtQueryInformationFile.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&statusBlock)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		uintptr(windowsFileEaInformationClass),
	)
	if result != 0 {
		return windowsExtendedAttributeFacts{}, normalizeWindowsNativeError(
			windowsNativePhaseMetadata,
			windows.NTStatus(result),
			false,
		)
	}
	return windowsExtendedAttributeFacts{size: binary.LittleEndian.Uint32(buffer[:])}, nil
}

func queryWindowsMetadataFacts(handle windows.Handle) (windowsMetadataFacts, error) {
	streams, err := queryWindowsStreamFacts(handle)
	if err != nil {
		return windowsMetadataFacts{}, err
	}
	ea, err := queryWindowsExtendedAttributeFacts(handle)
	if err != nil {
		return windowsMetadataFacts{}, err
	}
	security, err := queryWindowsSecurityFacts(handle)
	if err != nil {
		return windowsMetadataFacts{}, err
	}
	return windowsMetadataFacts{streams: streams, ea: ea, security: security}, nil
}

func ensureWindowsMetadataSupported(metadata windowsMetadataFacts) error {
	return validateWindowsObservedMetadata(metadata)
}

func ensureWindowsCanonicalMetadataSupported(
	metadata windowsMetadataFacts,
	expected windowsSecurityFacts,
) error {
	if err := validateWindowsObservedMetadata(metadata); err != nil {
		return err
	}
	return validateWindowsCanonicalSecurityFacts(metadata.security, expected)
}

func validateWindowsObservedMetadata(metadata windowsMetadataFacts) error {
	if metadata.streams.namedStreams {
		return windowsNativeUnsupported(windowsNativePhaseMetadata, "alternate data streams are not supported", nil)
	}
	if metadata.ea.size != 0 {
		return windowsNativeUnsupported(windowsNativePhaseMetadata, "extended attributes are not supported", nil)
	}
	return validateWindowsObservedSecurityFacts(metadata.security)
}

func queryWindowsSecurityFacts(handle windows.Handle) (windowsSecurityFacts, error) {
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return windowsSecurityFacts{}, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	return windowsSecurityFactsFromDescriptor(descriptor)
}

func windowsSecurityFactsFromDescriptor(descriptor *windows.SECURITY_DESCRIPTOR) (windowsSecurityFacts, error) {
	if descriptor == nil || !descriptor.IsValid() {
		return windowsSecurityFacts{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "security descriptor is unavailable", nil)
	}
	owner, ownerDefaulted, err := descriptor.Owner()
	if err != nil {
		return windowsSecurityFacts{}, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	group, groupDefaulted, err := descriptor.Group()
	if err != nil {
		return windowsSecurityFacts{}, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	dacl, daclDefaulted, daclErr := descriptor.DACL()
	if daclErr != nil && !errors.Is(daclErr, windows.ERROR_OBJECT_NOT_FOUND) {
		return windowsSecurityFacts{}, normalizeWindowsNativeError(windowsNativePhaseSecurity, daclErr, false)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return windowsSecurityFacts{}, normalizeWindowsNativeError(windowsNativePhaseSecurity, err, false)
	}
	ownerSID := ""
	if owner != nil && owner.IsValid() {
		ownerSID = owner.String()
	}
	groupSID := ""
	if group != nil && group.IsValid() {
		groupSID = group.String()
	}
	if ownerSID == "" || groupSID == "" {
		return windowsSecurityFacts{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "owner and group SIDs are required", nil)
	}
	canonical := descriptor.String()
	if canonical == "" || strings.HasPrefix(canonical, "<nil>") {
		return windowsSecurityFacts{}, windowsNativeUnsupported(windowsNativePhaseSecurity, "security descriptor cannot be canonicalized", nil)
	}
	selfRelative, err := copyWindowsSelfRelativeDescriptor(descriptor)
	if err != nil {
		return windowsSecurityFacts{}, err
	}
	present := daclErr == nil
	facts := windowsSecurityFacts{
		ownerSID:               ownerSID,
		groupSID:               groupSID,
		daclSDDL:               canonical,
		selfRelativeDescriptor: selfRelative,
		control:                control & windowsSecurityControlMask,
		daclPresent:            present,
		daclNull:               present && dacl == nil,
		daclDefaulted:          daclDefaulted,
		ownerDefaulted:         ownerDefaulted,
		groupDefaulted:         groupDefaulted,
	}
	if dacl != nil {
		facts.dacl, err = parseWindowsDACLFact(dacl)
		if err != nil {
			return windowsSecurityFacts{}, err
		}
	}
	return facts, nil
}

func (facts windowsSecurityFacts) equal(other windowsSecurityFacts) bool {
	return facts.ownerSID == other.ownerSID && facts.groupSID == other.groupSID &&
		facts.control == other.control && facts.daclPresent == other.daclPresent &&
		facts.daclNull == other.daclNull && facts.daclDefaulted == other.daclDefaulted &&
		facts.ownerDefaulted == other.ownerDefaulted && facts.groupDefaulted == other.groupDefaulted &&
		facts.dacl.equal(other.dacl)
}

func compareWindowsSecurityFacts(left, right windowsSecurityFacts) bool {
	return left.equal(right)
}

func parseWindowsEAInformation(buffer []byte) (windowsExtendedAttributeFacts, error) {
	if len(buffer) < 4 {
		return windowsExtendedAttributeFacts{}, fmt.Errorf("FILE_EA_INFORMATION is truncated")
	}
	return windowsExtendedAttributeFacts{size: binary.LittleEndian.Uint32(buffer[:4])}, nil
}

func parseWindowsStreamInformation(buffer []byte) (windowsStreamFacts, error) {
	facts := windowsStreamFacts{}
	for offset := 0; offset < len(buffer); {
		remaining := len(buffer) - offset
		if remaining == 0 || offset > 0 && allZeroBytes(buffer[offset:]) {
			break
		}
		if remaining < 24 {
			return windowsStreamFacts{}, fmt.Errorf("FILE_STREAM_INFORMATION is truncated")
		}
		next := binary.LittleEndian.Uint32(buffer[offset : offset+4])
		nameLength := binary.LittleEndian.Uint32(buffer[offset+4 : offset+8])
		if nameLength%2 != 0 {
			return windowsStreamFacts{}, fmt.Errorf("stream name is not UTF-16 aligned")
		}
		nameStart := offset + 24
		nameEnd := nameStart + int(nameLength)
		if nameEnd < nameStart || nameEnd > len(buffer) {
			return windowsStreamFacts{}, fmt.Errorf("stream name exceeds its record")
		}
		units := make([]uint16, int(nameLength)/2)
		for index := range units {
			units[index] = binary.LittleEndian.Uint16(buffer[nameStart+index*2:])
		}
		name, err := decodeWindowsUTF16Units(units)
		if err != nil {
			return windowsStreamFacts{}, err
		}
		facts.streams = append(facts.streams, name)
		if name != "::$DATA" {
			facts.namedStreams = true
		}
		if next == 0 {
			break
		}
		if next < uint32(nameEnd-offset) || int(next) > remaining {
			return windowsStreamFacts{}, fmt.Errorf("stream entry offset is invalid")
		}
		offset += int(next)
	}
	return facts, nil
}

func allZeroBytes(buffer []byte) bool {
	if len(buffer) == 0 {
		return false
	}
	for _, value := range buffer {
		if value != 0 {
			return false
		}
	}
	return true
}
