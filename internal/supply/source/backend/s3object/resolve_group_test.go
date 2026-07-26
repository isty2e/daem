package s3object

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func TestResolutionGroupRejectsInvalidInputs(t *testing.T) {
	sourceID := artifact.SourceID("s3:source")
	var group resolutionGroup
	if _, err := group.do(t.Context(), "", func(context.Context) (acquisition.Resolution, error) {
		t.Fatal("invalid source group function ran")
		return acquisition.Resolution{}, nil
	}); err == nil {
		t.Fatal("resolutionGroup.do accepted an empty source id")
	}
	if _, err := group.do(nil, sourceID, func(context.Context) (acquisition.Resolution, error) {
		t.Fatal("nil-context group function ran")
		return acquisition.Resolution{}, nil
	}); err == nil {
		t.Fatal("resolutionGroup.do accepted a nil context")
	}
	if _, err := group.do(t.Context(), sourceID, nil); err == nil {
		t.Fatal("resolutionGroup.do accepted a nil function")
	}
}

func TestResolutionGroupSharesSameSourceWork(t *testing.T) {
	sourceID := artifact.SourceID("s3:shared")
	var group resolutionGroup
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	joined := make(chan struct{}, 8)
	ownerDone := make(chan error, 1)

	go func() {
		_, err := group.do(context.Background(), sourceID, func(context.Context) (acquisition.Resolution, error) {
			calls.Add(1)
			close(started)
			<-release
			return acquisition.Resolution{}, nil
		})
		ownerDone <- err
	}()
	<-started

	waiterDone := make(chan error, 8)
	for range 8 {
		go func() {
			ctx := &resolutionGroupWaitContext{Context: context.Background(), joined: joined}
			_, err := group.do(ctx, sourceID, func(context.Context) (acquisition.Resolution, error) {
				calls.Add(1)
				return acquisition.Resolution{}, nil
			})
			waiterDone <- err
		}()
	}
	for range 8 {
		waitForS3TestSignal(t, joined, "resolution group waiter join")
	}
	close(release)
	if err := <-ownerDone; err != nil {
		t.Fatalf("owner error = %v", err)
	}
	for range 8 {
		if err := <-waiterDone; err != nil {
			t.Fatalf("waiter error = %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("resolution calls = %d, want 1", calls.Load())
	}
}

func TestResolutionGroupRunsDifferentSourcesConcurrently(t *testing.T) {
	var group resolutionGroup
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan error, 2)

	for _, sourceID := range []artifact.SourceID{"s3:first", "s3:second"} {
		go func(sourceID artifact.SourceID) {
			_, err := group.do(context.Background(), sourceID, func(context.Context) (acquisition.Resolution, error) {
				started <- struct{}{}
				<-release
				return acquisition.Resolution{}, nil
			})
			done <- err
		}(sourceID)
	}
	waitForS3TestSignal(t, started, "first distinct resolution")
	waitForS3TestSignal(t, started, "second distinct resolution")
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("resolution error = %v", err)
		}
	}
}

func TestResolutionGroupWaiterCancellationDoesNotCancelOwner(t *testing.T) {
	sourceID := artifact.SourceID("s3:waiter-cancel")
	var group resolutionGroup
	started := make(chan struct{})
	release := make(chan struct{})
	ownerDone := make(chan error, 1)
	var releaseOnce sync.Once
	releaseOwner := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(releaseOwner)

	go func() {
		_, err := group.do(context.Background(), sourceID, func(context.Context) (acquisition.Resolution, error) {
			close(started)
			<-release
			return acquisition.Resolution{}, nil
		})
		ownerDone <- err
	}()
	waitForS3TestSignal(t, started, "resolution group owner start")

	waiterBaseContext, cancelWaiter := context.WithCancel(context.Background())
	waiterJoined := make(chan struct{}, 1)
	waiterContext := &resolutionGroupWaitContext{Context: waiterBaseContext, joined: waiterJoined}
	waiterDone := make(chan error, 1)
	go func() {
		_, err := group.do(waiterContext, sourceID, func(context.Context) (acquisition.Resolution, error) {
			t.Error("canceled waiter function ran")
			return acquisition.Resolution{}, nil
		})
		waiterDone <- err
	}()
	waitForS3TestSignal(t, waiterJoined, "resolution group waiter join")
	cancelWaiter()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter error = %v, want context.Canceled", err)
	}

	releaseOwner()
	if err := <-ownerDone; err != nil {
		t.Fatalf("owner error = %v", err)
	}

	var retries atomic.Int32
	if _, err := group.do(t.Context(), sourceID, func(context.Context) (acquisition.Resolution, error) {
		retries.Add(1)
		return acquisition.Resolution{}, nil
	}); err != nil {
		t.Fatalf("retry error = %v", err)
	}
	if retries.Load() != 1 {
		t.Fatalf("retry calls = %d, want 1", retries.Load())
	}
}

func TestResolutionGroupOwnerCancellationIsSharedButNotMemoized(t *testing.T) {
	sourceID := artifact.SourceID("s3:owner-cancel")
	var group resolutionGroup
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	ownerDone := make(chan error, 1)
	waiterJoined := make(chan struct{}, 1)
	waiterDone := make(chan error, 1)

	go func() {
		_, err := group.do(ctx, sourceID, func(ctx context.Context) (acquisition.Resolution, error) {
			close(started)
			<-ctx.Done()
			return acquisition.Resolution{}, ctx.Err()
		})
		ownerDone <- err
	}()
	<-started
	go func() {
		waiterContext := &resolutionGroupWaitContext{Context: context.Background(), joined: waiterJoined}
		_, err := group.do(waiterContext, sourceID, func(context.Context) (acquisition.Resolution, error) {
			t.Error("waiter function ran during owner cancellation")
			return acquisition.Resolution{}, nil
		})
		waiterDone <- err
	}()
	waitForS3TestSignal(t, waiterJoined, "owner-cancellation waiter join")
	cancel()
	if err := <-ownerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("owner error = %v, want context.Canceled", err)
	}
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter error = %v, want shared context.Canceled", err)
	}

	var retries atomic.Int32
	if _, err := group.do(t.Context(), sourceID, func(context.Context) (acquisition.Resolution, error) {
		retries.Add(1)
		return acquisition.Resolution{}, nil
	}); err != nil {
		t.Fatalf("retry error = %v", err)
	}
	if retries.Load() != 1 {
		t.Fatalf("retry calls = %d, want 1", retries.Load())
	}
}

type resolutionGroupWaitContext struct {
	context.Context
	joined chan<- struct{}
	once   sync.Once
}

func (ctx *resolutionGroupWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { ctx.joined <- struct{}{} })
	return ctx.Context.Done()
}
