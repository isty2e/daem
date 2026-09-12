//go:build linux

package commit

import (
	"fmt"
	"strconv"

	"golang.org/x/sys/unix"
)

func openRestrictiveRemovalDirectory(parentFD int, name string) (int, error) {
	return unix.Openat(
		parentFD,
		name,
		unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
}

func chmodRestrictiveCleanupEntry(
	_ int,
	_ string,
	entryFD int,
	mode uint32,
) error {
	if entryFD < 0 {
		return fmt.Errorf("restrictive cleanup entry handle is required")
	}
	procFD, err := unix.Open("/proc", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open procfs for restrictive cleanup: %w", err)
	}
	defer unix.Close(procFD)

	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(procFD, &filesystem); err != nil {
		return fmt.Errorf("inspect procfs for restrictive cleanup: %w", err)
	}
	if filesystem.Type != unix.PROC_SUPER_MAGIC {
		return fmt.Errorf("restrictive cleanup requires a verified procfs")
	}

	var opened unix.Stat_t
	if err := unix.Fstat(entryFD, &opened); err != nil {
		return fmt.Errorf("inspect restrictive cleanup entry handle: %w", err)
	}
	procPath := "self/fd/" + strconv.Itoa(entryFD)
	var throughProc unix.Stat_t
	if err := unix.Fstatat(procFD, procPath, &throughProc, 0); err != nil {
		return fmt.Errorf("inspect restrictive cleanup procfs handle: %w", err)
	}
	openedIdentity := identityFromStat(procPath, &opened)
	procIdentity := identityFromStat(procPath, &throughProc)
	if (openedIdentity.kind != entryKindDirectory && openedIdentity.kind != entryKindRegular) ||
		!openedIdentity.sameObject(procIdentity) {
		return fmt.Errorf("restrictive cleanup procfs handle changed identity")
	}
	if err := unix.Fchmodat(procFD, procPath, mode, 0); err != nil {
		return fmt.Errorf("change restrictive cleanup entry mode: %w", err)
	}
	return nil
}

func chmodPreparedTreeEntryForCleanup(parentFD int, name string, expected EntryIdentity, mode uint32) error {
	fd, err := unix.Openat(parentFD, name, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if !expected.sameEntry(identityFromStat(expected.path, &stat)) {
		return fmt.Errorf("prepared tree entry %q changed before cleanup chmod", expected.path)
	}
	// O_PATH retains even a mode-0000 entry; verified procfs avoids relying on
	// fchmodat2 or following a replaceable child pathname on older kernels.
	return chmodRestrictiveCleanupEntry(parentFD, name, fd, mode)
}
