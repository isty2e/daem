package generate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/desired"
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
	Paths                daempaths.Paths
	Environment          desired.Environment
	UsePersistentCache   bool
	MaxParallelSourceOps int
	SourceEvents         acquisition.EventSink
	Events               lockbuild.EventSink
	HookEncoder          commandhook.ContributionEncoder
	MCPEncoder           lockrefine.MCPContributionEncoder
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

	lockfileSnapshot, err := lockbuild.BuildWithOptions(ctx, input.Environment, baseResolver, lockbuild.Options{
		MaxParallelSourceOps:    input.MaxParallelSourceOps,
		Events:                  input.Events,
		SourceEvents:            input.SourceEvents,
		HookContributionEncoder: input.HookEncoder,
		MCPContributionEncoder:  input.MCPEncoder,
	})
	if err != nil {
		return Result{}, err
	}
	content, err := lockfile.Marshal(lockfileSnapshot)
	if err != nil {
		return Result{}, fmt.Errorf("marshal lockfile: %w", err)
	}

	return Result{
		snapshot: lockfileSnapshot,
		content:  content,
	}, nil
}
