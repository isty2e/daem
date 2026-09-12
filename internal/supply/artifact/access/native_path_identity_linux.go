//go:build linux

package access

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

func nativePathComponentIdentityForFD(
	fd int,
	entry nativeEntry,
) (nativePathComponentIdentity, error) {
	var stat unix.Statx_t
	err := unix.Statx(
		fd,
		"",
		unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW,
		unix.STATX_BTIME|unix.STATX_MNT_ID,
		&stat,
	)
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
		return nativePathComponentIdentity{}, errors.Join(
			ErrNoFollowTraversalUnavailable,
			fmt.Errorf("Linux artifact mount identity is unavailable: %w", err),
		)
	}
	if err != nil {
		return nativePathComponentIdentity{}, err
	}
	var handle *unix.FileHandle
	if stat.Mask&unix.STATX_BTIME == 0 {
		// Comparing opaque handles needs no privilege to reopen by handle.
		observed, _, captureErr := unix.NameToHandleAt(fd, "", unix.AT_EMPTY_PATH)
		if captureErr != nil {
			return nativePathComponentIdentity{}, errors.Join(
				ErrNoFollowTraversalUnavailable,
				fmt.Errorf("Linux artifact file-handle identity is unavailable: %w", captureErr),
			)
		}
		handle = &observed
	}
	return nativePathComponentIdentityFromStatx(entry, stat, handle)
}

func nativePathComponentIdentityFromStatx(
	entry nativeEntry,
	stat unix.Statx_t,
	handle *unix.FileHandle,
) (nativePathComponentIdentity, error) {
	if stat.Mask&unix.STATX_MNT_ID == 0 {
		return nativePathComponentIdentity{}, errors.Join(
			ErrNoFollowTraversalUnavailable,
			fmt.Errorf("Linux artifact mount identity is unavailable"),
		)
	}
	identity := nativePathComponentIdentity{
		device: entry.identity.device,
		inode:  entry.identity.inode,
		kind:   entry.identity.mode & unix.S_IFMT,
		mount:  nativeMountIdentity{first: stat.Mnt_id},
	}
	if stat.Mask&unix.STATX_BTIME != 0 {
		identity.birthTimeSecond = stat.Btime.Sec
		identity.birthTimeNano = int64(stat.Btime.Nsec)
	} else if handle != nil && handle.Size() != 0 {
		digest := sha256.New()
		var kind [4]byte
		binary.BigEndian.PutUint32(kind[:], uint32(handle.Type()))
		_, _ = digest.Write(kind[:])
		_, _ = digest.Write(handle.Bytes())
		copy(identity.fileHandle[:], digest.Sum(nil))
	} else {
		return nativePathComponentIdentity{}, errors.Join(
			ErrNoFollowTraversalUnavailable,
			fmt.Errorf("Linux artifact birth-time and file-handle identity are unavailable"),
		)
	}
	return identity, nil
}
