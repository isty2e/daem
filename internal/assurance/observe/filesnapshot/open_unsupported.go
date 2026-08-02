//go:build !darwin && !linux

package filesnapshot

import "os"

func openRegularFile(path string) (*os.File, error) {
	return os.Open(path)
}
