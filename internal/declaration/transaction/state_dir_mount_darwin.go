//go:build darwin

package transaction

import (
	"context"
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
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return "", "", err
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return "", "", err
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	mount := fmt.Sprintf(
		"darwin-mount:%d:%d",
		uint32(filesystem.Fsid.Val[0]),
		uint32(filesystem.Fsid.Val[1]),
	)
	incarnation := fmt.Sprintf(
		"darwin-incarnation:%d:%d:%d",
		stat.Gen,
		stat.Btim.Sec,
		stat.Btim.Nsec,
	)
	return mount, incarnation, nil
}
