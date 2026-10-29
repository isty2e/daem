package fields

import (
	"context"
	"errors"

	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
)

// DecodeAdmitted selects canonical field mapping without choosing unknown-key policy.
func DecodeAdmitted(ctx context.Context, content []byte, target any) (tomlsyntax.Metadata, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	metadata, err := tomlsyntax.DecodeAdmitted(ctx, content, target)
	if err != nil {
		return metadata, err
	}
	undecoded, err := classifyKeys(ctx, metadata, target)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return tomlsyntax.Metadata{}, err
	}
	if err != nil {
		return metadata, err
	}
	metadata = metadata.WithUndecoded(undecoded)
	if err := ctx.Err(); err != nil {
		return tomlsyntax.Metadata{}, err
	}
	return metadata, nil
}
