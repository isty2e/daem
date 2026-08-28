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
	if stat.Mask&unix.STATX_MNT_ID == 0 {
		return nativePathComponentIdentity{}, errors.Join(
			ErrNoFollowTraversalUnavailable,
			fmt.Errorf("Linux artifact mount identity is unavailable"),
		)
	}
	var birthTimeSecond int64
	var birthTimeNano int64
	if stat.Mask&unix.STATX_BTIME != 0 {
		birthTimeSecond = stat.Btime.Sec
		birthTimeNano = int64(stat.Btime.Nsec)
	}
	return nativePathComponentIdentity{
		device:          entry.identity.device,
		inode:           entry.identity.inode,
		kind:            entry.identity.mode & unix.S_IFMT,
		birthTimeSecond: birthTimeSecond,
		birthTimeNano:   birthTimeNano,
		mount:           nativeMountIdentity{first: stat.Mnt_id},
	}, nil
}
