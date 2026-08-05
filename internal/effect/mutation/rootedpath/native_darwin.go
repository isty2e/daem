//go:build darwin

package rootedpath

import "golang.org/x/sys/unix"

func nativeObjectToken(fd int, device uint64, inode uint64) (identityToken, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return identityToken{}, err
	}
	return identityTokenFromValues(
		"darwin-rooted-path-object-v1",
		device,
		inode,
		uint64(stat.Gen),
		uint64(stat.Btim.Sec),
		uint64(stat.Btim.Nsec),
	), nil
}

func nativeMountToken(fd int) (identityToken, error) {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(fd, &stat); err != nil {
		return identityToken{}, err
	}
	return identityTokenFromValues(
		"darwin-rooted-path-mount-v1",
		uint64(uint32(stat.Fsid.Val[0])),
		uint64(uint32(stat.Fsid.Val[1])),
	), nil
}

func nativeRecoveryMountToken(fd int) (identityToken, error) {
	return nativeMountToken(fd)
}
