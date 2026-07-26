//go:build darwin || linux

package commit

import (
	"context"
	"fmt"
	"io/fs"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"golang.org/x/sys/unix"
)

type preparedTreeDirectoryMode struct {
	path mutationfs.TreeRelativePath
	mode fs.FileMode
}

func (writer *rootedTreeWriterUnix) SetRootMode(mode fs.FileMode) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if !writer.active || writer.prepared == nil || writer.prepared.state != preparedRootedTreeReady {
		return fmt.Errorf("rooted tree writer is no longer active")
	}
	if err := writer.ctx.Err(); err != nil {
		return err
	}
	if err := validateFileMode(mode); err != nil {
		return err
	}
	if writer.prepared.rootModeSet {
		return fmt.Errorf("rooted tree root mode is already set")
	}
	writer.prepared.rootMode = mode.Perm()
	writer.prepared.rootModeSet = true
	return nil
}

func (prepared *PreparedRootedTree) applyTreeModesLocked(ctx context.Context) error {
	writer := &rootedTreeWriterUnix{ctx: ctx, prepared: prepared, active: true}
	for index := len(prepared.directoryModes) - 1; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		directory := prepared.directoryModes[index]
		parent, err := writer.openParent(directory.path)
		if err != nil {
			return err
		}
		identity, stat, err := observeAt(parent.fd, parent.base, parent.path)
		if err == nil && identity.kind != entryKindDirectory {
			err = fmt.Errorf("prepared tree entry %q is not a directory", parent.path)
		}
		if err == nil {
			err = validateOwnedStat(parent.path, &stat)
		}
		fd := -1
		if err == nil {
			fd, err = openExpectedAt(parent.fd, parent.base, parent.path, identity)
		}
		if err == nil {
			err = prepared.anchor.capability.ValidateDirectoryHandle(uintptr(fd))
		}
		if err == nil {
			err = unix.Fchmod(fd, uint32(directory.mode.Perm()))
		}
		if err == nil {
			err = verifyTreeEntryMode(fd, parent.path, directory.mode)
		}
		if err == nil {
			err = syncDirectory(fd)
		}
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		parent.close()
		if err != nil {
			return err
		}
	}
	if err := unix.Fchmod(prepared.stageFD, uint32(prepared.rootMode.Perm())); err != nil {
		return err
	}
	return verifyTreeEntryMode(prepared.stageFD, prepared.stagePath, prepared.rootMode)
}

func verifyTreeEntryMode(fd int, path string, mode fs.FileMode) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return err
	}
	if fs.FileMode(stat.Mode).Perm() != mode.Perm() {
		return unsupported(fmt.Sprintf("prepared tree directory %q did not retain requested mode", path), nil)
	}
	return nil
}
