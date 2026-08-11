//go:build linux

package commit

import (
	"fmt"

	"golang.org/x/sys/unix"
)

const linuxUserModifiableFlags = 0x000380ff

const linuxPreparedTreeKernelManagedFlags = 0x00000100 | // FS_DIRTY_FL
	0x00000200 | // FS_COMPRBLK_FL
	0x00001000 | // FS_INDEX_FL
	0x00002000 | // FS_IMAGIC_FL
	0x00040000 | // FS_HUGE_FILE_FL
	0x00080000 | // FS_EXTENT_FL
	0x00200000 | // FS_EA_INODE_FL
	0x00400000 | // FS_EOFBLOCKS_FL
	0x10000000 // FS_INLINE_DATA_FL

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

func capturePreparedTreePlatformMetadataFacts(fd int, path string, _ *unix.Stat_t) (uint64, error) {
	flags, err := unix.IoctlGetInt(fd, unix.FS_IOC_GETFLAGS)
	if err != nil {
		return 0, unsupported("prepared tree file flags cannot be inspected", err)
	}
	if err := validatePreparedTreeLinuxFlags(path, flags); err != nil {
		return 0, err
	}
	return uint64(flags), nil
}

func validatePreparedTreeLinuxFlags(path string, flags int) error {
	if unsupportedFlags := flags &^ linuxPreparedTreeKernelManagedFlags; unsupportedFlags != 0 {
		return unsupported(
			fmt.Sprintf("prepared tree entry %q contains unsupported file flags 0x%x", path, unsupportedFlags),
			nil,
		)
	}
	return nil
}

func isAllowedPreparedTreeXattr(string) bool {
	return false
}

func isLinuxACL(name string) bool {
	return name == "system.posix_acl_access" || name == "system.posix_acl_default"
}
