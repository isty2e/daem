//go:build linux

package rootedpath

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

func nativeObjectToken(fd int, device uint64, inode uint64) (identityToken, error) {
	stat, err := statxDescriptor(fd, unix.STATX_BTIME|unix.STATX_MNT_ID)
	if err != nil && !errors.Is(err, errMountIdentityUnsupported) {
		return identityToken{}, err
	}
	var birthSecond uint64
	var birthNano uint64
	if err == nil && stat.Mask&unix.STATX_BTIME != 0 {
		birthSecond = uint64(stat.Btime.Sec)
		birthNano = uint64(stat.Btime.Nsec)
	}
	return identityTokenFromValues(
		"linux-rooted-path-object-v1",
		device,
		inode,
		birthSecond,
		birthNano,
	), nil
}

func nativeMountToken(fd int) (identityToken, error) {
	stat, err := statxDescriptor(fd, unix.STATX_MNT_ID)
	if err != nil {
		return identityToken{}, err
	}
	if stat.Mask&unix.STATX_MNT_ID == 0 {
		return identityToken{}, errMountIdentityUnsupported
	}
	return identityTokenFromValues("linux-rooted-path-mount-v1", stat.Mnt_id), nil
}

func nativeRecoveryMountToken(fd int) (identityToken, error) {
	stat, err := statxDescriptor(fd, unix.STATX_MNT_ID_UNIQUE)
	if err != nil {
		return identityToken{}, err
	}
	mountID, err := linuxUniqueMountID(stat)
	if err != nil {
		return identityToken{}, err
	}
	bootID, err := currentLinuxBootID()
	if err != nil {
		return identityToken{}, fmt.Errorf("observe Linux boot identity: %w", err)
	}
	return linuxRecoveryMountToken(mountID, bootID), nil
}

func linuxUniqueMountID(stat unix.Statx_t) (uint64, error) {
	if stat.Mask&unix.STATX_MNT_ID_UNIQUE == 0 {
		return 0, errMountIdentityUnsupported
	}
	return stat.Mnt_id, nil
}

func linuxRecoveryMountToken(mountID uint64, bootID linuxBootID) identityToken {
	return identityTokenFromValues(
		"linux-rooted-path-recovery-mount-v1",
		mountID,
		bootID.high,
		bootID.low,
	)
}

func statxDescriptor(fd int, mask int) (unix.Statx_t, error) {
	var stat unix.Statx_t
	err := unix.Statx(fd, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, mask, &stat)
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
		return unix.Statx_t{}, errMountIdentityUnsupported
	}
	return stat, err
}
