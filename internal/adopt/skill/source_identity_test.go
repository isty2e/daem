package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func skillTreeStructureLimitForTest(t *testing.T) access.TreeStructureLimit {
	t.Helper()
	limit, err := access.NewTreeStructureLimit(100, 16)
	if err != nil {
		t.Fatal(err)
	}
	return limit
}

func TestSourceIdentityCacheObservesOneCanonicalRouteOnce(t *testing.T) {
	readPath := filepath.Join(t.TempDir(), "skill")
	want := artifact.HashFileContent([]byte("skill"))
	observations := 0
	cache := newSourceIdentityCache(func(_ context.Context, path string, _ access.TraversalLimit) (artifact.ContentHash, sourceIdentityMeasurement, error) {
		observations++
		if path != readPath {
			t.Fatalf("observed path = %q, want %q", path, readPath)
		}
		return want, sourceIdentityMeasurement{entries: 1, bytes: 5}, nil
	})

	for range 3 {
		got, err := cache.ContentHash(context.Background(), readPath)
		if err != nil {
			t.Fatalf("ContentHash returned error: %v", err)
		}
		if got != want {
			t.Fatalf("ContentHash = %q, want %q", got, want)
		}
	}
	if observations != 1 {
		t.Fatalf("identity observations = %d, want 1", observations)
	}
}

