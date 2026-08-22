//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !windows

package filesnapshot

import "os"

func openEntryAt(*os.File, string) (*os.File, error) {
	return nil, ErrUnsupported
}
