package mutation

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestVisibilityObservationCoversDomainsWithFreshIndependentObservers(t *testing.T) {
	for _, count := range []int{0, 1, 127, 128, 513} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			domains, indices := visibilityObservationTestDomains(count)
			for index := range domains {
				if index%11 == 0 {
					domains[index].kind = domainHostRoute
				}
			}
			calls := make([]atomic.Int32, count)
			var factories atomic.Int32

			for epoch := 1; epoch <= 2; epoch++ {
				newObserver := func() pathIdentityObserver {
					factories.Add(1)
					var active atomic.Int32
					return func(path string, effect PathEffect) (canonicalPath, error) {
						if active.Add(1) != 1 {
							t.Error("observation cache shared by concurrent workers")
						}
						defer active.Add(-1)
						runtime.Gosched()
						calls[indices[path]].Add(1)
						return canonicalPath{
							keyPath: path, accessPath: path,
							witness: pathSemanticsWitness(fmt.Sprintf("%d-%d", epoch, effect)),
						}, nil
					}
				}
				observed, matches, err := observeVisibilityDomains(context.Background(), domains, newObserver)
				if err != nil || !matches || len(observed) != count {
					t.Fatalf("observation: length=%d matches=%t error=%v", len(observed), matches, err)
				}
				for index, domain := range domains {
					if domain.kind == domainHostRoute {
						if calls[index].Load() != 0 || observed[index].keyPath != "" {
							t.Fatalf("host route %d observed as a path", index)
						}
						continue
					}
					wantWitness := pathSemanticsWitness(fmt.Sprintf("%d-%d", epoch, domain.effect))
					if calls[index].Load() != int32(epoch) || observed[index].keyPath != domain.requestedPath ||
						observed[index].accessPath != domain.requestedPath || observed[index].witness != wantWitness {
						t.Fatalf("domain %d lost, repeated, reordered, or stale: %+v", index, observed[index])
					}
				}
			}

			wantFactories := 2
			if count >= 128 {
				wantFactories *= min(4, runtime.GOMAXPROCS(0))
			}
			if got := int(factories.Load()); got != wantFactories {
				t.Fatalf("observer instances = %d, want %d", got, wantFactories)
			}
		})
	}
}

func TestVisibilityObservationPreservesInputOrderWhenLaterFailureFinishesFirst(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	domains, indices := visibilityObservationTestDomains(256)
	earlier, later := errors.New("earlier domain"), errors.New("later domain")
	laterFinished := make(chan struct{})
	newObserver := func() pathIdentityObserver {
		return func(path string, _ PathEffect) (canonicalPath, error) {
			switch indices[path] {
			case 0:
				select {
				case <-laterFinished:
					return canonicalPath{}, earlier
				case <-ctx.Done():
					return canonicalPath{}, ctx.Err()
				}
			case 192:
				close(laterFinished)
				return canonicalPath{}, later
			default:
				return canonicalPath{keyPath: path}, nil
			}
		}
	}
	observed, matches, err := observeVisibilityDomains(ctx, domains, newObserver)
	if observed != nil || matches || !errors.Is(err, earlier) {
		t.Fatalf("failure precedence: observed=%v matches=%t error=%v", observed, matches, err)
	}
}

func TestVisibilityObservationMismatchWinsOverLaterError(t *testing.T) {
	for _, count := range []int{2, 256} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			domains, indices := visibilityObservationTestDomains(count)
			namespace := mustMutationTestCanonicalPath(t.TempDir())
			intent, err := newNamespaceLeaseIntent(namespace)
			if err != nil {
				t.Fatal(err)
			}
			intent.witness += "-changed"
			domains[0].namespaceLease = intent
			var firstObserved atomic.Bool
			newObserver := func() pathIdentityObserver {
				return func(path string, _ PathEffect) (canonicalPath, error) {
					if indices[path] == 0 {
						firstObserved.Store(true)
					}
					return canonicalPath{}, errors.New("later path failed")
				}
			}
			observed, matches, err := observeVisibilityDomains(context.Background(), domains, newObserver)
			if observed != nil || matches || err != nil || firstObserved.Load() {
				t.Fatalf("namespace refusal: observed=%v matches=%t error=%v first observed=%t", observed, matches, err, firstObserved.Load())
			}
		})
	}
}

func TestVisibilityObservationCancellationDrainsEveryStartedRead(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	domains, _ := visibilityObservationTestDomains(256)
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	finished := make(chan struct{})
	var releaseOnce sync.Once
	var active atomic.Int32
	var calls atomic.Int32
	newObserver := func() pathIdentityObserver {
		return func(string, PathEffect) (canonicalPath, error) {
			active.Add(1)
			defer active.Add(-1)
			calls.Add(1)
			started <- struct{}{}
			<-release
			return canonicalPath{}, ctx.Err()
		}
	}
	type result struct {
		observed []canonicalPath
		matches  bool
		err      error
	}
	resultChannel := make(chan result, 1)
	go func() {
		defer close(finished)
		observed, matches, err := observeVisibilityDomains(ctx, domains, newObserver)
		resultChannel <- result{observed, matches, err}
	}()
	defer func() {
		releaseOnce.Do(func() { close(release) })
		<-finished
	}()

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for range 4 {
		select {
		case <-started:
		case <-deadline.C:
			t.Fatal("observation workers did not start")
		}
	}
	cancel()
	select {
	case <-finished:
		t.Fatal("returned with reads still blocked")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	got := <-resultChannel
	if got.observed != nil || got.matches || !errors.Is(got.err, context.Canceled) || active.Load() != 0 || calls.Load() != 4 {
		t.Fatalf("cancellation: matches=%t error=%v active=%d calls=%d", got.matches, got.err, active.Load(), calls.Load())
	}
}

func visibilityObservationTestDomains(count int) ([]Domain, map[string]int) {
	domains := make([]Domain, count)
	indices := make(map[string]int, count)
	for index := range domains {
		path := fmt.Sprintf("/observation/%d", index)
		effect := PathEffectDirectoryEntry
		if index%2 != 0 {
			effect = PathEffectReferent
		}
		domains[index] = Domain{kind: domainLogicalPath, requestedPath: path, effect: effect}
		indices[path] = index
	}
	return domains, indices
}
