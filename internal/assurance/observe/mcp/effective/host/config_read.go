package host

import (
	"context"

	"github.com/isty2e/daem/internal/filesnapshot"
)

type configFileReader func(context.Context, string, int64) ([]byte, bool, error)

func (reader configFileReader) read(ctx context.Context, path string, maximumBytes int64) ([]byte, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if reader == nil {
		reader = filesnapshot.ReadRegularFileContext
	}
	return reader(ctx, path, maximumBytes)
}
