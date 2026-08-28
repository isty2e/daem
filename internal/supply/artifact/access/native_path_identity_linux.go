//go:build linux

package access

import (
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
	return nativePathComponentIdentityFromStatx(entry, stat)
}

func nativePathComponentIdentityFromStatx(
	entry nativeEntry,
	stat unix.Statx_t,
) (nativePathComponentIdentity, error) {
	if stat.Mask&unix.STATX_MNT_ID == 0 {
		return nativePathComponentIdentity{}, errors.Join(
			ErrNoFollowTraversalUnavailable,
			fmt.Errorf("Linux artifact mount identity is unavailable"),
		)
	}
	if stat.Mask&unix.STATX_BTIME == 0 {
		return nativePathComponentIdentity{}, errors.Join(
			ErrNoFollowTraversalUnavailable,
			fmt.Errorf("Linux artifact birth-time identity is unavailable"),
		)
	}
	return nativePathComponentIdentity{
		device:          entry.identity.device,
		inode:           entry.identity.inode,
		kind:            entry.identity.mode & unix.S_IFMT,
		birthTimeSecond: stat.Btime.Sec,
		birthTimeNano:   int64(stat.Btime.Nsec),
		mount:           nativeMountIdentity{first: stat.Mnt_id},
	}, nil
}