func TestSourceIdentityCacheKeepsDistinctCanonicalRoutesSeparate(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	observed := make(map[string]int)
	cache := newSourceIdentityCache(func(_ context.Context, path string, _ access.TraversalLimit) (artifact.ContentHash, sourceIdentityMeasurement, error) {
		observed[path]++
		return artifact.HashFileContent([]byte(path)), sourceIdentityMeasurement{entries: 1, bytes: int64(len(path))}, nil
	})

	firstHash, err := cache.ContentHash(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := cache.ContentHash(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == secondHash {
		t.Fatalf("distinct route hashes unexpectedly match: %q", firstHash)
	}
	if observed[first] != 1 || observed[second] != 1 {
		t.Fatalf("observations = %#v, want one per canonical route", observed)
	}
}

func TestSourceIdentityCacheDoesNotMemoizeFailedObservation(t *testing.T) {
	readPath := filepath.Join(t.TempDir(), "skill")
	wantErr := errors.New("identity drift")
	attempts := 0
	cache := newSourceIdentityCache(func(context.Context, string, access.TraversalLimit) (artifact.ContentHash, sourceIdentityMeasurement, error) {
		attempts++
		return "", sourceIdentityMeasurement{entries: 1, bytes: 2}, wantErr
	})

	for range 2 {
		if _, err := cache.ContentHash(context.Background(), readPath); !errors.Is(err, wantErr) {
			t.Fatalf("ContentHash error = %v, want %v", err, wantErr)
		}
	}
	if attempts != 2 {
		t.Fatalf("observation attempts = %d, want 2", attempts)
	}
}

func TestSourceIdentityCacheDoesNotMemoizeMalformedIdentity(t *testing.T) {
	readPath := filepath.Join(t.TempDir(), "skill")
	attempts := 0
	cache := newSourceIdentityCache(func(context.Context, string, access.TraversalLimit) (artifact.ContentHash, sourceIdentityMeasurement, error) {
		attempts++
		return "sha256:short", sourceIdentityMeasurement{entries: 1, bytes: 2}, nil
	})

	for range 2 {
		if _, err := cache.ContentHash(context.Background(), readPath); err == nil {
			t.Fatal("ContentHash accepted malformed source identity")
		}
	}
	if attempts != 2 {
		t.Fatalf("observation attempts = %d, want 2", attempts)
	}
}

func TestSourceIdentityCacheMemoizesClassifiedEligibilitySkips(t *testing.T) {
	readPath := filepath.Join(t.TempDir(), "skill")
	attempts := 0
	cache := newSourceIdentityCache(func(context.Context, string, access.TraversalLimit) (artifact.ContentHash, sourceIdentityMeasurement, error) {
		attempts++
		return "", sourceIdentityMeasurement{entries: 1, bytes: 0}, access.ErrRequiredRootRegularFile
	})

	for range 2 {
		if _, err := cache.ContentHash(context.Background(), readPath); !errors.Is(err, access.ErrRequiredRootRegularFile) {
			t.Fatalf("ContentHash error = %v, want required-file skip", err)
		}
	}
	if attempts != 1 {
		t.Fatalf("classified skip observations = %d, want 1", attempts)
	}
}

func TestSourceIdentityCacheHonorsCancellationBeforeCachedResult(t *testing.T) {
	readPath := filepath.Join(t.TempDir(), "skill")
	cache := newSourceIdentityCache(func(context.Context, string, access.TraversalLimit) (artifact.ContentHash, sourceIdentityMeasurement, error) {
		return artifact.HashFileContent([]byte("skill")), sourceIdentityMeasurement{entries: 1, bytes: 5}, nil
	})
	if _, err := cache.ContentHash(context.Background(), readPath); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.ContentHash(ctx, readPath); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached ContentHash error = %v, want context.Canceled", err)
	}
}

func TestSourceIdentityCacheDoesNotMemoizeObservationCompletedAfterCancellation(t *testing.T) {
	readPath := filepath.Join(t.TempDir(), "skill")
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	cache := newSourceIdentityCache(func(context.Context, string, access.TraversalLimit) (artifact.ContentHash, sourceIdentityMeasurement, error) {
		attempts++
		cancel()
		return artifact.HashFileContent([]byte("skill")), sourceIdentityMeasurement{entries: 1, bytes: 5}, nil
	})

	if _, err := cache.ContentHash(ctx, readPath); !errors.Is(err, context.Canceled) {
		t.Fatalf("ContentHash error = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("observation attempts = %d, want 1", attempts)
	}

	if _, err := cache.ContentHash(context.Background(), readPath); err != nil {
		t.Fatalf("uncanceled retry: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("observation attempts after retry = %d, want 2", attempts)
	}
}

func TestSourceIdentityCacheChargesEveryDistinctRouteToOneAggregateBudget(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	observations := 0
	cache := newSourceIdentityCacheWithLimits(
		func(_ context.Context, path string, _ access.TraversalLimit) (artifact.ContentHash, sourceIdentityMeasurement, error) {
			observations++
			return artifact.HashFileContent([]byte(path)), sourceIdentityMeasurement{entries: 1, bytes: 3}, nil
		},
		2,
		5,
	)

	if _, err := cache.ContentHash(t.Context(), first); err != nil {
		t.Fatalf("first ContentHash returned error: %v", err)
	}
	if _, err := cache.ContentHash(t.Context(), second); !errors.Is(err, errSourceIdentityLimitExceeded) {
		t.Fatalf("second ContentHash error = %v, want aggregate byte exhaustion", err)
	}
	if observations != 2 {
		t.Fatalf("identity observations = %d, want both distinct routes charged", observations)
	}
}

func TestSourceIdentityCacheChargesClassifiedSkipsBeforeMemoizingThem(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	valid := filepath.Join(root, "valid")
	cache := newSourceIdentityCacheWithLimits(
		func(_ context.Context, path string, _ access.TraversalLimit) (artifact.ContentHash, sourceIdentityMeasurement, error) {
			if path == missing {
				return "", sourceIdentityMeasurement{entries: 1, bytes: 3}, access.ErrRequiredRootRegularFile
			}
			return artifact.HashFileContent([]byte(path)), sourceIdentityMeasurement{entries: 1, bytes: 3}, nil
		},
		2,
		5,
	)

	if _, err := cache.ContentHash(t.Context(), missing); !errors.Is(err, access.ErrRequiredRootRegularFile) {
		t.Fatalf("classified ContentHash error = %v, want required root file", err)
	}
	if _, err := cache.ContentHash(t.Context(), valid); !errors.Is(err, errSourceIdentityLimitExceeded) {
		t.Fatalf("valid ContentHash error = %v, want prior classified work charged", err)
	}
}

func TestSourceIdentityCacheStopsPhysicalHashAtRemainingOperationBudget(t *testing.T) {
	for _, test := range []struct {
		name       string
		maxEntries int
		maxBytes   int64
	}{
		{name: "entries", maxEntries: 1, maxBytes: 64},
		{name: "bytes", maxEntries: 8, maxBytes: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "skill")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("1234"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "payload"), []byte("56"), 0o600); err != nil {
				t.Fatal(err)
			}
			root, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			structure := skillTreeStructureLimitForTest(t)
			cache := newSourceIdentityCacheWithLimits(
				func(
					ctx context.Context,
					readPath string,
					traversal access.TraversalLimit,
				) (artifact.ContentHash, sourceIdentityMeasurement, error) {
					return observeSkillDirectoryIdentity(ctx, readPath, traversal, structure)
				},
				test.maxEntries,
				test.maxBytes,
			)

			if _, err := cache.ContentHash(t.Context(), root); !errors.Is(err, errSourceIdentityLimitExceeded) {
				t.Fatalf("ContentHash error = %v, want source identity operation limit", err)
			}
		})
	}
}

func TestSourceIdentityCacheRejectsNoncanonicalKeysBeforeObservation(t *testing.T) {
	observed := false
	cache := newSourceIdentityCache(func(context.Context, string, access.TraversalLimit) (artifact.ContentHash, sourceIdentityMeasurement, error) {
		observed = true
		return "", sourceIdentityMeasurement{}, nil
	})

	root := t.TempDir()
	noncanonicalAbsolute := filepath.Join(root, "nested") + string(filepath.Separator) + ".." + string(filepath.Separator) + "skill"
	for _, readPath := range []string{"relative/skill", noncanonicalAbsolute} {
		if _, err := cache.ContentHash(context.Background(), readPath); err == nil {
			t.Fatalf("ContentHash accepted noncanonical path %q", readPath)
		}
	}
	if observed {
		t.Fatal("noncanonical cache key reached source observer")
	}
}
