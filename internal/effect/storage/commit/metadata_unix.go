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
	noDump      bool
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
