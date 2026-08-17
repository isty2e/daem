//go:build !darwin && !linux

package codexplugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.Join(unixNotDirectoryError(), file.Close())
	}

	const batchMaximum = 256
	names := make([]string, 0, min(maximumEntries+1, batchMaximum))
	var readErr error
	for {
		if err := ctx.Err(); err != nil {
			readErr = err
			break
		}
		remaining := maximumEntries + 1 - len(names)
		if remaining <= 0 {
			break
		}
		batch, err := file.Readdirnames(min(batchMaximum, remaining))
		names = append(names, batch...)
		if err := ctx.Err(); err != nil {
			readErr = err
			break
		}
		if len(names) > maximumEntries {
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

func unixNotDirectoryError() error {
	return errors.New("not a directory")
}

func directoryPathBlocked(err error) bool {
	return false
}
