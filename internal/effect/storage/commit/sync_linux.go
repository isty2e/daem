//go:build linux

package commit

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	linuxUserModifiableFlags = 0x000380ff
	linuxNoDump              = 0x40  // FS_NODUMP_FL
	linuxNoCompress          = 0x400 // FS_NOCOMP_FL, also set by Btrfs on incompressible files
	linuxSELinuxLabel        = "security.selinux"
)

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
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return unsupportedOperationError("directory descriptor flags are unavailable", err)
	}
	syncFD := fd
	if flags&unix.O_PATH != 0 {
		syncFD, err = unix.Openat(
			fd,
			".",
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if err != nil {
			return unsupportedOperationError("search-only directory cannot be reopened for sync", err)
		}
		defer unix.Close(syncFD)
	}
	if err := unix.Fsync(syncFD); err != nil {
		return unsupportedOperationError("directory fsync is unavailable", err)
	}
	return nil
}

func capturePreservedMetadata(fd int, _ *unix.Stat_t, path string) (preservedMetadata, error) {
	flags, err := linuxFileFlags(fd)
	if err != nil {
		return preservedMetadata{}, unsupported("file flags cannot be inspected", err)
	}
	if flags&linuxUserModifiableFlags&^linuxNoDump != 0 {
		return preservedMetadata{}, unsupported(fmt.Sprintf("file flags 0x%x cannot be preserved", flags), nil)
	}
	names, err := listXattrNames(fd)
	if err != nil {
		return preservedMetadata{}, err
	}
	metadata := preservedMetadata{existingPath: path, noDump: flags&linuxNoDump != 0, xattrs: make(map[string][]byte)}
	metadata.selinuxLabel, err = readSELinuxLabel(fd)
	if err != nil {
		return preservedMetadata{}, &SELinuxLabelError{Destination: path, Err: err}
	}
	for _, name := range names {
		if err := admitLinuxAccess(name); err != nil {
			return preservedMetadata{}, err
		}
		if name == linuxSELinuxLabel || linuxContentSignature(name) {
			continue
		}
		value, err := readXattrValue(fd, name)
		if err != nil {
			return preservedMetadata{}, err
		}
		metadata.xattrs[name] = value
	}
	return metadata, nil
}

func readSELinuxLabel(fd int) ([]byte, error) {
	label, err := readXattrValue(fd, linuxSELinuxLabel)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.EOPNOTSUPP) {
		return nil, nil
	}
	return label, err
}

func (metadata preservedMetadata) matchesSELinuxLabel(fd int) (bool, error) {
	current, err := readSELinuxLabel(fd)
	if err != nil {
		return false, &SELinuxLabelError{Destination: metadata.existingPath, Err: err}
	}
	return (current == nil) == (metadata.selinuxLabel == nil) && bytes.Equal(current, metadata.selinuxLabel), nil
}

func (metadata preservedMetadata) verifySELinuxLabel(fd int) error {
	if matches, err := metadata.matchesSELinuxLabel(fd); err != nil || matches {
		return err
	}
	return &SELinuxLabelError{Destination: metadata.existingPath, Err: unsupported("replacement label differs from the original", nil)}
}

func prepareReplacementAccess(fd int, metadata preservedMetadata) error {
	if matches, err := metadata.matchesSELinuxLabel(fd); err != nil || matches {
		return err
	}
	if metadata.selinuxLabel == nil {
		return &SELinuxLabelError{Destination: metadata.existingPath, Err: unsupported("cannot remove the kernel-assigned label", nil)}
	}
	if err := unix.Fsetxattr(fd, linuxSELinuxLabel, metadata.selinuxLabel, 0); err != nil {
		return &SELinuxLabelError{Destination: metadata.existingPath, Err: unsupported("cannot label the empty replacement", err)}
	}
	return metadata.verifySELinuxLabel(fd)
}

func applyPreservedMetadata(fd int, metadata preservedMetadata) error {
	for name, value := range metadata.xattrs {
		if linuxAccessLabel(name) {
			continue // Other access labels must match the OS-assigned values.
		}
		if err := unix.Fsetxattr(fd, name, value, 0); err != nil {
			return unsupported(fmt.Sprintf("extended attribute %q cannot be preserved", name), err)
		}
	}
	if metadata.noDump {
		flags, err := linuxFileFlags(fd)
		if err != nil {
			return err
		}
		if flags&linuxNoDump == 0 {
			if err := unix.IoctlSetPointerInt(fd, unix.FS_IOC_SETFLAGS, flags|linuxNoDump); err != nil {
				return unsupported("backup-exclusion flag cannot be preserved", err)
			}
		}
	}
	return verifyPreservedMetadata(fd, metadata)
}

