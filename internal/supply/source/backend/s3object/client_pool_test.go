package s3object

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientPoolCollapsesConcurrentConstructionByConfiguration(t *testing.T) {
	var factoryCalls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	want := &fakeS3Client{}
	pool := newClientPool(func(
		ctx context.Context,
		_ clientConfiguration,
	) (client, error) {
		if factoryCalls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return want, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	const callerCount = 12
	results := make(chan client, callerCount)
	errs := make(chan error, callerCount)
	var waitGroup sync.WaitGroup
	for range callerCount {
		waitGroup.Go(func() {
			resolved, err := pool.get(
				context.Background(),
				clientConfiguration{region: "us-east-1"},
			)
			results <- resolved
			errs <- err
		})
	}
	<-started
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("factory calls before release = %d, want 1", got)
	}
	close(release)
	waitGroup.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("clientPool.get returned error: %v", err)
		}
	}
	for resolved := range results {
		if resolved != want {
			t.Fatalf("clientPool.get returned %T %p, want shared %p", resolved, resolved, want)
		}
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("factory calls = %d, want 1", got)
	}
}

func TestClientPoolSeparatesRegionsAndResolverEpochs(t *testing.T) {
	var factoryCalls atomic.Int32
	factory := func(
		context.Context,
		clientConfiguration,
	) (client, error) {
		factoryCalls.Add(1)
		return &fakeS3Client{}, nil
	}
	firstEpoch := newClientPool(factory)
	for _, configuration := range []clientConfiguration{
		{region: "us-east-1"},
		{region: "us-west-2"},
		{region: "us-east-1"},
	} {
		if _, err := firstEpoch.get(context.Background(), configuration); err != nil {
			t.Fatalf("first epoch get(%q): %v", configuration.region, err)
		}
	}
	if got := factoryCalls.Load(); got != 2 {
		t.Fatalf("first epoch factory calls = %d, want one per region", got)
	}

	secondEpoch := newClientPool(factory)
	if _, err := secondEpoch.get(
		context.Background(),
		clientConfiguration{region: "us-east-1"},
	); err != nil {
		t.Fatalf("second epoch get: %v", err)
	}
	if got := factoryCalls.Load(); got != 3 {
		t.Fatalf("factory calls after new resolver epoch = %d, want 3", got)
	}
}

func TestClientPoolDoesNotRetainFailureOrCanceledWaiter(t *testing.T) {
	t.Run("factory failure", func(t *testing.T) {
		var factoryCalls atomic.Int32
		want := &fakeS3Client{}
		pool := newClientPool(func(
			context.Context,
			clientConfiguration,
		) (client, error) {
			if factoryCalls.Add(1) == 1 {
				return nil, errors.New("transient config failure")
			}
			return want, nil
		})
		configuration := clientConfiguration{region: "us-east-1"}
		if _, err := pool.get(context.Background(), configuration); err == nil {
			t.Fatal("first clientPool.get unexpectedly succeeded")
		}
		resolved, err := pool.get(context.Background(), configuration)
		if err != nil {
			t.Fatalf("retry clientPool.get returned error: %v", err)
		}
		if resolved != want || factoryCalls.Load() != 2 {
			t.Fatalf(
				"retry result/factory calls = %p/%d, want %p/2",
				resolved,
				factoryCalls.Load(),
				want,
			)
		}
	})

	t.Run("constructing caller cancellation", func(t *testing.T) {
		var factoryCalls atomic.Int32
		started := make(chan struct{})
		want := &fakeS3Client{}
		pool := newClientPool(func(
			ctx context.Context,
			_ clientConfiguration,
		) (client, error) {
			if factoryCalls.Add(1) == 1 {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return want, nil
		})
		configuration := clientConfiguration{region: "us-east-1"}
		constructingContext, cancel := context.WithCancel(t.Context())
		firstDone := make(chan error, 1)
		go func() {
			_, err := pool.get(constructingContext, configuration)
			firstDone <- err
		}()
		<-started
		cancel()
		if err := <-firstDone; !errors.Is(err, context.Canceled) {
			t.Fatalf("constructing caller error = %v, want context canceled", err)
		}

		resolved, err := pool.get(context.Background(), configuration)
		if err != nil {
			t.Fatalf("retry after constructing caller cancellation: %v", err)
		}
		if resolved != want || factoryCalls.Load() != 2 {
			t.Fatalf(
				"retry result/factory calls = %p/%d, want %p/2",
				resolved,
				factoryCalls.Load(),
				want,
			)
		}
	})

	t.Run("canceled waiter", func(t *testing.T) {
		var factoryCalls atomic.Int32
		started := make(chan struct{})
		release := make(chan struct{})
		want := &fakeS3Client{}
		pool := newClientPool(func(
			ctx context.Context,
			_ clientConfiguration,
		) (client, error) {
			factoryCalls.Add(1)
			close(started)
			select {
			case <-release:
				return want, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
		configuration := clientConfiguration{region: "us-east-1"}
		firstDone := make(chan error, 1)
		go func() {
			_, err := pool.get(context.Background(), configuration)
			firstDone <- err
		}()
		<-started

		waiterContext, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		if _, err := pool.get(waiterContext, configuration); !errors.Is(
			err,
			context.DeadlineExceeded,
		) {
			t.Fatalf("canceled waiter error = %v, want deadline exceeded", err)
		}
		close(release)
		if err := <-firstDone; err != nil {
			t.Fatalf("constructing caller returned error: %v", err)
		}
		resolved, err := pool.get(context.Background(), configuration)
		if err != nil {
			t.Fatalf("cached get returned error: %v", err)
		}
		if resolved != want || factoryCalls.Load() != 1 {
			t.Fatalf(
				"cached result/factory calls = %p/%d, want %p/1",
				resolved,
				factoryCalls.Load(),
				want,
			)
		}
	})
}
