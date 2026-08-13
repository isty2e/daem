//go:build !darwin && !linux

package filesnapshot

import "os"

func openRegularFile(path string, _ bool) (*os.File, error) {
	return os.Open(path)
}
