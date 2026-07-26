package resolution

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolveBatchEmptyRequests(t *testing.T) {
	resolver := Resolver{}

	results, err := resolver.ResolveBatch(context.Background(), nil, acquisition.BatchOptions{})
	if err != nil {
		t.Fatalf("ResolveBatch returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0", len(results))
	}
}

func TestResolveBatchRejectsInvalidBatchContractBeforeWork(t *testing.T) {
	validSource := sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)
	validRequest := batchRequest("request-1", 0, acquisition.OperationResolve, validSource)
	for _, testCase := range []struct {
		name     string
		ctx      context.Context
		requests []acquisition.Request
		want     string
	}{
		{
			name:     "nil context",
			ctx:      nil,
			requests: []acquisition.Request{validRequest},
			want:     "source batch context is required",
		},
		{
			name: "duplicate id",
			ctx:  context.Background(),
			requests: []acquisition.Request{
				batchRequest("same", 0, acquisition.OperationResolve, validSource),
				batchRequest("same", 1, acquisition.OperationResolve, validSource),
			},
			want: `duplicate source batch request id "same"`,
		},
		{
			name:     "zero request",
			ctx:      context.Background(),
			requests: []acquisition.Request{{}},
			want:     "request id is required",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tracker := newBatchResolverTracker(t)
			resolver := Resolver{local: tracker}

			_, err := resolver.ResolveBatch(testCase.ctx, testCase.requests, acquisition.NewBatchOptions(4, nil))
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("ResolveBatch error = %v, want %q", err, testCase.want)
			}
			if calls := tracker.totalCalls(); calls != 0 {
				t.Fatalf("backend calls = %d, want 0", calls)
			}
		})
	}
}

func TestResolveBatchMaxParallelOneRunsUniqueOperationsSequentially(t *testing.T) {
	tracker := newBatchResolverTracker(t)
	resolver := Resolver{local: tracker}

	results, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
		batchRequest("first", 0, acquisition.OperationResolve, sourcetest.Local(t, "skills/first", source.LocalSourceModeVendor)),
		batchRequest("second", 1, acquisition.OperationResolve, sourcetest.Local(t, "skills/second", source.LocalSourceModeVendor)),
		batchRequest("third", 2, acquisition.OperationListRoot, sourcetest.Local(t, "skills/third", source.LocalSourceModeVendor)),
	}, acquisition.NewBatchOptions(1, nil))
	if err != nil {
		t.Fatalf("ResolveBatch returned error: %v", err)
	}
	assertBatchResultsOK(t, results)
	if maxActive := tracker.maxActive(); maxActive != 1 {
		t.Fatalf("max active calls = %d, want 1", maxActive)
	}
	if log := tracker.callLogSnapshot(); strings.Join(log, ",") != "resolve:skills/first,resolve:skills/second,list_root:skills/third" {
		t.Fatalf("call log = %#v, want input order", log)
	}
}

func TestResolveBatchMaxParallelZeroUsesSequentialLimit(t *testing.T) {
	tracker := newBatchResolverTracker(t)
	resolver := Resolver{local: tracker}

	results, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
		batchRequest("first", 0, acquisition.OperationResolve, sourcetest.Local(t, "skills/first", source.LocalSourceModeVendor)),
		batchRequest("second", 1, acquisition.OperationResolve, sourcetest.Local(t, "skills/second", source.LocalSourceModeVendor)),
	}, acquisition.BatchOptions{})
	if err != nil {
		t.Fatalf("ResolveBatch returned error: %v", err)
	}
	assertBatchResultsOK(t, results)
	if maxActive := tracker.maxActive(); maxActive != 1 {
		t.Fatalf("max active calls = %d, want 1", maxActive)
	}
}

func TestResolveBatchMaxParallelGreaterThanOneOverlapsUniqueOperations(t *testing.T) {
	release := make(chan struct{})
	tracker := newBatchResolverTracker(t)
	tracker.release = release
	tracker.started = make(chan string, 2)
	resolver := Resolver{local: tracker}

	done := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
			batchRequest("first", 0, acquisition.OperationResolve, sourcetest.Local(t, "skills/first", source.LocalSourceModeVendor)),
			batchRequest("second", 1, acquisition.OperationResolve, sourcetest.Local(t, "skills/second", source.LocalSourceModeVendor)),
		}, acquisition.NewBatchOptions(2, nil))
		done <- err
	}()

	waitForBatchStarts(t, tracker.started, 2)
	if maxActive := tracker.maxActive(); maxActive != 2 {
		t.Fatalf("max active calls = %d, want 2", maxActive)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("ResolveBatch returned error: %v", err)
	}
}

