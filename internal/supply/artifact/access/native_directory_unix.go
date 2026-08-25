//go:build darwin || linux

package access

import (
	"context"
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
	return readNativeDirectoryNamesUpTo(context.Background(), directoryFD, -1)
}

func visitNativeDirectoryNames(directoryFD int, visit func(string) error) error {
	readFD, err := unix.Openat(directoryFD, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(readFD), "artifact-directory")
	if file == nil {
		_ = unix.Close(readFD)
		return fmt.Errorf("wrap artifact directory descriptor")
	}

	for {
		names, readErr := file.Readdirnames(256)
		for _, name := range names {
			if err := visit(name); err != nil {
				return errors.Join(err, file.Close())
			}
		}
		if errors.Is(readErr, io.EOF) {
			return file.Close()
		}
		if readErr != nil {
			return errors.Join(readErr, file.Close())
		}
	}
}

func readNativeDirectoryNamesWithinBudget(
	ctx context.Context,
	directoryFD int,
	relativeRoot string,
	budget *traversalBudget,
) ([]string, error) {
	structureRemaining, structureBounded := budget.structureEntriesRemaining()
	traversalRemaining, traversalBounded := budget.traversalEntriesRemaining()
	maximumEntries := -1
	switch {
	case structureBounded && traversalBounded:
		maximumEntries = min(structureRemaining, traversalRemaining)
	case structureBounded:
		maximumEntries = structureRemaining
	case traversalBounded:
		maximumEntries = traversalRemaining
	}
	names, err := readNativeDirectoryNamesUpTo(ctx, directoryFD, maximumEntries)
	if err != nil {
		return nil, err
	}
	if maximumEntries >= 0 && len(names) > maximumEntries {
		budget.chargeRootListing(len(names))
		if traversalBounded && (!structureBounded || traversalRemaining < structureRemaining) {
			return nil, newTraversalEntryLimitError("traversal", relativeRoot, budget.limit.maxEntries)
		}
		return nil, fmt.Errorf(
			"artifact tree exceeds %d entries below %q",
			budget.structureLimit.maximumEntries,
			relativeRoot,
		)
	}
	return names, nil
}

func readNativeDirectoryNamesUpTo(
	ctx context.Context,
	directoryFD int,
	maximumEntries int,
) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("artifact access context is required")
	}
	readFD, err := unix.Openat(directoryFD, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(readFD), "artifact-directory")
	if file == nil {
		_ = unix.Close(readFD)
		return nil, fmt.Errorf("wrap artifact directory descriptor")
	}

	const batchMaximum = 256
	bounded := maximumEntries >= 0 && maximumEntries < math.MaxInt
	capacity := batchMaximum
	if bounded {
		capacity = min(maximumEntries+1, batchMaximum)
	}
	names := make([]string, 0, capacity)
	var readErr error
	for {
		if err := ctx.Err(); err != nil {
			readErr = err
			break
		}
		batchSize := batchMaximum
		if bounded {
			remaining := maximumEntries + 1 - len(names)
			if remaining <= 0 {
				break
			}
			batchSize = min(batchMaximum, remaining)
		}
		batch, err := file.Readdirnames(batchSize)
		names = append(names, batch...)
		if err := ctx.Err(); err != nil {
			readErr = err
			break
		}
		if bounded && len(names) > maximumEntries {
			break
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			readErr = err
			break
		}
		if len(batch) == 0 {
			readErr = fmt.Errorf("artifact directory enumeration made no progress")
			break
		}
	}
	closeErr := file.Close()
	if readErr != nil {
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

func verifyNativeDirectoryNames(ctx context.Context, directoryFD int, expected []string) error {
	actual, err := readNativeDirectoryNamesUpTo(ctx, directoryFD, len(expected))
	if err != nil {
		return err
	}
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("artifact access directory entries changed while open")
	}
	return nil
}
