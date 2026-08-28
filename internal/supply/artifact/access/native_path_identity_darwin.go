//go:build darwin

package access

import (
	"golang.org/x/sys/unix"
)

func nativePathComponentIdentityForFD(
	fd int,
	entry nativeEntry,
) (nativePathComponentIdentity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nativePathComponentIdentity{}, err
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return nativePathComponentIdentity{}, err
	}
	return nativePathComponentIdentity{
		device:          entry.identity.device,
		inode:           entry.identity.inode,
		kind:            entry.identity.mode & unix.S_IFMT,
		generation:      uint64(stat.Gen),
		birthTimeSecond: stat.Btim.Sec,
		birthTimeNano:   stat.Btim.Nsec,
		mount: nativeMountIdentity{
			first:  uint64(uint32(filesystem.Fsid.Val[0])),
			second: uint64(uint32(filesystem.Fsid.Val[1])),
		},
	}, nil
}
