package lockfile

import (
	"context"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
)

// Codec validates current lock values through a caller-owned contribution catalog.
// It grants no document capability.
type Codec struct {
	contributions aggregate.ContributionCatalog
}

func NewCodec(contributions aggregate.ContributionCatalog) (Codec, error) {
	if err := contributions.Validate(); err != nil {
		return Codec{}, err
	}
	return Codec{contributions: contributions}, nil
}

func (codec Codec) Marshal(ctx context.Context, file lock.File) ([]byte, error) {
	if err := codec.contributions.Validate(); err != nil {
		return nil, err
	}
	if err := lock.Validate(file); err != nil {
		return nil, err
	}
	return marshalValidatedWithCatalog(ctx, file, codec.contributions)
}

func (codec Codec) Load(ctx context.Context, path string) (lock.File, error) {
	if err := codec.contributions.Validate(); err != nil {
		return lock.File{}, err
	}
	content, err := declarationartifact.Read(ctx, path)
	if err != nil {
		return lock.File{}, err
	}
	return loadContentWithCatalog(ctx, content, codec.contributions)
}
