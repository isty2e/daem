package s3object

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolveConcurrentSameSourceDedupeOneClientCall(t *testing.T) {
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	client := &fakeS3Client{
		body:      []byte("project instructions\n"),
		versionID: "v1",
		started:   started,
		release:   release,
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}

	sourceSpec := sourcetest.S3(t, "s3://daem/instructions/project.md", "v1", "", sourcepkg.S3ObjectFormatFile)
	results := resolveS3WithJoinedFollowers(t, resolver, resolver, sourceSpec, 8, started, release)

	for index, result := range results {
		if result.err != nil {
			t.Fatalf("Resolve %d returned error: %v", index, result.err)
		}
		if result.artifact != results[0].artifact {
			t.Fatalf("Resolve %d artifact = %#v, want %#v", index, result.artifact, results[0].artifact)
		}
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("GetObject calls = %d, want 1", calls)
	}
	if closes := client.closeCount(); closes != 1 {
		t.Fatalf("Body closes = %d, want 1", closes)
	}
}

func TestResolveCopiedResolverSharesInFlightDedupe(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	client := &fakeS3Client{
		body:      []byte("project instructions\n"),
		versionID: "v1",
		started:   started,
		release:   release,
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	copiedResolver := resolver
	sourceSpec := sourcetest.S3(t, "s3://daem/instructions/project.md", "v1", "", sourcepkg.S3ObjectFormatFile)

	results := resolveS3WithJoinedFollowers(t, resolver, copiedResolver, sourceSpec, 2, started, release)
	first := results[0]
	second := results[1]
	if first.err != nil || second.err != nil {
		t.Fatalf("Resolve errors = %v, %v", first.err, second.err)
	}
	if first.artifact != second.artifact {
		t.Fatalf("artifacts differ: %#v != %#v", first.artifact, second.artifact)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("GetObject calls = %d, want 1", calls)
	}
}

func TestResolveIndependentResolversSerializeSameFinalRoot(t *testing.T) {
	cacheRoot := t.TempDir()
	firstClient := &fakeS3Client{body: []byte("same\n"), versionID: "v1"}
	secondClient := &fakeS3Client{body: []byte("same\n"), versionID: "v1"}
	firstResolver, err := newResolverWithClient(cacheRoot, firstClient)
	if err != nil {
		t.Fatalf("first newResolverWithClient returned error: %v", err)
	}
	secondResolver, err := newResolverWithClient(cacheRoot, secondClient)
	if err != nil {
		t.Fatalf("second newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/instructions/project.md", "v1", "", sourcepkg.S3ObjectFormatFile)

	start := make(chan struct{})
	results := make(chan s3ResolveResult, 2)
	for _, currentResolver := range []Resolver{firstResolver, secondResolver} {
		go func(currentResolver Resolver) {
			<-start
			resolved, err := currentResolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
			results <- s3ResolveResult{artifact: resolved, err: err}
		}(currentResolver)
	}
	close(start)

	first := <-results
	second := <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("Resolve errors = %v, %v", first.err, second.err)
	}
	if !first.artifact.Identity().Equal(second.artifact.Identity()) {
		t.Fatalf("artifacts differ: %#v != %#v", first.artifact, second.artifact)
	}

	firstIdentity := first.artifact.Identity()
	entryRoot := firstResolver.artifactEntryRoot(
		firstIdentity.SourceID(),
		firstIdentity.ResolvedRef(),
		firstIdentity.ContentHash(),
	)
	if !s3EntryExists(entryRoot) {
		t.Fatalf("final entry %q was not published", entryRoot)
	}
	if calls := firstClient.callCount() + secondClient.callCount(); calls != 1 {
		t.Fatalf("total GetObject calls = %d, want one serialized immutable fill", calls)
	}
	assertNoS3TempEntries(t, firstResolver.artifactParent(firstIdentity.SourceID()))
}

func TestResolveNoVersionInFlightDedupeButSequentialResolveRefetches(t *testing.T) {
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	client := &fakeS3Client{
		bodies:  [][]byte{[]byte("first\n"), []byte("second\n")},
		started: started,
		release: release,
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/instructions/project.md", "", "", sourcepkg.S3ObjectFormatFile)

	results := resolveS3WithJoinedFollowers(t, resolver, resolver, sourceSpec, 4, started, release)
	for index, result := range results {
		if result.err != nil {
			t.Fatalf("Resolve %d returned error: %v", index, result.err)
		}
		if result.artifact != results[0].artifact {
			t.Fatalf("Resolve %d artifact = %#v, want %#v", index, result.artifact, results[0].artifact)
		}
		if result.artifact.Identity().ResolvedRef() != "" {
			t.Fatalf("Resolve %d ResolvedRef = %q, want empty", index, result.artifact.Identity().ResolvedRef())
		}
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("in-flight GetObject calls = %d, want 1", calls)
	}

	secondArtifact, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}
	if calls := client.callCount(); calls != 2 {
		t.Fatalf("sequential GetObject calls = %d, want 2", calls)
	}
	if secondArtifact.Identity().ContentHash() == results[0].artifact.Identity().ContentHash() {
		t.Fatalf("second ContentHash = %q, want changed content hash", secondArtifact.Identity().ContentHash())
	}
}

func TestResolveWaiterCancellationDoesNotCancelOwner(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	client := &fakeS3Client{
		body:      []byte("owner\n"),
		versionID: "v1",
		started:   started,
		release:   release,
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/instructions/project.md", "", "", sourcepkg.S3ObjectFormatFile)

	ownerDone := make(chan s3ResolveResult, 1)
	var releaseOnce sync.Once
	releaseOwner := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(releaseOwner)
	go func() {
		resolved, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
		ownerDone <- s3ResolveResult{artifact: resolved, err: err}
	}()
	waitForS3TestSignal(t, started, "owner GetObject start")

	waiterBaseContext, cancelWaiter := context.WithCancel(context.Background())
	waiterContext := &s3WaiterJoinContext{
		Context: waiterBaseContext,
		joined:  make(chan struct{}),
	}
	waiterDone := make(chan error, 1)
	go func() {
		_, err := resolver.Resolve(waiterContext, sourceSpec, noOperationOptions)
		waiterDone <- err
	}()
	waitForS3TestSignal(t, waiterContext.joined, "waiter in-flight join")
	cancelWaiter()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter Resolve error = %v, want context.Canceled", err)
	}

	releaseOwner()
	owner := <-ownerDone
	if owner.err != nil {
		t.Fatalf("owner Resolve returned error: %v", owner.err)
	}
	ownerIdentity := owner.artifact.Identity()
	if !s3EntryExists(resolver.artifactEntryRoot(
		ownerIdentity.SourceID(),
		ownerIdentity.ResolvedRef(),
		ownerIdentity.ContentHash(),
	)) {
		t.Fatalf("owner artifact was not published complete")
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("GetObject calls = %d, want one owner call", calls)
	}
}

func resolveS3WithJoinedFollowers(
	t *testing.T,
	leader Resolver,
	follower Resolver,
	sourceSpec sourcepkg.Source,
	workers int,
	started <-chan struct{},
	release chan struct{},
) []s3ResolveResult {
	t.Helper()
	if workers < 2 {
		t.Fatalf("workers = %d, want at least 2", workers)
	}

	var releaseOnce sync.Once
	releaseLeader := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(releaseLeader)

	results := make([]s3ResolveResult, workers)
	var waitGroup sync.WaitGroup
	waitGroup.Go(func() {
		resolved, err := leader.Resolve(context.Background(), sourceSpec, noOperationOptions)
		results[0] = s3ResolveResult{artifact: resolved, err: err}
	})
	waitForS3TestSignal(t, started, "leader GetObject start")

	joined := make([]<-chan struct{}, 0, workers-1)
	for worker := 1; worker < workers; worker++ {
		waiterJoined := make(chan struct{})
		joined = append(joined, waiterJoined)
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			ctx := &s3WaiterJoinContext{
				Context: context.Background(),
				joined:  waiterJoined,
			}
			resolved, err := follower.Resolve(ctx, sourceSpec, noOperationOptions)
			results[worker] = s3ResolveResult{artifact: resolved, err: err}
		}(worker)
	}
	for worker, waiterJoined := range joined {
		waitForS3TestSignal(t, waiterJoined, fmt.Sprintf("follower %d in-flight join", worker+1))
	}

	releaseLeader()
	workersDone := make(chan struct{})
	go func() {
		waitGroup.Wait()
		close(workersDone)
	}()
	waitForS3TestSignal(t, workersDone, "resolve workers to finish")

	return results
}

type s3WaiterJoinContext struct {
	context.Context
	joined chan struct{}
	once   sync.Once
}

func (ctx *s3WaiterJoinContext) Done() <-chan struct{} {
	// Resolve checks Err before Group.Do; Group first calls Done after this
	// follower has captured the existing in-flight call and entered its wait path.
	ctx.once.Do(func() { close(ctx.joined) })
	return ctx.Context.Done()
}
