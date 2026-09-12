//go:build linux

package commit

import (
	"errors"

	"golang.org/x/sys/unix"
)

func renameNoReplace(fromFD int, from string, toFD int, to string) error {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fromFD, &filesystem); err != nil {
		return err
	}
	err := unix.Renameat2(fromFD, from, toFD, to, unix.RENAME_NOREPLACE)
	if filesystem.Type != unix.NFS_SUPER_MAGIC {
		return unsupportedOperationError("no-replace rename is unavailable", err)
	}
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) {
		// NFS publication assumes one writer to this namespace; this absence
		// check is not atomic exclusion of concurrent external creation.
		var destination unix.Stat_t
		inspectErr := unix.Fstatat(toFD, to, &destination, unix.AT_SYMLINK_NOFOLLOW)
		if inspectErr == nil {
			return unix.EEXIST
		}
		if !errors.Is(inspectErr, unix.ENOENT) {
			return inspectErr
		}
		err = unix.Renameat(fromFD, from, toFD, to)
	}
	return nfsRenameResult(err)
}

func renameReplace(fromFD int, from string, toFD int, to string) error {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fromFD, &filesystem); err != nil {
		return err
	}
	err := unix.Renameat(fromFD, from, toFD, to)
	if filesystem.Type == unix.NFS_SUPER_MAGIC {
		return nfsRenameResult(err)
	}
	return err
}

func nfsRenameResult(err error) error {
	if err == nil {
		return nil
	}
	// An NFS server can perform a rename before a failed RPC retry is reported.
	return errors.Join(errRenameIndeterminate, err)
}
