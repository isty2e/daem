package generate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
)

// Input carries canonical desired facts and source-boundary options for one
// prospective lock generation.
type Input struct {
	Paths                  daempaths.Paths
	Environment            desired.Environment
	UsePersistentCache     bool
	PreparePersistentCache func(context.Context) error
	MaxParallelSourceOps   int
	SourceEvents           acquisition.EventSink
	Events                 lockbuild.EventSink
	HookEncoder            commandhook.ContributionEncoder
	MCPEncoder             lockrefine.MCPContributionEncoder
	ExtensionOrderIdentity lockrefine.ExtensionOrderIdentityResolver
	NativeDocuments        *NativeDocumentProfile
}

// NativeDocumentProfile supplies source parsing limits and native transport
// admission without granting observation, root or execution authority.
type NativeDocumentProfile struct {
	Codec  lockfile.NativeCodec
	Limits configdocument.Limits
}

// Result carries a generated canonical lock snapshot and its boundary
// serialization.
type Result struct {
	snapshot lock.File
	content  []byte
}

// Snapshot returns the generated canonical lock snapshot.
func (result Result) Snapshot() lock.File { return result.snapshot }

// Content returns a defensive copy of the serialized lockfile bytes that
// correspond to Snapshot.
func (result Result) Content() []byte {
	return append([]byte(nil), result.content...)
}

// Build resolves lockable desired resources into a canonical lock snapshot
// and lockfile bytes.
func Build(ctx context.Context, input Input) (Result, error) {
	var native NativeDocumentProfile
	if input.NativeDocuments != nil {
		native = *input.NativeDocuments
		if err := native.Limits.Validate(); err != nil {
			return Result{}, err
		}
	} else if len(input.Environment.HostConfigs()) != 0 {
		return Result{}, fmt.Errorf("native lock generation requires its selected codec and document limits")
	}
	if err := lockrefine.ValidateManagedPathIntent(input.Environment); err != nil {
		return Result{}, err
	}

	resolverPaths := input.Paths
	if !input.UsePersistentCache {
		tempCacheDir, err := os.MkdirTemp("", "daem-lock-")
		if err != nil {
			return Result{}, fmt.Errorf("create temporary source cache: %w", err)
		}
		defer os.RemoveAll(tempCacheDir)
		resolverPaths.SourceCacheDir = filepath.Join(tempCacheDir, "sources")
	}

	baseResolver, err := sourceresolution.NewResolver(resolverPaths)
	if err != nil {
		return Result{}, err
	}
	if input.UsePersistentCache {
		baseResolver = baseResolver.WithCachePreparation(input.PreparePersistentCache)
	}

	options := lockbuild.Options{
		MaxParallelSourceOps:    input.MaxParallelSourceOps,
		Events:                  input.Events,
		SourceEvents:            input.SourceEvents,
		HookContributionEncoder: input.HookEncoder,
		MCPContributionEncoder:  input.MCPEncoder,
		ExtensionOrderIdentity:  input.ExtensionOrderIdentity,
	}
	var lockfileSnapshot lock.File
	if len(input.Environment.HostConfigs()) != 0 {
		lockfileSnapshot, err = lockbuild.BuildNativeDocumentsWithOptions(ctx, input.Environment, baseResolver, options, native.Limits)
	} else {
		lockfileSnapshot, err = lockbuild.BuildWithOptions(ctx, input.Environment, baseResolver, options)
	}
	if err != nil {
		return Result{}, err
	}
	var content []byte
	if lockfileSnapshot.Version == lock.NativeVersion {
		content, err = native.Codec.Marshal(ctx, lockfileSnapshot)
	} else {
		content, err = lockfile.Marshal(lockfileSnapshot)
	}
	if err != nil {
		return Result{}, fmt.Errorf("marshal lockfile: %w", err)
	}

	return Result{
		snapshot: lockfileSnapshot,
		content:  content,
	}, nil
}
