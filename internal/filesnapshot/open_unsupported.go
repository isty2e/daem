//go:build !darwin && !linux && !windows

package filesnapshot

import "os"

func openRegularFile(path string, _ bool) (*os.File, error) {
	return os.Open(path)
}
