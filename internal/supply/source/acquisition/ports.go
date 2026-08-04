package acquisition

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/supply/source"
)

// Resolver materializes a source with optional operation observation routing.
type Resolver interface {
	Resolve(context.Context, source.Source, OperationOptions) (Resolution, error)
}

// RootLister lists direct source-root children with optional operation observation routing.
type RootLister interface {
	ListSourceRoot(context.Context, source.Source, OperationOptions) (source.RootListing, error)
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
	maxParallel       int
	events            EventSink
	rootListingBudget *source.RootListingBudget
}

// NewBatchOptions constructs bounded source batch options.
func NewBatchOptions(maxParallel int, events EventSink) BatchOptions {
	return BatchOptions{
		maxParallel:       maxParallel,
		events:            events,
		rootListingBudget: source.NewRootListingBudget(),
	}
}

// WithRootListingBudget binds one budget shared by every root-listing request.
func (options BatchOptions) WithRootListingBudget(
	budget *source.RootListingBudget,
) (BatchOptions, error) {
	if budget == nil {
		return BatchOptions{}, fmt.Errorf("source root listing budget is required")
	}
	options.rootListingBudget = budget
	return options, nil
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

// RootListingBudget returns the operation-wide source-root enumeration budget.
func (options BatchOptions) RootListingBudget() *source.RootListingBudget {
	if options.rootListingBudget != nil {
		return options.rootListingBudget
	}
	return source.NewRootListingBudget()
}
