// Package nativeconfig owns command-local native input/profile composition.
// Reconciliation belongs to readiness; prospective generation consumes injected
// codecs and limits without importing this observation boundary.
package nativeconfig

import (
	"context"
	"errors"

	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func DocumentLimits() configdocument.Limits {
	return configdocument.Limits{
		MaximumBytes: recovery.MaximumRecoveryBackupFileBytes,
		MaximumDepth: tomlstrict.MaximumDepth,
	}
}

func LockCodec(ctx context.Context) (lockfile.NativeCodec, error) {
	contributions, err := aggregatecodec.ForContributions(ctx, codecIDs(), DocumentLimits())
	if err != nil {
		return lockfile.NativeCodec{}, err
	}
	return lockfile.NewNativeCodec(contributions)
}

func codecIDs() []aggregate.CodecContractID {
	var required []aggregate.CodecContractID
	for _, placement := range aggregate.ImplementedHookPlacements() {
		required = append(required, placement.CodecContractID())
	}
	for _, placement := range aggregate.ImplementedMCPContracts() {
		required = append(required, placement.CodecContractID())
	}
	for _, syntax := range []string{"json", "jsonc", "json-line-comments", "toml"} {
		id, _ := aggregate.HostConfigCodecForSyntax(syntax)
		required = append(required, id)
	}
	return required
}

// LoadLockfile reads an explicitly selected native snapshot at the workflow boundary
// without changing the default lockfile reader's generation or family admission.
func LoadLockfile(ctx context.Context, path string) (lock.File, error) {
	file, err := lockfile.Load(ctx, path)
	var version lockfile.UnsupportedVersionError
	if !errors.As(err, &version) || version.Found != lock.NativeVersion {
		return file, err
	}
	codec, err := LockCodec(ctx)
	if err != nil {
		return lock.File{}, err
	}
	return codec.Load(ctx, path)
}

func ResolveSourceEpoch(ctx context.Context, paths daempaths.Paths, environment desired.Environment, file lock.File, selection targetselection.Selection) (lockobserve.SourceEpoch, error) {
	if file.Version == lock.NativeVersion {
		return lockobserve.ResolveNativeDocumentSourceEpoch(ctx, paths, environment, file, selection, DocumentLimits())
	}
	return lockobserve.ResolveSourceEpoch(ctx, paths, environment, file, selection)
}
