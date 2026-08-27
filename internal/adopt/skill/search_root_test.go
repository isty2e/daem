package skill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSearchRootCacheAcceptsExactEntryAndNameLimits(t *testing.T) {
	root := t.TempDir()
	cache := mustSearchRootCache(t, searchRootObserverForNames("bb", "aa"), 2, 4, 2)

	entries, err := cache.entries(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(entries); got != "[aa bb]" {
		t.Fatalf("entries = %s, want [aa bb]", got)
	}
}

func TestSearchRootCacheStopsAtFirstEntryOverflow(t *testing.T) {
	root := t.TempDir()
	visited := 0
	cache := mustSearchRootCache(t, func(_ context.Context, _ string, visit func(string) error) error {
		for _, name := range []string{"aa", "bb", "cc", "dd"} {
			visited++
			if err := visit(name); err != nil {
				return err
			}
		}
		return nil
	}, 2, 64, 8)

	_, err := cache.entries(t.Context(), root)
	if !errors.Is(err, ErrSearchRootLimitExceeded) {
		t.Fatalf("entries error = %v, want search-root limit", err)
	}
	if visited != 3 {
		t.Fatalf("visited names = %d, want N+1 probe at 3", visited)
	}
	if len(cache.listings) != 0 {
		t.Fatalf("failed listing was cached: %#v", cache.listings)
	}
}

func TestSearchRootBudgetAcceptsDefaultExactLimitAndRejectsNext(t *testing.T) {
	budget, err := newSearchRootBudget(defaultSearchRootLimits())
	if err != nil {
		t.Fatal(err)
	}
	budget.entries = maximumSearchRootEntries - 1
	budget.nameBytes = maximumSearchRootNameBytes - 1
	if err := budget.admitName(1); err != nil {
		t.Fatalf("exact default limit: %v", err)
	}
	if err := budget.admitName(1); !errors.Is(err, ErrSearchRootLimitExceeded) {
		t.Fatalf("default N+1 error = %v, want search-root limit", err)
	}
}

func TestSearchRootCacheBoundsAggregateAndIndividualNameBytes(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name     string
		names    []string
		maxBytes int64
		maxName  int64
	}{
		{name: "aggregate", names: []string{"aa", "bbb"}, maxBytes: 4, maxName: 3},
		{name: "individual", names: []string{"aaaa"}, maxBytes: 8, maxName: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache := mustSearchRootCache(
				t,
				searchRootObserverForNames(test.names...),
				8,
				test.maxBytes,
				test.maxName,
			)
			if _, err := cache.entries(t.Context(), root); !errors.Is(err, ErrSearchRootLimitExceeded) {
				t.Fatalf("entries error = %v, want search-root limit", err)
			}
		})
	}
}

