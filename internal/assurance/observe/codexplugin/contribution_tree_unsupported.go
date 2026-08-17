//go:build !darwin && !linux

package codexplugin

import (
	"errors"
	"os"
	"path/filepath"
)

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

func openChildDirectoryNoFollow(parent *os.File, name string) (*os.File, error) {
	if parent == nil {
		return nil, errors.New("Codex plugin directory descriptor is required")
	}
	if !validDirentComponent(name) {
		return nil, errors.New("not a directory")
	}
	return openDirectoryNoFollow(filepath.Join(parent.Name(), name))
}

func classifyChild(parent *os.File, name string) (childKind, error) {
	if parent == nil {
		return childMissing, errors.New("Codex plugin directory descriptor is required")
	}
	if !validDirentComponent(name) {
		return childSymlink, errors.New("not a directory")
	}
	info, err := os.Lstat(filepath.Join(parent.Name(), name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return childMissing, nil
		}
		return childMissing, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return childSymlink, nil
	}
	if info.IsDir() {
		return childDirectory, nil
	}
	if info.Mode().IsRegular() {
		return childFile, nil
	}
	return childOther, nil
}

func directoryPathBlocked(err error) bool {
	return false
}
