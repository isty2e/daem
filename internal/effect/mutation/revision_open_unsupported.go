//go:build !darwin && !linux

package mutation

import "os"

func openRevisionDirectory(path string) (*os.File, error) {
	return os.Open(path)
}

func openRevisionRegularFile(path string) (*os.File, error) {
	return os.Open(path)
}