func TestSearchRootCacheAcceptsManyExactLengthNames(t *testing.T) {
	const entries = 64
	const nameBytes = 64
	names := make([]string, entries)
	for index := range names {
		names[index] = fmt.Sprintf("%04d%s", index, strings.Repeat("x", nameBytes-4))
	}
	cache := mustSearchRootCache(
		t,
		searchRootObserverForNames(names...),
		entries,
		entries*nameBytes,
		nameBytes,
	)
	listed, err := cache.entries(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != entries {
		t.Fatalf("listed names = %d, want %d", len(listed), entries)
	}
}

func TestSearchRootCacheRejectsPreCancellationBeforeObservation(t *testing.T) {
	root := t.TempDir()
	observed := false
	cache := mustSearchRootCache(t, func(_ context.Context, _ string, _ func(string) error) error {
		observed = true
		return nil
	}, 8, 64, 16)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := cache.entries(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("entries error = %v, want context.Canceled", err)
	}
	if observed {
		t.Fatal("pre-canceled listing reached the filesystem observer")
	}
}

func TestSearchRootCacheHonorsCancellationAndDoesNotCacheFailure(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	observations := 0
	cache := mustSearchRootCache(t, func(_ context.Context, _ string, visit func(string) error) error {
		observations++
		if err := visit("first"); err != nil {
			return err
		}
		cancel()
		return visit("second")
	}, 8, 64, 16)

	if _, err := cache.entries(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("entries error = %v, want context.Canceled", err)
	}
	if len(cache.listings) != 0 {
		t.Fatalf("canceled listing was cached: %#v", cache.listings)
	}
	if _, err := cache.entries(context.Background(), root); err != nil {
		t.Fatalf("uncanceled retry: %v", err)
	}
	if observations != 2 {
		t.Fatalf("listing observations = %d, want canceled attempt and retry", observations)
	}
}

func TestSearchRootCacheSharesBudgetAcrossDistinctRoots(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	third := t.TempDir()
	cache := mustSearchRootCache(t, searchRootObserverForNames("entry"), 2, 64, 16)

	for _, root := range []string{first, second} {
		if _, err := cache.entries(t.Context(), root); err != nil {
			t.Fatalf("entries(%q): %v", root, err)
		}
	}
	if _, err := cache.entries(t.Context(), third); !errors.Is(err, ErrSearchRootLimitExceeded) {
		t.Fatalf("third root error = %v, want shared operation limit", err)
	}
}

func TestSearchRootCacheReusesSortedListingDefensively(t *testing.T) {
	root := t.TempDir()
	observations := 0
	cache := mustSearchRootCache(t, func(_ context.Context, _ string, visit func(string) error) error {
		observations++
		for _, name := range []string{"zeta", "alpha", "middle"} {
			if err := visit(name); err != nil {
				return err
			}
		}
		return nil
	}, 8, 64, 16)

	first, err := cache.entries(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	first[0] = "changed"
	second, err := cache.entries(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(second); got != "[alpha middle zeta]" {
		t.Fatalf("cached entries = %s, want [alpha middle zeta]", got)
	}
	if observations != 1 {
		t.Fatalf("listing observations = %d, want one", observations)
	}
}

func TestSearchRootCacheDoesNotCacheOperationalFailure(t *testing.T) {
	root := t.TempDir()
	wantErr := errors.New("listing changed")
	observations := 0
	cache := mustSearchRootCache(t, func(_ context.Context, _ string, _ func(string) error) error {
		observations++
		if observations == 1 {
			return wantErr
		}
		return nil
	}, 8, 64, 16)

	if _, err := cache.entries(t.Context(), root); !errors.Is(err, wantErr) {
		t.Fatalf("first entries error = %v, want operational failure", err)
	}
	if _, err := cache.entries(t.Context(), root); err != nil {
		t.Fatalf("retry entries: %v", err)
	}
	if observations != 2 {
		t.Fatalf("listing observations = %d, want failed observation retried", observations)
	}
}

func TestSearchRootCacheDoesNotMaterializeGeneratedOverflowRemainder(t *testing.T) {
	root := t.TempDir()
	const generatedEntries = 1_000_000
	const admittedEntries = 1_024
	observed := 0
	observer := func(_ context.Context, _ string, visit func(string) error) error {
		for index := range generatedEntries {
			observed++
			if err := visit(fmt.Sprintf("entry-%07d", index)); err != nil {
				return err
			}
		}
		return nil
	}
	cache := mustSearchRootCache(t, observer, admittedEntries, 1<<20, 64)
	if _, err := cache.entries(t.Context(), root); !errors.Is(err, ErrSearchRootLimitExceeded) {
		t.Fatalf("entries error = %v, want search-root limit", err)
	}
	if observed != admittedEntries+1 {
		t.Fatalf("generated entries observed = %d, want %d", observed, admittedEntries+1)
	}

	result := testing.Benchmark(func(benchmark *testing.B) {
		for benchmark.Loop() {
			candidate := mustSearchRootCache(benchmark, observer, admittedEntries, 1<<20, 64)
			_, _ = candidate.entries(context.Background(), root)
		}
	})
	if allocated := result.AllocedBytesPerOp(); allocated >= 512<<10 {
		t.Fatalf("generated overflow allocated %d bytes/op, want bounded prefix retention", allocated)
	}
}

func mustSearchRootCache(
	t testing.TB,
	observer searchRootObserver,
	maximumEntries int,
	maximumNameBytes int64,
	maximumEntryNameBytes int64,
) *SearchRootCache {
	t.Helper()
	limits, err := newSearchRootLimits(maximumEntries, maximumNameBytes, maximumEntryNameBytes)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := newSearchRootCache(observer, limits)
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func searchRootObserverForNames(names ...string) searchRootObserver {
	return func(_ context.Context, _ string, visit func(string) error) error {
		for _, name := range names {
			if err := visit(name); err != nil {
				return err
			}
		}
		return nil
	}
}
