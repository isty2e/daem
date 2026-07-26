//go:build linux

package commit

import "golang.org/x/sys/unix"

func renameNoReplace(fromFD int, from string, toFD int, to string) error {
	err := unix.Renameat2(fromFD, from, toFD, to, unix.RENAME_NOREPLACE)
	return unsupportedOperationError("no-replace rename is unavailable", err)
}
