//go:build linux

package commit

import (
	"fmt"

	"golang.org/x/sys/unix"
)

const linuxUserModifiableFlags = 0x000380ff

func syncPayload(fd int) error {
	if err := unix.Fsync(fd); err != nil {
		return unsupportedOperationError("file fsync is unavailable", err)
	}
	return nil
}

func syncDirectory(fd int) error {
	if err := unix.Fsync(fd); err != nil {
		return unsupportedOperationError("directory fsync is unavailable", err)
	}
	return nil
}

func capturePreservedMetadata(fd int, _ *unix.Stat_t) (preservedMetadata, error) {
	flags, err := unix.IoctlGetInt(fd, unix.FS_IOC_GETFLAGS)
	if err != nil {
		return preservedMetadata{}, unsupported("file flags cannot be inspected", err)
	}
	if flags&linuxUserModifiableFlags != 0 {
		return preservedMetadata{}, unsupported(fmt.Sprintf("file flags 0x%x cannot be preserved", flags), nil)
	}
	metadata, err := captureXattrs(fd)
	if err != nil {
		return preservedMetadata{}, err
	}
	for name := range metadata.xattrs {
		if isLinuxACL(name) {
			return preservedMetadata{}, unsupported("POSIX ACL metadata cannot be preserved", nil)
		}
	}
	metadata.replacement = true
	return metadata, nil
}

func applyPreservedMetadata(fd int, metadata preservedMetadata) error {
	if err := applyXattrs(fd, metadata); err != nil {
		return err
	}
	return verifyPreservedMetadata(fd, metadata)
}

func verifyPreservedMetadata(fd int, metadata preservedMetadata) error {
	flags, err := unix.IoctlGetInt(fd, unix.FS_IOC_GETFLAGS)
	if err != nil {
		return unsupported("file flags cannot be inspected", err)
	}
	if flags&linuxUserModifiableFlags != 0 {
		return unsupported(fmt.Sprintf("file flags 0x%x cannot be preserved", flags), nil)
	}
	observed, err := captureXattrs(fd)
	if err != nil {
		return err
	}
	for name := range observed.xattrs {
		if isLinuxACL(name) {
			return unsupported("POSIX ACL metadata cannot be preserved", nil)
		}
	}
	return verifyObservedXattrs(observed, metadata, func(string) bool { return false })
}

func isLinuxACL(name string) bool {
	return name == "system.posix_acl_access" || name == "system.posix_acl_default"
}
