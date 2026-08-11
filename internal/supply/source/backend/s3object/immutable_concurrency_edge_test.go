package s3object

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolveExactVersionManyIndependentReadersUseOnePersistentArtifact(t *testing.T) {
	cacheRoot := t.TempDir()
	seedClient := &fakeS3Client{body: []byte("immutable\n"), versionID: "v1"}
	seedResolver, err := newResolverWithClient(cacheRoot, seedClient)
	if err != nil {
		t.Fatal(err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/object", "v1", "", sourcepkg.S3ObjectFormatFile)
	want := mustResolveS3(t, seedResolver, sourceSpec)

	const readerCount = 24
	resolvers := make([]Resolver, readerCount)
	var factoryCalls atomic.Int64
	for index := range resolvers {
		resolvers[index], err = newResolverWithClientFactory(cacheRoot, func(context.Context, clientConfiguration) (client, error) {
			factoryCalls.Add(1)
			return nil, errors.New("persistent hit unexpectedly requested a client")
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	results := make(chan s3ResolveResult, readerCount)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for _, resolver := range resolvers {
		go func(resolver Resolver) {
			<-start
			resolved, resolveErr := resolver.Resolve(ctx, sourceSpec, noOperationOptions)
			results <- s3ResolveResult{artifact: resolved, err: resolveErr}
		}(resolver)
	}
	close(start)
	for index := range readerCount {
		result := <-results
		if result.err != nil || result.artifact != want {
			t.Fatalf("reader %d = %#v, %v, want %#v", index, result.artifact, result.err, want)
		}
	}
	if calls := factoryCalls.Load(); calls != 0 {
		t.Fatalf("client factory calls = %d, want no network path", calls)
	}
	if calls := seedClient.callCount(); calls != 1 {
		t.Fatalf("seed GetObject calls = %d, want one", calls)
	}
}

func TestResolveDifferentExactVersionsCanFillInParallel(t *testing.T) {
	cacheRoot := t.TempDir()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	firstClient := &fakeS3Client{
		body:      []byte("version one\n"),
		versionID: "v1",
		started:   started,
		release:   release,
	}
	secondClient := &fakeS3Client{
		body:      []byte("version two\n"),
		versionID: "v2",
		started:   started,
		release:   release,
	}
	firstResolver, err := newResolverWithClient(cacheRoot, firstClient)
	if err != nil {
		t.Fatal(err)
	}
	secondResolver, err := newResolverWithClient(cacheRoot, secondClient)
	if err != nil {
		t.Fatal(err)
	}
	firstSource := sourcetest.S3(t, "s3://daem/object", "v1", "", sourcepkg.S3ObjectFormatFile)
	secondSource := sourcetest.S3(t, "s3://daem/object", "v2", "", sourcepkg.S3ObjectFormatFile)
	results := make(chan s3ResolveResult, 2)
	go func() {
		resolved, resolveErr := firstResolver.Resolve(context.Background(), firstSource, noOperationOptions)
		results <- s3ResolveResult{artifact: resolved, err: resolveErr}
	}()
	go func() {
		resolved, resolveErr := secondResolver.Resolve(context.Background(), secondSource, noOperationOptions)
		results <- s3ResolveResult{artifact: resolved, err: resolveErr}
	}()

	waitForS3TestSignal(t, started, "first exact-version GetObject start")
	waitForS3TestSignal(t, started, "second exact-version GetObject start")
	releaseOnce.Do(func() { close(release) })
	first := <-results
	second := <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("parallel Resolve errors = %v, %v", first.err, second.err)
	}
	if first.artifact.Identity().SourceID() == second.artifact.Identity().SourceID() ||
		first.artifact.Identity().ContentHash() == second.artifact.Identity().ContentHash() {
		t.Fatalf("parallel exact versions collided: first=%#v second=%#v", first.artifact, second.artifact)
	}
	if firstClient.callCount() != 1 || secondClient.callCount() != 1 {
		t.Fatalf("GetObject calls = %d/%d, want one per version", firstClient.callCount(), secondClient.callCount())
	}
}

func TestResolveFailedExactVersionOwnerAllowsLaterFiller(t *testing.T) {
	cacheRoot := t.TempDir()
	sourceSpec := sourcetest.S3(t, "s3://daem/object", "v1", "", sourcepkg.S3ObjectFormatFile)
	remoteErr := errors.New("initial fetch failed")
	failingClient := &fakeS3Client{err: remoteErr}
	failingResolver, err := newResolverWithClient(cacheRoot, failingClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failingResolver.Resolve(t.Context(), sourceSpec, noOperationOptions); !errors.Is(err, remoteErr) {
		t.Fatalf("initial Resolve error = %v, want remote failure", err)
	}
	sourceID := mustS3SourceID(t, sourceSpec)
	identity := mustImmutableLookupIdentity(t, sourceID, "v1")
	cacheAuthority := mustCaptureS3CacheRoot(t, failingResolver)
	if _, found, err := failingResolver.state.immutableIndex.read(t.Context(), cacheAuthority, identity); err != nil || found {
		t.Fatalf("row after failed owner = found %t, error %v", found, err)
	}
	if err := cacheAuthority.Close(); err != nil {
		t.Fatalf("close cache authority: %v", err)
	}

	goodClient := &fakeS3Client{body: []byte("recovered\n"), versionID: "v1"}
	goodResolver, err := newResolverWithClient(cacheRoot, goodClient)
	if err != nil {
		t.Fatal(err)
	}
	want := mustResolveS3(t, goodResolver, sourceSpec)
	probeClient := &fakeS3Client{err: errors.New("post-fill network path used")}
	probeResolver, err := newResolverWithClient(cacheRoot, probeClient)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustResolveS3(t, probeResolver, sourceSpec); got != want {
		t.Fatalf("post-fill artifact = %#v, want %#v", got, want)
	}
	if failingClient.callCount() != 1 || goodClient.callCount() != 1 || probeClient.callCount() != 0 {
		t.Fatalf("GetObject calls = failed:%d filler:%d probe:%d", failingClient.callCount(), goodClient.callCount(), probeClient.callCount())
	}
}

func TestResolveRejectsInvalidIndexNamespaceInfrastructure(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "lookup lock root is a file", setup: func(t *testing.T, cacheRoot string) {
			root := filepath.Join(cacheRoot, "locks")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "s3-immutable"), []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "lookup index root is a file", setup: func(t *testing.T, cacheRoot string) {
			root := filepath.Join(cacheRoot, "indexes")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "s3-immutable"), []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cacheRoot := t.TempDir()
			test.setup(t, cacheRoot)
			client := &fakeS3Client{body: []byte("available\n"), versionID: "v1"}
			resolver, err := newResolverWithClient(cacheRoot, client)
			if err != nil {
				t.Fatal(err)
			}
			sourceSpec := sourcetest.S3(t, "s3://daem/object", "v1", "", sourcepkg.S3ObjectFormatFile)

			_, firstErr := resolver.Resolve(t.Context(), sourceSpec, noOperationOptions)
			if firstErr == nil {
				t.Fatal("Resolve succeeded with invalid immutable-index namespace")
			}
			if calls := client.callCount(); calls != 0 {
				t.Fatalf("GetObject calls = %d, want no network work", calls)
			}
		})
	}
}

func TestResolvePersistentHitEmitsOnlyCacheWaitAndCacheHit(t *testing.T) {
	cacheRoot := t.TempDir()
	seedClient := &fakeS3Client{body: []byte("immutable\n"), versionID: "v1"}
	seedResolver, err := newResolverWithClient(cacheRoot, seedClient)
	if err != nil {
		t.Fatal(err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/object", "v1", "", sourcepkg.S3ObjectFormatFile)
	want := mustResolveS3(t, seedResolver, sourceSpec)
	var factoryCalls atomic.Int64
	resolver, err := newResolverWithClientFactory(cacheRoot, func(context.Context, clientConfiguration) (client, error) {
		factoryCalls.Add(1)
		return nil, errors.New("cache hit unexpectedly requested a client")
	})
	if err != nil {
		t.Fatal(err)
	}
	events := make([]acquisition.Event, 0, 2)
	request, err := acquisition.NewRequest("edge:000001", 1, acquisition.OperationResolve, sourceSpec)
	if err != nil {
		t.Fatal(err)
	}
	options, err := acquisition.NewOperationOptions(request, func(event acquisition.Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolver.Resolve(t.Context(), sourceSpec, options)
	if err != nil || got != want {
		t.Fatalf("persistent Resolve = %#v, %v, want %#v", got, err, want)
	}
	wantKinds := []acquisition.EventKind{acquisition.EventCacheWait, acquisition.EventCacheHit}
	if len(events) != len(wantKinds) {
		t.Fatalf("event count = %d, want %d: %#v", len(events), len(wantKinds), events)
	}
	for index, event := range events {
		if event.Kind() != wantKinds[index] ||
			event.SourceID() != want.Identity().SourceID() ||
			event.ResolvedRef() != want.Identity().ResolvedRef() {
			t.Fatalf("event %d = %#v, want kind %q and artifact identity", index, event, wantKinds[index])
		}
	}
	if calls := factoryCalls.Load(); calls != 0 {
		t.Fatalf("client factory calls = %d, want zero", calls)
	}
}
