//go:build darwin

package commit

import "golang.org/x/sys/unix"

func renameNoReplace(fromFD int, from string, toFD int, to string) error {
	err := unix.RenameatxNp(fromFD, from, toFD, to, unix.RENAME_EXCL)
	return unsupportedOperationError("no-replace rename is unavailable", err)
}
