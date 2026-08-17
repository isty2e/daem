//go:build !darwin && !linux

package filesnapshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func readRegularFileAt(
	ctx context.Context,
	dir *os.File,
	name string,
	maximumBytes int64,
) (content []byte, exists bool, err error) {
	if dir == nil {
		return nil, false, fmt.Errorf("file snapshot directory descriptor is required")
	}
	if err := validDirentName(name); err != nil {
		return nil, false, err
	}
	path := dir.Name()
	if path == "" {
		return nil, false, fmt.Errorf("file snapshot directory descriptor has no path")
	}
	return ReadRegularFileContext(ctx, filepath.Join(path, name), maximumBytes)
}
