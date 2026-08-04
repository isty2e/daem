package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

type batchErrorKey struct {
	operation acquisition.Operation
	sourceID  string
}

type trackingBatchResolver struct {
	artifacts map[string]resolutionFixture
	listings  map[string]source.RootListing
	errors    map[batchErrorKey]error

	batches              [][]acquisition.Request
	batchOptions         []acquisition.BatchOptions
	resolveCalls         []string
	listRootCalls        []string
	mismatchFirstRequest bool
	dropLastResult       bool
	cancelAfterBatch     context.CancelFunc
}

type failingSequentialResolver struct {
	errBySourceID map[string]error
}

func (resolver failingSequentialResolver) Resolve(
	_ context.Context,
	sourceSpec source.Source,
	_ acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	if configuredErr := resolver.errBySourceID[string(sourceID)]; configuredErr != nil {
		return acquisition.Resolution{}, configuredErr
	}

	return acquisition.Resolution{}, fmt.Errorf("missing fixture for source %s", sourceID)
}

func (resolver *trackingBatchResolver) Resolve(
	ctx context.Context,
	sourceSpec source.Source,
	_ acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	resolver.resolveCalls = append(resolver.resolveCalls, string(sourceID))

	resolvedArtifact, ok := resolver.artifacts[string(sourceID)]
	if !ok {
		return acquisition.Resolution{}, fmt.Errorf("missing source %s", sourceID)
	}

	return resolutionFromTestFixture(ctx, sourceSpec, resolvedArtifact)
}

func (resolver *trackingBatchResolver) ListSourceRoot(
	_ context.Context,
	sourceSpec source.Source,
	options acquisition.OperationOptions,
) (source.RootListing, error) {
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return source.RootListing{}, err
	}
	resolver.listRootCalls = append(resolver.listRootCalls, string(sourceID))

	listing, ok := resolver.listings[string(sourceID)]
	if !ok {
		return source.RootListing{}, fmt.Errorf("missing source root %s", sourceID)
	}
	if err := chargeRootListingBudget(options.RootListingBudget(), listing); err != nil {
		return source.RootListing{}, err
	}

	return listing, nil
}

func (resolver *trackingBatchResolver) ResolveBatch(
	ctx context.Context,
	requests []acquisition.Request,
	options acquisition.BatchOptions,
) ([]acquisition.Result, error) {
	resolver.batches = append(resolver.batches, append([]acquisition.Request(nil), requests...))
	resolver.batchOptions = append(resolver.batchOptions, options)

	results := make([]acquisition.Result, 0, len(requests))
	for index, request := range requests {
		resultRequest := request
		if resolver.mismatchFirstRequest && index == 0 {
			mismatchedRequest, requestErr := acquisition.NewRequest(
				"wrong-request",
				request.Ordinal(),
				request.Operation(),
				request.Source(),
			)
			if requestErr != nil {
				return nil, requestErr
			}
			resultRequest = mismatchedRequest
		}

		sourceID, err := source.SourceIDFor(request.Source())
		if err != nil {
			result, resultErr := acquisition.NewFailureResult(resultRequest, err)
			if resultErr != nil {
				return nil, resultErr
			}
			results = append(results, result)
			continue
		}
		if configuredErr := resolver.errors[batchErrorKey{operation: request.Operation(), sourceID: string(sourceID)}]; configuredErr != nil {
			result, resultErr := acquisition.NewFailureResult(resultRequest, configuredErr)
			if resultErr != nil {
				return nil, resultErr
			}
			results = append(results, result)
			continue
		}

		var result acquisition.Result
		switch request.Operation() {
		case acquisition.OperationResolve:
			resolvedArtifact, ok := resolver.artifacts[string(sourceID)]
			if !ok {
				result, err = acquisition.NewFailureResult(resultRequest, fmt.Errorf("missing source %s", sourceID))
				break
			}
			resolution, resolveErr := resolutionFromTestFixture(ctx, request.Source(), resolvedArtifact)
			if resolveErr != nil {
				return nil, resolveErr
			}
			result, err = acquisition.NewResolutionResult(resultRequest, resolution)
		case acquisition.OperationListRoot:
			listing, ok := resolver.listings[string(sourceID)]
			if !ok {
				result, err = acquisition.NewFailureResult(resultRequest, fmt.Errorf("missing source root %s", sourceID))
				break
			}
			if budgetErr := chargeRootListingBudget(options.RootListingBudget(), listing); budgetErr != nil {
				return nil, budgetErr
			}
			result, err = acquisition.NewListingResult(resultRequest, listing)
		default:
			return nil, fmt.Errorf("unknown source operation %q", request.Operation())
		}
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	if resolver.cancelAfterBatch != nil {
		resolver.cancelAfterBatch()
	}
	if resolver.dropLastResult && len(results) > 0 {
		results = results[:len(results)-1]
	}

	return results, nil
}

func chargeRootListingBudget(
	budget *source.RootListingBudget,
	listing source.RootListing,
) error {
	for _, name := range listing.ChildNames() {
		if err := budget.AdmitEntryName(len(name)); err != nil {
			return err
		}
	}
	return nil
}

func hasBatchOperation(batches [][]acquisition.Request, operation acquisition.Operation) bool {
	for _, batch := range batches {
		for _, request := range batch {
			if request.Operation() == operation {
				return true
			}
		}
	}

	return false
}

type lockEventRecorder struct {
	events []Event
}

func newLockEventRecorder() *lockEventRecorder {
	return &lockEventRecorder{}
}

func (recorder *lockEventRecorder) sink(event Event) {
	recorder.events = append(recorder.events, event)
}

func (recorder *lockEventRecorder) snapshot() []Event {
	return append([]Event(nil), recorder.events...)
}

func lockEventKinds(events []Event) []EventKind {
	kinds := make([]EventKind, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}

	return kinds
}

func filterLockEvents(events []Event, kind EventKind) []Event {
	filtered := make([]Event, 0)
	for _, event := range events {
		if event.Kind == kind {
			filtered = append(filtered, event)
		}
	}

	return filtered
}