func TestResolveBatchKeepsResultOrderWhenCompletionOrderDiffers(t *testing.T) {
	slowSource := sourcetest.Local(t, "skills/slow", source.LocalSourceModeVendor)
	slowID := mustSourceID(t, slowSource)
	releaseSlow := make(chan struct{})
	tracker := newBatchResolverTracker(t)
	tracker.releaseBySourceID[slowID] = releaseSlow
	tracker.done = make(chan string, 2)
	resolver := Resolver{local: tracker}

	resultsDone := make(chan []acquisition.Result, 1)
	errorDone := make(chan error, 1)
	go func() {
		results, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
			batchRequest("slow", 0, acquisition.OperationResolve, slowSource),
			batchRequest("fast", 1, acquisition.OperationResolve, sourcetest.Local(t, "skills/fast", source.LocalSourceModeVendor)),
		}, acquisition.NewBatchOptions(2, nil))
		resultsDone <- results
		errorDone <- err
	}()

	if got := waitForBatchDone(t, tracker.done); got != "resolve:skills/fast" {
		t.Fatalf("first completed call = %q, want fast resolve", got)
	}
	close(releaseSlow)
	results := <-resultsDone
	if err := <-errorDone; err != nil {
		t.Fatalf("ResolveBatch returned error: %v", err)
	}
	if results[0].Request().ID() != "slow" || results[1].Request().ID() != "fast" {
		t.Fatalf("result request order = %q, %q; want slow, fast", results[0].Request().ID(), results[1].Request().ID())
	}
	assertBatchResultsOK(t, results)
}

func TestResolveBatchDedupesDuplicateResolveRequests(t *testing.T) {
	tracker := newBatchResolverTracker(t)
	resolver := Resolver{local: tracker}
	sourceSpec := sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)

	results, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
		batchRequest("first", 0, acquisition.OperationResolve, sourceSpec),
		batchRequest("second", 1, acquisition.OperationResolve, sourceSpec),
		batchRequest("third", 2, acquisition.OperationResolve, sourceSpec),
	}, acquisition.NewBatchOptions(1, nil))
	if err != nil {
		t.Fatalf("ResolveBatch returned error: %v", err)
	}
	if calls := tracker.resolveCallsFor(sourceSpec); calls != 1 {
		t.Fatalf("resolve calls = %d, want 1", calls)
	}
	firstResolution, ok := results[0].Resolution()
	if !ok {
		t.Fatalf("result 0 = %#v, want resolution", results[0])
	}
	for index, result := range results {
		if result.Request().ID() != acquisition.RequestID(fmt.Sprintf("%s", []string{"first", "second", "third"}[index])) {
			t.Fatalf("result %d request id = %q", index, result.Request().ID())
		}
		resolution, ok := result.Resolution()
		if !ok || !resolution.Identity().Equal(firstResolution.Identity()) {
			t.Fatalf("result %d resolution = %#v, want shared owner identity", index, result)
		}
	}
}

