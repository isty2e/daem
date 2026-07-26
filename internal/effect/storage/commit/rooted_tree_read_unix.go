//go:build darwin || linux

package commit

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

// SnapshotRootedDirectory streams one identity-stable, no-follow directory
// snapshot through a destination-bound capability. It does not consume the
// capability; the caller must close it on every exit.
func SnapshotRootedDirectory(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	sink mutationfs.RootedTreeSnapshotSink,
) (EntryIdentity, error) {
	path, err := rootedCapabilityPath(capability)
	if err != nil {
		return EntryIdentity{}, err
	}
	if sink == nil {
		return EntryIdentity{}, newFailure(
			failureUncommitted,
			phaseValidate,
			path,
			fmt.Errorf("rooted tree snapshot sink is required"),
		)
	}
	if err := ctx.Err(); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseValidate, path, err)
	}

	anchor, err := openCommitParent(path, capability, false)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	expected, stat, err := anchor.observe(anchor.base, path)
	if err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, err)
	}
	if expected.kind == entryKindSymlink {
		return EntryIdentity{}, failureBeforeVisibility(phaseCaptureIdentity, path, rootedFinalSymlinkFailure(path))
	}
	if expected.kind != entryKindDirectory {
		return EntryIdentity{}, failureBeforeVisibility(
			phaseValidate,
			path,
			fmt.Errorf("entry is not a directory"),
		)
	}
	if err := validateOwnedStat(path, &stat); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	directoryFD, _, err := anchor.openExpected(anchor.base, path, expected)
	if err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	defer unix.Close(directoryFD)
	if err := capability.ValidateDirectoryHandle(uintptr(directoryFD)); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseValidate, path, err)
	}
	if err := sink.VisitRoot(fs.FileMode(stat.Mode).Perm()); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseReadPayload, path, err)
	}
	if err := snapshotRootedDirectoryEntries(ctx, capability, sink, directoryFD, path, nil); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseReadPayload, path, err)
	}
	if err := verifySnapshotEntry(anchor.parentFD(), anchor.base, directoryFD, path, expected); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	if err := anchor.verifyChain(); err != nil {
		return EntryIdentity{}, failureBeforeVisibility(phaseRevalidateEntry, path, err)
	}
	return expected, nil
}

func snapshotRootedDirectoryEntries(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	sink mutationfs.RootedTreeSnapshotSink,
	directoryFD int,
	directoryPath string,
	components []string,
) error {
	names, err := readDirectoryNames(directoryFD, directoryPath)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		entryPath := filepath.Join(directoryPath, name)
		identity, stat, err := observeAt(directoryFD, name, entryPath)
		if err != nil {
			return err
		}
		if err := validateOwnedStat(entryPath, &stat); err != nil {
			return err
		}
		relative, err := mutationfs.NewTreeRelativePath(append(append([]string(nil), components...), name)...)
		if err != nil {
			return err
		}

		switch identity.kind {
		case entryKindDirectory:
			entryFD, err := openExpectedAt(directoryFD, name, entryPath, identity)
			if err != nil {
				return err
			}
			if err := capability.ValidateDirectoryHandle(uintptr(entryFD)); err != nil {
				_ = unix.Close(entryFD)
				return err
			}
			if err := sink.VisitDirectory(relative, fs.FileMode(stat.Mode).Perm()); err != nil {
				_ = unix.Close(entryFD)
				return err
			}
			err = snapshotRootedDirectoryEntries(
				ctx,
				capability,
				sink,
				entryFD,
				entryPath,
				append(append([]string(nil), components...), name),
			)
			if err == nil {
				err = verifySnapshotEntry(directoryFD, name, entryFD, entryPath, identity)
			}
			closeErr := unix.Close(entryFD)
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		case entryKindRegular:
			entryFD, err := openExpectedAt(directoryFD, name, entryPath, identity)
			if err != nil {
				return err
			}
			if err := capability.ValidateDirectoryHandle(uintptr(entryFD)); err != nil {
				_ = unix.Close(entryFD)
				return err
			}
			reader := &rootedSnapshotFileReader{ctx: ctx, fd: entryFD}
			err = sink.VisitRegularFile(
				relative,
				fs.FileMode(stat.Mode).Perm(),
				stat.Size,
				reader,
			)
			if err == nil && reader.count != stat.Size {
				err = fmt.Errorf(
					"rooted tree snapshot sink consumed %d of %d bytes for %q",
					reader.count,
					stat.Size,
					entryPath,
				)
			}
			if err == nil && !reader.eof {
				var extra [1]byte
				count, readErr := reader.Read(extra[:])
				if count != 0 || readErr != io.EOF {
					if readErr == nil {
						readErr = fmt.Errorf("content exceeds observed size %d", stat.Size)
					}
					err = readErr
				}
			}
			if err == nil {
				err = verifySnapshotEntry(directoryFD, name, entryFD, entryPath, identity)
			}
			closeErr := unix.Close(entryFD)
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return unsupported(fmt.Sprintf("rooted tree contains unsupported entry %q", entryPath), nil)
		}
	}
	return nil
}

func verifySnapshotEntry(parentFD int, name string, entryFD int, path string, expected EntryIdentity) error {
	var opened unix.Stat_t
	if err := unix.Fstat(entryFD, &opened); err != nil {
		return err
	}
	if err := validateOwnedStat(path, &opened); err != nil {
		return err
	}
	if !expected.sameEntry(identityFromStat(path, &opened)) {
		return fmt.Errorf("entry identity changed while snapshotting %q", path)
	}
	observed, _, err := observeAt(parentFD, name, path)
	if err != nil {
		return err
	}
	if !expected.sameEntry(observed) {
		return fmt.Errorf("entry binding changed while snapshotting %q", path)
	}
	return nil
}

type rootedSnapshotFileReader struct {
	ctx   context.Context
	fd    int
	count int64
	eof   bool
}

func (reader *rootedSnapshotFileReader) Read(payload []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	if len(payload) == 0 {
		return 0, nil
	}
	for {
		count, err := unix.Read(reader.fd, payload)
		if err == unix.EINTR {
			continue
		}
		reader.count += int64(count)
		if count == 0 && err == nil {
			reader.eof = true
			return 0, io.EOF
		}
		return count, err
	}
}
