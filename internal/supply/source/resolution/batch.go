package resolution

import (
	"context"
	"fmt"
	"sync"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

var _ acquisition.BatchResolver = Resolver{}

// ResolveBatch resolves source operations concurrently while preserving request result slots.
func (resolver Resolver) ResolveBatch(
	ctx context.Context,
	requests []acquisition.Request,
	options acquisition.BatchOptions,
) ([]acquisition.Result, error) {
	if ctx == nil {
		return nil, fmt.Errorf("source batch context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateBatchRequests(requests); err != nil {
		return nil, err
	}
	resolver = resolver.withResolutionSession()

	results := make([]acquisition.Result, len(requests))
	owners := make([]batchOwner, 0, len(requests))
	ownerByKey := make(map[batchOperationKey]int)
	ownerIndexBySlot := make([]int, len(requests))
	for index, request := range requests {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		emitBatchRequestEvent(options.Events(), request, acquisition.EventQueued, "", "", nil)

		sourceID, err := source.SourceIDFor(request.Source())
		if err != nil {
			return nil, fmt.Errorf("source batch request %q: %w", request.ID(), err)
		}

		key := batchOperationKey{operation: request.Operation(), sourceID: sourceID}
		if ownerIndex, ok := ownerByKey[key]; ok {
			ownerIndexBySlot[index] = ownerIndex
			continue
		}

		ownerIndex := len(owners)
		ownerByKey[key] = ownerIndex
		ownerIndexBySlot[index] = ownerIndex
		owners = append(owners, batchOwner{
			request:  request,
			sourceID: sourceID,
		})
	}
	if err := resolver.assignRepositoryPreparationGroups(owners); err != nil {
		return nil, err
	}

	if len(owners) == 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		return results, nil
	}

	if err := resolver.runBatchOwners(ctx, owners, options); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for slot, request := range requests {
		owner := owners[ownerIndexBySlot[slot]]
		result, err := owner.resultFor(request)
		if err != nil {
			return nil, fmt.Errorf("construct source batch result[%d]: %w", slot, err)
		}
		results[slot] = result
		if owner.err != nil {
			emitBatchRequestEvent(options.Events(), request, acquisition.EventFailed, owner.sourceID, "", owner.err)
			continue
		}
		emitBatchRequestEvent(options.Events(), request, acquisition.EventCompleted, owner.sourceID, owner.resolvedRef(), nil)
	}

	return results, nil
}

type batchOperationKey struct {
	operation acquisition.Operation
	sourceID  artifact.SourceID
}

type batchOwner struct {
	request  acquisition.Request
	sourceID artifact.SourceID

	resolution acquisition.Resolution
	listing    source.RootListing
	err        error

	repositoryPreparation *repositoryPreparation
	repositoryLeader      bool
}

type repositoryPreparation struct {
	ready   chan struct{}
	err     error
	members []int
}

func (owner batchOwner) resolvedRef() artifact.ResolvedRef {
	switch owner.request.Operation() {
	case acquisition.OperationResolve:
		return owner.resolution.Identity().ResolvedRef()
	case acquisition.OperationListRoot:
		return owner.listing.ResolvedRef()
	default:
		return ""
	}
}

func (owner batchOwner) resultFor(request acquisition.Request) (acquisition.Result, error) {
	if owner.err != nil {
		return acquisition.NewFailureResult(request, owner.err)
	}
	switch request.Operation() {
	case acquisition.OperationResolve:
		return acquisition.NewResolutionResult(request, owner.resolution)
	case acquisition.OperationListRoot:
		return acquisition.NewListingResult(request, owner.listing)
	default:
		return acquisition.Result{}, fmt.Errorf("unknown source operation %q", request.Operation())
	}
}

func validateBatchRequests(requests []acquisition.Request) error {
	seen := make(map[acquisition.RequestID]struct{}, len(requests))
	for _, request := range requests {
		if err := request.Validate(); err != nil {
			return err
		}
		if _, ok := seen[request.ID()]; ok {
			return fmt.Errorf("duplicate source batch request id %q", request.ID())
		}
		seen[request.ID()] = struct{}{}
	}

	return nil
}

func (resolver Resolver) runBatchOwners(ctx context.Context, owners []batchOwner, options acquisition.BatchOptions) error {
	maxParallel := min(max(options.NormalizedMaxParallel(), 1), len(owners))

	jobs := make(chan int)
	var waitGroup sync.WaitGroup
	for range maxParallel {
		waitGroup.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case index, ok := <-jobs:
					if !ok {
						return
					}
					if err := ctx.Err(); err != nil {
						return
					}
					resolver.runBatchOwner(ctx, &owners[index], options.Events())
				}
			}
		})
	}

	for _, index := range ownerLaunchOrder(owners) {
		select {
		case <-ctx.Done():
			close(jobs)
			waitGroup.Wait()
			return ctx.Err()
		case jobs <- index:
		}
	}

	close(jobs)
	waitGroup.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}

	return nil
}

