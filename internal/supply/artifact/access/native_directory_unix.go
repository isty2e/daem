//go:build darwin || linux

package access

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"sort"

	"golang.org/x/sys/unix"
)

func readNativeDirectoryNames(directoryFD int) ([]string, error) {
	return readNativeDirectoryNamesUpTo(directoryFD, -1)
}

func readNativeDirectoryNamesWithinBudget(
	directoryFD int,
	relativeRoot string,
	budget *traversalBudget,
) ([]string, error) {
	remaining, bounded := budget.structureEntriesRemaining()
	if !bounded {
		return readNativeDirectoryNames(directoryFD)
	}
	names, err := readNativeDirectoryNamesUpTo(directoryFD, remaining)
	if err != nil {
		return nil, err
	}
	if len(names) > remaining {
		return nil, fmt.Errorf(
			"artifact tree exceeds %d entries below %q",
			budget.structureLimit.maximumEntries,
			relativeRoot,
		)
	}
	return names, nil
}

func readNativeDirectoryNamesUpTo(directoryFD int, maximumEntries int) ([]string, error) {
	readFD, err := unix.Openat(directoryFD, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(readFD), "artifact-directory")
	if file == nil {
		_ = unix.Close(readFD)
		return nil, fmt.Errorf("wrap artifact directory descriptor")
	}
	readCount := -1
	if maximumEntries >= 0 && maximumEntries < math.MaxInt {
		readCount = maximumEntries + 1
	}
	names, readErr := file.Readdirnames(readCount)
	closeErr := file.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, errors.Join(readErr, closeErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sort.Strings(names)
	return names, nil
}

func nativeDirectoryContainsExactName(directoryFD int, name string) (bool, error) {
	names, err := readNativeDirectoryNames(directoryFD)
	if err != nil {
		return false, err
	}
	index := sort.SearchStrings(names, name)
	return index < len(names) && names[index] == name, nil
}

func verifyNativeDirectoryNames(directoryFD int, expected []string) error {
	actual, err := readNativeDirectoryNamesUpTo(directoryFD, len(expected))
	if err != nil {
		return err
	}
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("artifact access directory entries changed while open")
	}
	return nil
}
