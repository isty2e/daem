//go:build darwin || linux

package codexplugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

func readDirectoryNamesUpTo(ctx context.Context, path string, maximumEntries int) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("Codex plugin observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maximumEntries < 0 {
		return nil, fmt.Errorf("Codex plugin directory listing budget is required")
	}

	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("wrap Codex plugin directory descriptor")
	}

	const batchMaximum = 256
	bounded := maximumEntries < math.MaxInt
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
			readErr = fmt.Errorf("Codex plugin directory enumeration made no progress")
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
	return names, nil
}

func directoryPathBlocked(err error) bool {
	return errors.Is(err, unix.ELOOP)
}