func TestResolveBatchEmitsSourceLifecycleEventsForDuplicateSlots(t *testing.T) {
	tracker := newBatchResolverTracker(t)
	resolver := Resolver{local: tracker}
	recorder := newSourceEventRecorder()
	sourceSpec := sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)
	sourceID := mustSourceID(t, sourceSpec)

	results, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
		batchRequest("first", 0, acquisition.OperationResolve, sourceSpec),
		batchRequest("second", 1, acquisition.OperationResolve, sourceSpec),
		batchRequest("third", 2, acquisition.OperationResolve, sourceSpec),
	}, acquisition.NewBatchOptions(3, recorder.sink))
	if err != nil {
		t.Fatalf("ResolveBatch returned error: %v", err)
	}
	assertBatchResultsOK(t, results)

	events := recorder.snapshot()
	assertSourceEventCount(t, events, acquisition.EventQueued, 3)
	assertSourceEventCount(t, events, acquisition.EventStarted, 1)
	assertSourceEventCount(t, events, acquisition.EventCompleted, 3)
	assertSourceEventCount(t, events, acquisition.EventFailed, 0)
	assertSourceEventIDs(t, events, acquisition.EventQueued, []acquisition.RequestID{"first", "second", "third"})
	assertSourceEventIDs(t, events, acquisition.EventCompleted, []acquisition.RequestID{"first", "second", "third"})
	for _, event := range filterSourceEvents(events, acquisition.EventCompleted) {
		if event.ResolvedRef() != "" {
			t.Fatalf("completed local event resolved ref = %q, want empty", event.ResolvedRef())
		}
	}

	started := filterSourceEvents(events, acquisition.EventStarted)
	if len(started) != 1 {
		t.Fatalf("started events = %#v, want one owner event", started)
	}
	if started[0].Request().ID() != "first" || started[0].SourceID() != sourceID {
		t.Fatalf("started event = %#v, want first owner and source id %q", started[0], sourceID)
	}
	if calls := tracker.resolveCallsFor(sourceSpec); calls != 1 {
		t.Fatalf("resolve calls = %d, want one deduped owner", calls)
	}
}

func TestResolveBatchEmitsTerminalEventsInRequestOrderForInterleavedDuplicates(t *testing.T) {
	tracker := newBatchResolverTracker(t)
	firstSource := sourcetest.Local(t, "skills/first", source.LocalSourceModeVendor)
	failingSource := sourcetest.Local(t, "skills/failing", source.LocalSourceModeVendor)
	wantErr := errors.New("injected sibling failure")
	tracker.resolveErrBySourceID[mustSourceID(t, failingSource)] = wantErr
	resolver := Resolver{local: tracker}
	recorder := newSourceEventRecorder()

	results, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
		batchRequest("first-owner", 0, acquisition.OperationResolve, firstSource),
		batchRequest("failing-sibling", 1, acquisition.OperationResolve, failingSource),
		batchRequest("first-duplicate", 2, acquisition.OperationResolve, firstSource),
	}, acquisition.NewBatchOptions(2, recorder.sink))
	if err != nil {
		t.Fatalf("ResolveBatch returned top-level error: %v", err)
	}
	if results[0].Err() != nil || !errors.Is(results[1].Err(), wantErr) || results[2].Err() != nil {
		t.Fatalf("result errors = %v, %v, %v", results[0].Err(), results[1].Err(), results[2].Err())
	}

	terminalIDs := make([]acquisition.RequestID, 0, len(results))
	terminalKinds := make([]acquisition.EventKind, 0, len(results))
	for _, event := range recorder.snapshot() {
		if event.Kind() != acquisition.EventCompleted && event.Kind() != acquisition.EventFailed {
			continue
		}
		terminalIDs = append(terminalIDs, event.Request().ID())
		terminalKinds = append(terminalKinds, event.Kind())
	}
	wantIDs := []acquisition.RequestID{"first-owner", "failing-sibling", "first-duplicate"}
	wantKinds := []acquisition.EventKind{
		acquisition.EventCompleted,
		acquisition.EventFailed,
		acquisition.EventCompleted,
	}
	if !slices.Equal(terminalIDs, wantIDs) || !slices.Equal(terminalKinds, wantKinds) {
		t.Fatalf(
			"terminal events = ids %v kinds %v, want ids %v kinds %v",
			terminalIDs,
			terminalKinds,
			wantIDs,
			wantKinds,
		)
	}
	if calls := tracker.resolveCallsFor(firstSource); calls != 1 {
		t.Fatalf("first source resolve calls = %d, want one deduped owner", calls)
	}
}

