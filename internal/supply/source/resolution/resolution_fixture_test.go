package resolution

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

var noOperationOptions acquisition.OperationOptions

func mustGitSource(t *testing.T, locator string, repositoryPath string, ref string) source.Source {
	t.Helper()

	sourceSpec, err := source.NewGitSource(locator, repositoryPath, ref)
	if err != nil {
		t.Fatalf("NewGitSource returned error: %v", err)
	}
	return sourceSpec
}

type fakeResolver struct {
	view        access.View
	contentHash artifact.ContentHash
}

func newFakeResolver(t *testing.T) fakeResolver {
	view, contentHash := newResolutionTestView(t)
	return fakeResolver{view: view, contentHash: contentHash}
}

func (resolver fakeResolver) Resolve(
	_ context.Context,
	sourceSpec source.Source,
	_ acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	return resolver.resolution(sourceSpec)
}

func (resolver fakeResolver) resolution(sourceSpec source.Source) (acquisition.Resolution, error) {
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	identity, err := artifact.NewExactIdentity(
		sourceID,
		resolutionTestRef(sourceSpec),
		resolver.view.Kind(),
		resolver.contentHash,
	)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	return acquisition.NewResolution(sourceSpec, identity, resolver.view)
}

type fakeRootResolver struct {
	fakeResolver
	name string
}

func (resolver fakeRootResolver) ListSourceRoot(
	_ context.Context,
	sourceSpec source.Source,
	_ acquisition.OperationOptions,
) (source.RootListing, error) {
	return source.NewRootListing(sourceSpec, resolutionTestRef(sourceSpec), artifact.ArtifactKindDirectory, []string{resolver.name + "-root"})
}

type batchResolverTracker struct {
	mu sync.Mutex

	resolveCallsBySourceID map[artifact.SourceID]int
	listCallsBySourceID    map[artifact.SourceID]int
	resolveErrBySourceID   map[artifact.SourceID]error
	listErrBySourceID      map[artifact.SourceID]error
	releaseBySourceID      map[artifact.SourceID]<-chan struct{}

	callLog []string
	active  int
	maxSeen int

	started chan string
	done    chan string
	release <-chan struct{}
	view    access.View
	hash    artifact.ContentHash
}

func newBatchResolverTracker(t *testing.T) *batchResolverTracker {
	t.Helper()
	view, contentHash := newResolutionTestView(t)
	return &batchResolverTracker{
		resolveCallsBySourceID: make(map[artifact.SourceID]int),
		listCallsBySourceID:    make(map[artifact.SourceID]int),
		resolveErrBySourceID:   make(map[artifact.SourceID]error),
		listErrBySourceID:      make(map[artifact.SourceID]error),
		releaseBySourceID:      make(map[artifact.SourceID]<-chan struct{}),
		view:                   view,
		hash:                   contentHash,
	}
}

func (resolver *batchResolverTracker) Resolve(
	ctx context.Context,
	sourceSpec source.Source,
	_ acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	localSource, _ := sourceSpec.Local()
	if err := resolver.begin(ctx, acquisition.OperationResolve, sourceID, localSource.Path()); err != nil {
		return acquisition.Resolution{}, err
	}
	defer resolver.finish(acquisition.OperationResolve, localSource.Path())

	resolver.mu.Lock()
	callErr := resolver.resolveErrBySourceID[sourceID]
	resolver.mu.Unlock()
	if callErr != nil {
		return acquisition.Resolution{}, callErr
	}

	identity, err := artifact.NewExactIdentity(
		sourceID,
		"",
		artifact.ArtifactKindDirectory,
		resolver.hash,
	)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	return acquisition.NewResolution(sourceSpec, identity, resolver.view)
}

func (resolver *batchResolverTracker) ListSourceRoot(
	ctx context.Context,
	sourceSpec source.Source,
	_ acquisition.OperationOptions,
) (source.RootListing, error) {
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return source.RootListing{}, err
	}
	localSource, _ := sourceSpec.Local()
	if err := resolver.begin(ctx, acquisition.OperationListRoot, sourceID, localSource.Path()); err != nil {
		return source.RootListing{}, err
	}
	defer resolver.finish(acquisition.OperationListRoot, localSource.Path())

	resolver.mu.Lock()
	callErr := resolver.listErrBySourceID[sourceID]
	resolver.mu.Unlock()
	if callErr != nil {
		return source.RootListing{}, callErr
	}

	return source.NewRootListing(sourceSpec, "", artifact.ArtifactKindDirectory, []string{"listed"})
}

func resolutionTestRef(sourceSpec source.Source) artifact.ResolvedRef {
	switch sourceSpec.Kind() {
	case source.SourceKindGit:
		return artifact.ResolvedRef(strings.Repeat("a", 40))
	case source.SourceKindS3:
		s3Source, _ := sourceSpec.S3()
		return artifact.ResolvedRef(s3Source.VersionID())
	default:
		return ""
	}
}

