//go:build darwin

package commit

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

func syncPayload(fd int) error {
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_FULLFSYNC, 0); err != nil {
		return unsupportedOperationError("F_FULLFSYNC is unavailable", err)
	}
	return nil
}

func syncDirectory(fd int) error {
	if err := unix.Fsync(fd); err != nil {
		return unsupportedOperationError("directory fsync is unavailable", err)
	}
	return nil
}

func capturePreservedMetadata(fd int, stat *unix.Stat_t) (preservedMetadata, error) {
	if stat.Flags != 0 {
		return preservedMetadata{}, unsupported(fmt.Sprintf("file flags 0x%x cannot be preserved", stat.Flags), nil)
	}
	hasACL, err := hasDarwinACL(fd)
	if err != nil {
		return preservedMetadata{}, unsupported("ACL and extended security metadata cannot be inspected", err)
	}
	if hasACL {
		return preservedMetadata{}, unsupported("ACL or extended security metadata cannot be preserved", nil)
	}
	metadata, err := captureXattrs(fd)
	metadata.replacement = true
	return metadata, err
}

func applyPreservedMetadata(fd int, metadata preservedMetadata) error {
	if err := applyXattrs(fd, metadata); err != nil {
		return err
	}
	return verifyPreservedMetadata(fd, metadata)
}

func verifyPreservedMetadata(fd int, metadata preservedMetadata) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Flags != 0 {
		return unsupported(fmt.Sprintf("file flags 0x%x cannot be preserved", stat.Flags), nil)
	}
	hasACL, err := hasDarwinACL(fd)
	if err != nil {
		return unsupported("ACL and extended security metadata cannot be inspected", err)
	}
	if hasACL {
		return unsupported("ACL or extended security metadata cannot be preserved", nil)
	}
	return verifyXattrs(fd, metadata, func(name string) bool {
		return isAllowedPreparedTreeXattr(name)
	})
}

func isAllowedPreparedTreeXattr(name string) bool {
	return name == "com.apple.provenance"
}

func hasDarwinACL(fd int) (bool, error) {
	attributes := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_RETURNED_ATTRS | unix.ATTR_CMN_EXTENDED_SECURITY,
	}
	buffer := make([]byte, 16*1024)
	_, _, errno := unix.Syscall6(
		unix.SYS_FGETATTRLIST,
		uintptr(fd),
		uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		uintptr(unix.FSOPT_ATTR_CMN_EXTENDED),
		0,
	)
	if errno != 0 {
		return false, errno
	}
	if len(buffer) < 32 {
		return false, fmt.Errorf("fgetattrlist returned a truncated attribute header")
	}
	total := int(binary.LittleEndian.Uint32(buffer[:4]))
	if total < 32 || total > len(buffer) {
		return false, fmt.Errorf("fgetattrlist returned invalid length %d", total)
	}
	returnedCommon := binary.LittleEndian.Uint32(buffer[4:8])
	if returnedCommon&unix.ATTR_CMN_EXTENDED_SECURITY == 0 {
		return false, nil
	}

	const attributeReferenceOffset = 24
	relative := int(int32(binary.LittleEndian.Uint32(buffer[attributeReferenceOffset : attributeReferenceOffset+4])))
	length := int(binary.LittleEndian.Uint32(buffer[attributeReferenceOffset+4 : attributeReferenceOffset+8]))
	start := attributeReferenceOffset + relative
	if relative < 0 || length < 44 || start < 0 || start+length > total {
		return false, fmt.Errorf("fgetattrlist returned invalid extended-security reference")
	}
	const entryCountOffset = 36
	entryCount := binary.LittleEndian.Uint32(buffer[start+entryCountOffset : start+entryCountOffset+4])
	return entryCount != ^uint32(0), nil
}
