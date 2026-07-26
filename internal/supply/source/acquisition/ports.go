package acquisition

import (
	"context"

	"github.com/isty2e/daem/internal/supply/source"
)

// Resolver materializes a source without knowing host targets or destinations.
type Resolver interface {
	Resolve(context.Context, source.Source) (Resolution, error)
}

// ResolverWithOptions resolves a source with operation observation routing.
type ResolverWithOptions interface {
	ResolveWithOptions(context.Context, source.Source, OperationOptions) (Resolution, error)
}

// RootLister lists direct source-root children without resolving artifacts.
type RootLister interface {
	ListSourceRoot(context.Context, source.Source) (source.RootListing, error)
}

// RootListerWithOptions lists a source root with operation observation routing.
type RootListerWithOptions interface {
	ListSourceRootWithOptions(context.Context, source.Source, OperationOptions) (source.RootListing, error)
}

// BatchResolver resolves source operations with stable result slots.
type BatchResolver interface {
	ResolveBatch(context.Context, []Request, BatchOptions) ([]Result, error)
}

// ResolutionSessionProvider creates a fresh operation-local resolver session.
type ResolutionSessionProvider interface {
	NewResolutionSession() (Resolver, error)
}

// RepositorySnapshotPreparer exposes optional operation-local repository
// snapshot sharing without exposing a concrete backend type.
type RepositorySnapshotPreparer interface {
	RepositorySnapshotGroup(source.Source) (RepositorySnapshotGroupID, bool, error)
	PrepareRepositorySnapshot(context.Context, source.Source, OperationOptions) error
}

// BatchOptions controls source batch execution.
type BatchOptions struct {
	maxParallel int
	events      EventSink
}

// NewBatchOptions constructs bounded source batch options.
func NewBatchOptions(maxParallel int, events EventSink) BatchOptions {
	return BatchOptions{maxParallel: maxParallel, events: events}
}

// NormalizedMaxParallel returns the effective concurrency limit.
func (options BatchOptions) NormalizedMaxParallel() int {
	if options.maxParallel <= 0 {
		return 1
	}
	return options.maxParallel
}

// Events returns the non-authoritative event sink.
func (options BatchOptions) Events() EventSink { return options.events }
