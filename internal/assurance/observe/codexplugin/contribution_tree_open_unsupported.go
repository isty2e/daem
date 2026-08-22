//go:build !darwin && !linux && !windows

package codexplugin

import (
	"errors"
	"os"
)

func openDirectory(path string) (*os.File, error) {
	return openDirectoryNoFollow(path)
}

func openDirectoryNoFollow(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !info.IsDir() {
		return nil, errors.Join(errors.New("not a directory"), file.Close())
	}
	return file, nil
}

func directoryPathBlocked(err error) bool {
	return false
}