func verifyPreservedMetadata(fd int, metadata preservedMetadata) error {
	flags, err := linuxFileFlags(fd)
	if err != nil {
		return unsupported("file flags cannot be inspected", err)
	}
	if flags&linuxUserModifiableFlags&^linuxNoDump != 0 || (metadata.noDump && flags&linuxNoDump == 0) {
		return unsupported(fmt.Sprintf("file flags 0x%x cannot be preserved", flags), nil)
	}
	names, err := listXattrNames(fd)
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(names))
	for _, name := range names {
		present[name] = true
		if err := admitLinuxAccess(name); err != nil {
			return err
		}
		if _, known := metadata.xattrs[name]; !known && metadata.existingPath != "" && name != linuxSELinuxLabel && linuxAccessLabel(name) {
			return unsupported(fmt.Sprintf("replacement has a different access label %q", name), nil)
		}
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	for name, expected := range metadata.xattrs {
		if !present[name] {
			return unsupported(fmt.Sprintf("extended attribute %q is missing", name), nil)
		}
		// NON-GOAL: detecting later same-name user.* value changes without owner
		// read permission. Linux withholds these values even on the retained fd;
		// their prepared same-inode check is the value evidence. Revisit if an
		// unprivileged read or reliable change-counter API becomes available.
		if stat.Mode&0o400 == 0 && strings.HasPrefix(name, "user.") {
			continue
		}
		actual, err := readXattrValue(fd, name)
		if err != nil {
			return err
		}
		if string(actual) != string(expected) {
			return unsupported(fmt.Sprintf("extended attribute %q did not verify", name), nil)
		}
	}
	if metadata.existingPath != "" {
		return metadata.verifySELinuxLabel(fd)
	}
	return nil
}

func validateInPlacePermissionAccess(fd int, _ *unix.Stat_t, needsChmod bool) error {
	// A regular file can have an access ACL, but not a directory default ACL.
	for _, name := range []string{"system.posix_acl_access", "security.capability"} {
		_, err := unix.Fgetxattr(fd, name, nil)
		switch {
		case err == nil:
			return unsupported(fmt.Sprintf("permission bits do not describe %q", name), nil)
		case errors.Is(err, unix.ENODATA), errors.Is(err, unix.EOPNOTSUPP):
		default:
			return unsupported(fmt.Sprintf("permission attribute %q cannot be inspected", name), err)
		}
	}
	if needsChmod {
		flags, err := linuxFileFlags(fd)
		if err != nil {
			return unsupported("file flags cannot be inspected", err)
		}
		const blocksChmod = 0x10 | 0x20 // FS_IMMUTABLE_FL | FS_APPEND_FL
		if flags&blocksChmod != 0 {
			return unsupported("immutable or append-only file cannot be restricted", nil)
		}
	}
	return nil
}

func capturePreparedTreePlatformMetadataFacts(fd int, path string, _ *unix.Stat_t) (uint64, error) {
	flags, err := linuxFileFlags(fd)
	if err != nil {
		return 0, unsupported("prepared tree file flags cannot be inspected", err)
	}
	return preparedTreeLinuxFlagFacts(path, flags)
}

func preparedTreeLinuxFlagFacts(path string, flags int) (uint64, error) {
	facts := flags &^ (linuxNoDump | linuxNoCompress)
	if unsupportedFlags := facts &^ linuxPreparedTreeKernelManagedFlags; unsupportedFlags != 0 {
		return 0, unsupported(
			fmt.Sprintf("prepared tree entry %q contains unsupported file flags 0x%x", path, unsupportedFlags),
			nil,
		)
	}
	return uint64(facts), nil
}

func preparedTreeAccessLabel(name string) (bool, error) {
	return linuxAccessLabel(name), admitLinuxAccess(name)
}

func linuxAccessLabel(name string) bool {
	return !linuxContentSignature(name) && (strings.HasPrefix(name, "security.") || strings.HasPrefix(name, "system.") || strings.HasPrefix(name, "trusted."))
}

func linuxContentSignature(name string) bool {
	return name == "security.ima" || name == "security.evm"
}

func admitLinuxAccess(name string) error {
	switch name {
	case "system.posix_acl_access", "system.posix_acl_default", "security.capability":
		return unsupported(fmt.Sprintf("cannot preserve access control %q on replacement", name), nil)
	}
	return nil
}

func linuxFileFlags(fd int) (int, error) {
	flags, err := unix.IoctlGetInt(fd, unix.FS_IOC_GETFLAGS)
	if err == nil {
		return flags, nil
	}
	var filesystem unix.Statfs_t
	if statErr := unix.Fstatfs(fd, &filesystem); statErr != nil {
		return 0, errors.Join(err, statErr)
	}
	if linuxNFSFlagsUnavailable(int64(filesystem.Type), err) {
		return 0, nil
	}
	return 0, err
}

func linuxNFSFlagsUnavailable(filesystemType int64, err error) bool {
	return filesystemType == unix.NFS_SUPER_MAGIC &&
		(errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EOPNOTSUPP))
}
