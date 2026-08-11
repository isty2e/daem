//go:build darwin

package commit

import "golang.org/x/sys/unix"

func openRestrictiveRemovalDirectory(_ int, _ string) (int, error) {
	return -1, nil
}

func chmodRestrictiveRemovalDirectory(
	parentFD int,
	name string,
	_ int,
	mode uint32,
) error {
	return unix.Fchmodat(parentFD, name, mode, unix.AT_SYMLINK_NOFOLLOW)
}
