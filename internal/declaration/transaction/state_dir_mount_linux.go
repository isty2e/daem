//go:build linux

package transaction

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

func observeStateDirPlatform(ctx context.Context, path string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", "", err
	}
	defer unix.Close(fd)
	var stat unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID|unix.STATX_BTIME, &stat); err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
			return "", "", fmt.Errorf("operation mount identity is unsupported: %w", err)
		}
		return "", "", err
	}
	if stat.Mask&unix.STATX_MNT_ID == 0 {
		return "", "", fmt.Errorf("operation mount identity is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	incarnation := ""
	if stat.Mask&unix.STATX_BTIME != 0 {
		incarnation = fmt.Sprintf("linux-birth:%d:%d", stat.Btime.Sec, stat.Btime.Nsec)
	}
	return fmt.Sprintf("linux-mount:%d", stat.Mnt_id), incarnation, nil
}