func TestResolveBatchDedupesDuplicateListRootRequests(t *testing.T) {
	tracker := newBatchResolverTracker(t)
	resolver := Resolver{local: tracker}
	recorder := newSourceEventRecorder()
	sourceSpec := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)

	results, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
		batchRequest("first", 0, acquisition.OperationListRoot, sourceSpec),
		batchRequest("second", 1, acquisition.OperationListRoot, sourceSpec),
	}, acquisition.NewBatchOptions(1, recorder.sink))
	if err != nil {
		t.Fatalf("ResolveBatch returned error: %v", err)
	}
	if calls := tracker.listCallsFor(sourceSpec); calls != 1 {
		t.Fatalf("list calls = %d, want 1", calls)
	}
	for index, result := range results {
		if result.Request().ID() != acquisition.RequestID([]string{"first", "second"}[index]) {
			t.Fatalf("result %d request id = %q", index, result.Request().ID())
		}
		listing, ok := result.Listing()
		if !ok || len(listing.ChildNames()) != 1 || listing.ChildNames()[0] != "listed" {
			t.Fatalf("result %d listing = %#v, want shared owner listing", index, listing)
		}
	}
	for _, event := range filterSourceEvents(recorder.snapshot(), acquisition.EventCompleted) {
		if event.ResolvedRef() != "" {
			t.Fatalf("completed local list-root event resolved ref = %q, want empty", event.ResolvedRef())
		}
	}
}

func TestResolveBatchDedupesDuplicateListRootErrors(t *testing.T) {
	wantErr := errors.New("list failed")
	tracker := newBatchResolverTracker(t)
	sourceSpec := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)
	tracker.listErrBySourceID[mustSourceID(t, sourceSpec)] = wantErr
	resolver := Resolver{local: tracker}

	results, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
		batchRequest("first", 0, acquisition.OperationListRoot, sourceSpec),
		batchRequest("second", 1, acquisition.OperationListRoot, sourceSpec),
	}, acquisition.NewBatchOptions(1, nil))
	if err != nil {
		t.Fatalf("ResolveBatch returned top-level error: %v", err)
	}
	if calls := tracker.listCallsFor(sourceSpec); calls != 1 {
		t.Fatalf("list calls = %d, want 1", calls)
	}
	for index, result := range results {
		if !errors.Is(result.Err(), wantErr) {
			t.Fatalf("result %d error = %v, want shared list failure", index, result.Err())
		}
		if result.Request().ID() != acquisition.RequestID([]string{"first", "second"}[index]) {
			t.Fatalf("result %d request id = %q", index, result.Request().ID())
		}
	}
}

func TestResolveBatchDoesNotDedupeDifferentOperationsForSameSource(t *testing.T) {
	tracker := newBatchResolverTracker(t)
	resolver := Resolver{local: tracker}
	sourceSpec := sourcetest.Local(t, "skills", source.LocalSourceModeVendor)

	results, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
		batchRequest("resolve", 0, acquisition.OperationResolve, sourceSpec),
		batchRequest("list", 1, acquisition.OperationListRoot, sourceSpec),
	}, acquisition.NewBatchOptions(2, nil))
	if err != nil {
		t.Fatalf("ResolveBatch returned error: %v", err)
	}
	assertBatchResultsOK(t, results)
	if calls := tracker.resolveCallsFor(sourceSpec); calls != 1 {
		t.Fatalf("resolve calls = %d, want 1", calls)
	}
	if calls := tracker.listCallsFor(sourceSpec); calls != 1 {
		t.Fatalf("list calls = %d, want 1", calls)
	}
}

func TestResolveBatchRequestErrorsDoNotCancelIndependentRequests(t *testing.T) {
	wantErr := errors.New("resolve failed")
	failingSource := sourcetest.Local(t, "skills/fail", source.LocalSourceModeVendor)
	tracker := newBatchResolverTracker(t)
	tracker.resolveErrBySourceID[mustSourceID(t, failingSource)] = wantErr
	resolver := Resolver{local: tracker}

	results, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
		batchRequest("fail", 0, acquisition.OperationResolve, failingSource),
		batchRequest("ok", 1, acquisition.OperationResolve, sourcetest.Local(t, "skills/ok", source.LocalSourceModeVendor)),
	}, acquisition.NewBatchOptions(2, nil))
	if err != nil {
		t.Fatalf("ResolveBatch returned top-level error: %v", err)
	}
	if !errors.Is(results[0].Err(), wantErr) {
		t.Fatalf("first result error = %v, want resolve failure", results[0].Err())
	}
	if results[1].Err() != nil {
		t.Fatalf("second result error = %v, want nil", results[1].Err())
	}
	if _, ok := results[1].Resolution(); !ok {
		t.Fatalf("second result = %#v, want resolution", results[1])
	}
}