func (resolver *batchResolverTracker) begin(ctx context.Context, operation acquisition.Operation, sourceID artifact.SourceID, path string) error {
	resolver.mu.Lock()
	switch operation {
	case acquisition.OperationResolve:
		resolver.resolveCallsBySourceID[sourceID]++
	case acquisition.OperationListRoot:
		resolver.listCallsBySourceID[sourceID]++
	}
	resolver.active++
	if resolver.active > resolver.maxSeen {
		resolver.maxSeen = resolver.active
	}
	resolver.callLog = append(resolver.callLog, fmt.Sprintf("%s:%s", operation, path))
	started := resolver.started
	release := resolver.release
	sourceRelease := resolver.releaseBySourceID[sourceID]
	resolver.mu.Unlock()

	if started != nil {
		select {
		case started <- fmt.Sprintf("%s:%s", operation, path):
		case <-ctx.Done():
			resolver.decrementActive()
			return ctx.Err()
		}
	}
	if sourceRelease != nil {
		release = sourceRelease
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			resolver.decrementActive()
			return ctx.Err()
		}
	}

	return nil
}

func (resolver *batchResolverTracker) finish(operation acquisition.Operation, path string) {
	resolver.decrementActive()
	done := resolver.done
	if done == nil {
		return
	}

	done <- fmt.Sprintf("%s:%s", operation, path)
}

func (resolver *batchResolverTracker) decrementActive() {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()

	resolver.active--
}

func (resolver *batchResolverTracker) maxActive() int {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()

	return resolver.maxSeen
}

func (resolver *batchResolverTracker) callLogSnapshot() []string {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()

	return append([]string(nil), resolver.callLog...)
}

func (resolver *batchResolverTracker) totalCalls() int {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()

	total := 0
	for _, count := range resolver.resolveCallsBySourceID {
		total += count
	}
	for _, count := range resolver.listCallsBySourceID {
		total += count
	}

	return total
}

func (resolver *batchResolverTracker) resolveCallsFor(sourceSpec source.Source) int {
	sourceID := mustSourceID(nil, sourceSpec)
	resolver.mu.Lock()
	defer resolver.mu.Unlock()

	return resolver.resolveCallsBySourceID[sourceID]
}

func (resolver *batchResolverTracker) listCallsFor(sourceSpec source.Source) int {
	sourceID := mustSourceID(nil, sourceSpec)
	resolver.mu.Lock()
	defer resolver.mu.Unlock()

	return resolver.listCallsBySourceID[sourceID]
}

func batchRequest(id acquisition.RequestID, ordinal int, operation acquisition.Operation, sourceSpec source.Source) acquisition.Request {
	request, err := acquisition.NewRequest(id, ordinal, operation, sourceSpec)
	if err != nil {
		panic(fmt.Sprintf("construct test source acquisition request: %v", err))
	}
	return request
}

func assertBatchResultsOK(t *testing.T, results []acquisition.Result) {
	t.Helper()

	for index, result := range results {
		if result.Err() != nil {
			t.Fatalf("result %d error = %v", index, result.Err())
		}
		switch result.Request().Operation() {
		case acquisition.OperationResolve:
			if _, ok := result.Listing(); ok {
				t.Fatalf("result %d unexpectedly carries a listing", index)
			}
			resolution, ok := result.Resolution()
			if !ok || resolution.Identity().SourceID() == "" {
				t.Fatalf("result %d = %#v, want resolution", index, result)
			}
		case acquisition.OperationListRoot:
			if _, ok := result.Resolution(); ok {
				t.Fatalf("result %d unexpectedly carries a resolution", index)
			}
			listing, ok := result.Listing()
			if !ok || listing.Kind() == "" {
				t.Fatalf("result %d = %#v, want source root listing", index, result)
			}
		}
	}
}

func mustSourceID(t *testing.T, sourceSpec source.Source) artifact.SourceID {
	if t != nil {
		t.Helper()
	}

	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		if t == nil {
			panic(err)
		}
		t.Fatalf("SourceIDFor returned error: %v", err)
	}

	return sourceID
}

func waitForBatchStarts(t *testing.T, started <-chan string, count int) {
	t.Helper()

	for range count {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for batch call to start")
		}
	}
}

func waitForBatchDone(t *testing.T, done <-chan string) string {
	t.Helper()

	select {
	case value := <-done:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for batch call to finish")
		return ""
	}
}

func assertNoGoroutineGrowth(t *testing.T, before int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine count grew from %d to %d", before, runtime.NumGoroutine())
}

type sourceEventRecorder struct {
	mu     sync.Mutex
	events []acquisition.Event
}

func newSourceEventRecorder() *sourceEventRecorder {
	return &sourceEventRecorder{}
}

func (recorder *sourceEventRecorder) sink(event acquisition.Event) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	recorder.events = append(recorder.events, event)
}

func (recorder *sourceEventRecorder) snapshot() []acquisition.Event {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	return append([]acquisition.Event(nil), recorder.events...)
}

func filterSourceEvents(events []acquisition.Event, kind acquisition.EventKind) []acquisition.Event {
	filtered := make([]acquisition.Event, 0)
	for _, event := range events {
		if event.Kind() == kind {
			filtered = append(filtered, event)
		}
	}

	return filtered
}

func assertSourceEventCount(t *testing.T, events []acquisition.Event, kind acquisition.EventKind, want int) {
	t.Helper()

	if got := len(filterSourceEvents(events, kind)); got != want {
		t.Fatalf("%s event count = %d, want %d; events=%#v", kind, got, want, events)
	}
}

func assertSourceEventIDs(t *testing.T, events []acquisition.Event, kind acquisition.EventKind, want []acquisition.RequestID) {
	t.Helper()

	filtered := filterSourceEvents(events, kind)
	got := make([]acquisition.RequestID, 0, len(filtered))
	for _, event := range filtered {
		got = append(got, event.Request().ID())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s event request ids = %#v, want %#v; events=%#v", kind, got, want, events)
	}
}
