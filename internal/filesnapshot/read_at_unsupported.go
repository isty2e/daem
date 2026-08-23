//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !windows

package filesnapshot

import (
	"context"
	"fmt"
	"os"
)

func readRegularFileAtCounted(
	ctx context.Context,
	dir *os.File,
	name string,
	maximumBytes int64,
) (CountedContent, error) {
	if ctx == nil {
		return CountedContent{}, fmt.Errorf("file snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return CountedContent{}, err
	}
	if dir == nil {
		return CountedContent{}, fmt.Errorf("file snapshot directory descriptor is required")
	}
	if maximumBytes <= 0 {
		return CountedContent{}, fmt.Errorf("maximum file size must be positive")
	}
	if err := validDirentName(name); err != nil {
		return CountedContent{}, err
	}
	return CountedContent{}, ErrUnsupported
}