func TestResolveBatchCancellationStopsBlockedOwners(t *testing.T) {
	release := make(chan struct{})
	tracker := newBatchResolverTracker(t)
	tracker.release = release
	tracker.started = make(chan string, 2)
	resolver := Resolver{local: tracker}
	recorder := newSourceEventRecorder()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	before := runtime.NumGoroutine()
	go func() {
		_, err := resolver.ResolveBatch(ctx, []acquisition.Request{
			batchRequest("first", 0, acquisition.OperationResolve, sourcetest.Local(t, "skills/first", source.LocalSourceModeVendor)),
			batchRequest("second", 1, acquisition.OperationResolve, sourcetest.Local(t, "skills/second", source.LocalSourceModeVendor)),
		}, acquisition.NewBatchOptions(2, recorder.sink))
		done <- err
	}()

	waitForBatchStarts(t, tracker.started, 2)
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolveBatch error = %v, want context.Canceled", err)
	}
	close(release)
	assertNoGoroutineGrowth(t, before)

	events := recorder.snapshot()
	assertSourceEventCount(t, events, acquisition.EventStarted, 2)
	assertSourceEventCount(t, events, acquisition.EventCompleted, 0)
	assertSourceEventCount(t, events, acquisition.EventFailed, 0)
}

func TestResolveBatchCancellationWithDuplicateRequestsStartsOnlyOwner(t *testing.T) {
	release := make(chan struct{})
	tracker := newBatchResolverTracker(t)
	tracker.release = release
	tracker.started = make(chan string, 1)
	resolver := Resolver{local: tracker}
	ctx, cancel := context.WithCancel(context.Background())
	sourceSpec := sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)

	done := make(chan error, 1)
	before := runtime.NumGoroutine()
	go func() {
		_, err := resolver.ResolveBatch(ctx, []acquisition.Request{
			batchRequest("first", 0, acquisition.OperationResolve, sourceSpec),
			batchRequest("second", 1, acquisition.OperationResolve, sourceSpec),
			batchRequest("third", 2, acquisition.OperationResolve, sourceSpec),
		}, acquisition.NewBatchOptions(4, nil))
		done <- err
	}()

	waitForBatchStarts(t, tracker.started, 1)
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolveBatch error = %v, want context.Canceled", err)
	}
	close(release)
	if calls := tracker.resolveCallsFor(sourceSpec); calls != 1 {
		t.Fatalf("resolve calls = %d, want only one owner call", calls)
	}
	assertNoGoroutineGrowth(t, before)
}

func TestResolveBatchCancellationBeforeLaunchReturnsContextError(t *testing.T) {
	tracker := newBatchResolverTracker(t)
	resolver := Resolver{local: tracker}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := resolver.ResolveBatch(ctx, []acquisition.Request{
		batchRequest("request", 0, acquisition.OperationResolve, sourcetest.Local(t, "skills/demo", source.LocalSourceModeVendor)),
	}, acquisition.NewBatchOptions(1, nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolveBatch error = %v, want context.Canceled", err)
	}
	if calls := tracker.totalCalls(); calls != 0 {
		t.Fatalf("backend calls = %d, want 0", calls)
	}
}

func TestResolveBatchUnsupportedS3ListRootIsRequestError(t *testing.T) {
	baseResolver := newFakeResolver(t)
	resolver := Resolver{
		local: newBatchResolverTracker(t),
		git:   newBatchResolverTracker(t),
		s3:    baseResolver,
	}

	results, err := resolver.ResolveBatch(context.Background(), []acquisition.Request{
		batchRequest("s3-list", 0, acquisition.OperationListRoot, sourcetest.S3(t, "s3://bucket/key.tar.gz", "", "", source.S3ObjectFormatTarGzip)),
	}, acquisition.NewBatchOptions(1, nil))
	if err != nil {
		t.Fatalf("ResolveBatch returned top-level error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Err() == nil || !strings.Contains(results[0].Err().Error(), "S3 skill groups are unsupported") {
		t.Fatalf("result error = %v, want unsupported S3 skill group diagnostic", results[0].Err())
	}
}
