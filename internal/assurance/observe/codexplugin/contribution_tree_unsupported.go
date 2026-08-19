//go:build !darwin && !linux

package codexplugin

import (
	"errors"
	"os"
)

func openChildDirectoryNoFollow(parent *os.File, name string) (*os.File, error) {
	if parent == nil {
		return nil, errors.New("Codex plugin directory descriptor is required")
	}
	if !validDirentComponent(name) {
		return nil, errors.New("not a directory")
	}
	return nil, errDescriptorRelativeTreeUnsupported
}

func classifyChild(parent *os.File, name string) (childKind, error) {
	if parent == nil {
		return childMissing, errors.New("Codex plugin directory descriptor is required")
	}
	if !validDirentComponent(name) {
		return childSymlink, errors.New("not a directory")
	}
	return childMissing, errDescriptorRelativeTreeUnsupported
}
