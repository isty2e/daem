//go:build darwin || linux

package commit

import (
	"fmt"
	"strings"

	"golang.org/x/sys/unix"
)

type preservedMetadata struct {
	xattrs      map[string][]byte
	replacement bool
}

func captureXattrs(fd int) (preservedMetadata, error) {
	metadata := preservedMetadata{xattrs: make(map[string][]byte)}
	names, err := listXattrNames(fd)
	if err != nil {
		return preservedMetadata{}, err
	}
	for _, name := range names {
		value, err := readXattrValue(fd, name)
		if err != nil {
			return preservedMetadata{}, err
		}
		metadata.xattrs[name] = value
	}
	return metadata, nil
}

func listXattrNames(fd int) ([]string, error) {
	size, err := unix.Flistxattr(fd, nil)
	if err != nil {
		return nil, unsupported("extended attributes cannot be inspected", err)
	}
	if size == 0 {
		return nil, nil
	}
	buffer := make([]byte, size)
	written, err := unix.Flistxattr(fd, buffer)
	if err != nil {
		return nil, unsupported("extended attributes cannot be inspected", err)
	}
	names := make([]string, 0)
	for name := range strings.SplitSeq(string(buffer[:written]), "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func readXattrValue(fd int, name string) ([]byte, error) {
	size, err := unix.Fgetxattr(fd, name, nil)
	if err != nil {
		return nil, unsupported(fmt.Sprintf("extended attribute %q cannot be read", name), err)
	}
	value := make([]byte, size)
	if size == 0 {
		return value, nil
	}
	written, err := unix.Fgetxattr(fd, name, value)
	if err != nil {
		return nil, unsupported(fmt.Sprintf("extended attribute %q cannot be read", name), err)
	}
	return value[:written], nil
}

func applyXattrs(fd int, metadata preservedMetadata) error {
	for name, value := range metadata.xattrs {
		if err := unix.Fsetxattr(fd, name, value, 0); err != nil {
			return unsupported(fmt.Sprintf("extended attribute %q cannot be preserved", name), err)
		}
	}
	return nil
}

func verifyXattrNames(names []string, metadata preservedMetadata) error {
	observed := make(map[string]struct{}, len(names))
	for _, name := range names {
		observed[name] = struct{}{}
	}
	for name := range metadata.xattrs {
		if _, exists := observed[name]; !exists {
			return unsupported(fmt.Sprintf("extended attribute %q did not verify", name), nil)
		}
	}
	if metadata.replacement {
		for name := range observed {
			if _, expected := metadata.xattrs[name]; !expected {
				return unsupported(fmt.Sprintf("unexpected extended attribute %q on replacement", name), nil)
			}
		}
	}
	return nil
}
