//go:build darwin || linux

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

func TestSourceIdentityCacheDoesNotMemoizeClassifiedSymlinkWhenRemainderExceedsBudget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skill")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "payload"), filepath.Join(root, "a-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "z-extra"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	observations := 0
	structure := skillTreeStructureLimitForTest(t)
	cache := newSourceIdentityCacheWithLimits(
		func(
			ctx context.Context,
			readPath string,
			traversal access.TraversalLimit,
		) (artifact.ContentHash, sourceIdentityMeasurement, error) {
			observations++
			return observeSkillDirectoryIdentity(ctx, readPath, traversal, structure)
		},
		2,
		1<<20,
	)

	for range 2 {
		if _, err := cache.ContentHash(t.Context(), root); !errors.Is(err, errSourceIdentityLimitExceeded) {
			t.Fatalf("ContentHash error = %v, want remainder-inclusive budget exhaustion", err)
		}
	}
	if observations != 2 {
		t.Fatalf("classified symlink observations = %d, want 2 uncached remainder exhaustions", observations)
	}
}

func TestSourceIdentityCacheMemoizesClassifiedSymlinkWhenRemainderFitsBudget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skill")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "payload"), filepath.Join(root, "a-link")); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	observations := 0
	structure := skillTreeStructureLimitForTest(t)
	cache := newSourceIdentityCacheWithLimits(
		func(
			ctx context.Context,
			readPath string,
			traversal access.TraversalLimit,
		) (artifact.ContentHash, sourceIdentityMeasurement, error) {
			observations++
			return observeSkillDirectoryIdentity(ctx, readPath, traversal, structure)
		},
		16,
		1<<20,
	)

	for range 2 {
		if _, err := cache.ContentHash(t.Context(), root); !errors.Is(err, access.ErrUnsupportedSymlink) {
			t.Fatalf("ContentHash error = %v, want classified nested symlink", err)
		}
	}
	if observations != 1 {
		t.Fatalf("classified symlink observations = %d, want 1 memoized skip", observations)
	}
}
