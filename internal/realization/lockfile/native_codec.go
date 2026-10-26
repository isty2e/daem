package lockfile

import (
	"context"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
)

// NativeCodec selects native-generation transport with caller-owned concrete
// value admission. It does not install codecs in ordinary readers or executors.
type NativeCodec struct {
	contributions aggregate.ContributionCatalog
}

func NewNativeCodec(contributions aggregate.ContributionCatalog) (NativeCodec, error) {
	if err := contributions.Validate(); err != nil {
		return NativeCodec{}, err
	}
	return NativeCodec{contributions: contributions}, nil
}

func (codec NativeCodec) Marshal(ctx context.Context, file lock.File) ([]byte, error) {
	if err := codec.contributions.Validate(); err != nil {
		return nil, err
	}
	if err := lock.ValidateNative(file); err != nil {
		return nil, err
	}
	return marshalValidatedWithCatalog(ctx, file, codec.contributions)
}

func (codec NativeCodec) Load(ctx context.Context, path string) (lock.File, error) {
	if err := codec.contributions.Validate(); err != nil {
		return lock.File{}, err
	}
	content, err := declarationartifact.Read(ctx, path)
	if err != nil {
		return lock.File{}, err
	}
	return loadNativeContentWithCatalog(ctx, content, codec.contributions)
}