func (resolver Resolver) runBatchOwner(ctx context.Context, owner *batchOwner, events acquisition.EventSink) {
	emitBatchRequestEvent(events, owner.request, acquisition.EventStarted, owner.sourceID, "", nil)
	operationOptions, err := acquisition.NewOperationOptions(owner.request, events)
	if err != nil {
		owner.err = err
		return
	}
	if err := resolver.prepareRepositorySnapshot(ctx, owner, operationOptions); err != nil {
		owner.err = err
		return
	}
	switch owner.request.Operation() {
	case acquisition.OperationResolve:
		owner.resolution, owner.err = resolver.Resolve(ctx, owner.request.Source(), operationOptions)
	case acquisition.OperationListRoot:
		owner.listing, owner.err = resolver.ListSourceRoot(ctx, owner.request.Source(), operationOptions)
	default:
		owner.err = fmt.Errorf("unknown source operation %q", owner.request.Operation())
	}
}

func (resolver Resolver) assignRepositoryPreparationGroups(owners []batchOwner) error {
	preparer, ok := resolver.git.(acquisition.RepositorySnapshotPreparer)
	if !ok {
		return nil
	}

	groups := make(map[acquisition.RepositorySnapshotGroupID]*repositoryPreparation)
	for index := range owners {
		key, grouped, err := preparer.RepositorySnapshotGroup(owners[index].request.Source())
		if err != nil {
			return fmt.Errorf("classify repository snapshot for request %q: %w", owners[index].request.ID(), err)
		}
		if !grouped {
			continue
		}
		if err := key.Validate(); err != nil {
			return fmt.Errorf("classify repository snapshot for request %q: %w", owners[index].request.ID(), err)
		}
		group, ok := groups[key]
		if !ok {
			group = &repositoryPreparation{ready: make(chan struct{})}
			groups[key] = group
		}
		group.members = append(group.members, index)
	}

	for _, group := range groups {
		if len(group.members) < 2 {
			continue
		}
		for memberOffset, ownerIndex := range group.members {
			owners[ownerIndex].repositoryPreparation = group
			owners[ownerIndex].repositoryLeader = memberOffset == 0
		}
	}
	return nil
}

func (resolver Resolver) prepareRepositorySnapshot(
	ctx context.Context,
	owner *batchOwner,
	options acquisition.OperationOptions,
) error {
	preparation := owner.repositoryPreparation
	if preparation == nil {
		return nil
	}

	if owner.repositoryLeader {
		preparer, ok := resolver.git.(acquisition.RepositorySnapshotPreparer)
		if !ok {
			return fmt.Errorf("repository snapshot preparation capability is unavailable")
		}
		preparation.err = preparer.PrepareRepositorySnapshot(ctx, owner.request.Source(), options)
		close(preparation.ready)
		return preparation.err
	}

	select {
	case <-preparation.ready:
		return preparation.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ownerLaunchOrder(owners []batchOwner) []int {
	order := make([]int, 0, len(owners))
	for index := range owners {
		if owners[index].repositoryPreparation != nil && !owners[index].repositoryLeader {
			continue
		}
		order = append(order, index)
	}
	for index := range owners {
		if owners[index].repositoryPreparation != nil && !owners[index].repositoryLeader {
			order = append(order, index)
		}
	}
	return order
}

func emitBatchRequestEvent(
	events acquisition.EventSink,
	request acquisition.Request,
	kind acquisition.EventKind,
	sourceID artifact.SourceID,
	resolvedRef artifact.ResolvedRef,
	err error,
) {
	event, eventErr := acquisition.NewEvent(kind, request, sourceID, resolvedRef, err)
	if eventErr == nil {
		events.Emit(event)
	}
}
