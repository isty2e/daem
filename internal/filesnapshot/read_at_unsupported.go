//go:build !darwin && !linux

package filesnapshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func readRegularFileAtCounted(
	ctx context.Context,
	dir *os.File,
	name string,
	maximumBytes int64,
) (CountedContent, error) {
	if dir == nil {
		return CountedContent{}, fmt.Errorf("file snapshot directory descriptor is required")
	}
	if err := validDirentName(name); err != nil {
		return CountedContent{}, err
	}
	path := dir.Name()
	if path == "" {
		return CountedContent{}, fmt.Errorf("file snapshot directory descriptor has no path")
	}
	return ReadRegularFileContextCounted(ctx, filepath.Join(path, name), maximumBytes)
}
