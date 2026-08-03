package skill

import (
	"context"
	"errors"
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
	cache := newSourceIdentityCache(func(_ context.Context, path string) (artifact.ContentHash, error) {
		observations++
		if path != readPath {
			t.Fatalf("observed path = %q, want %q", path, readPath)
		}
		return want, nil
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
	cache := newSourceIdentityCache(func(_ context.Context, path string) (artifact.ContentHash, error) {
		observed[path]++
		return artifact.HashFileContent([]byte(path)), nil
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
	cache := newSourceIdentityCache(func(context.Context, string) (artifact.ContentHash, error) {
		attempts++
		return "", wantErr
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
	cache := newSourceIdentityCache(func(context.Context, string) (artifact.ContentHash, error) {
		attempts++
		return "sha256:short", nil
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

func TestSourceIdentityCacheHonorsCancellationBeforeCachedResult(t *testing.T) {
	readPath := filepath.Join(t.TempDir(), "skill")
	cache := newSourceIdentityCache(func(context.Context, string) (artifact.ContentHash, error) {
		return artifact.HashFileContent([]byte("skill")), nil
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
	cache := newSourceIdentityCache(func(context.Context, string) (artifact.ContentHash, error) {
		attempts++
		cancel()
		return artifact.HashFileContent([]byte("skill")), nil
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

func TestSourceIdentityCacheRejectsNoncanonicalKeysBeforeObservation(t *testing.T) {
	observed := false
	cache := newSourceIdentityCache(func(context.Context, string) (artifact.ContentHash, error) {
		observed = true
		return "", nil
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
