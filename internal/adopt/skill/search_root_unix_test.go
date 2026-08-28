//go:build darwin || linux

package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchRootCachePreservesFinalRootSymlinkCompatibility(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(physical, "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}

	cache := NewSearchRootCache()
	entries, err := cache.entries(t.Context(), alias)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries.names) != 1 || entries.names[0] != "review" {
		t.Fatalf("symlinked root entries = %#v, want [review]", entries.names)
	}
	resolved, err := resolvedImportSkillReadPath(physical)
	if err != nil {
		t.Fatal(err)
	}
	if entries.readRoot != resolved {
		t.Fatalf("symlinked root read path = %q, want %q", entries.readRoot, resolved)
	}
}

func TestSearchRootCacheRejectsChangedSharedResolvedRoot(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	firstAlias := filepath.Join(base, "first")
	secondAlias := filepath.Join(base, "second")
	for _, alias := range []string{firstAlias, secondAlias} {
		if err := os.Symlink(physical, alias); err != nil {
			t.Fatal(err)
		}
	}

	cache := NewSearchRootCache()
	if entries, err := cache.entries(t.Context(), firstAlias); err != nil || len(entries.names) != 0 {
		t.Fatalf("initial entries = %#v, error = %v, want empty", entries.names, err)
	}
	if err := os.Mkdir(filepath.Join(physical, "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.entries(t.Context(), secondAlias); err == nil ||
		!strings.Contains(err.Error(), "changed after observation") {
		t.Fatalf("shared resolved-root reuse error = %v, want stale listing", err)
	}
}

func TestSearchRootCacheRejectsAliasRetargetDuringObservation(t *testing.T) {
	base := t.TempDir()
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	for _, root := range []string{first, second} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}

	cache, err := newSearchRootCache(func(
		_ context.Context,
		readRoot string,
		visit func(string) error,
	) (searchRootObservation, error) {
		resolvedFirst, resolveErr := resolvedImportSkillReadPath(first)
		if resolveErr != nil {
			return searchRootObservation{}, resolveErr
		}
		if readRoot != resolvedFirst {
			return searchRootObservation{}, errors.New("observer received unexpected read root")
		}
		if visitErr := visit("shared"); visitErr != nil {
			return searchRootObservation{}, visitErr
		}
		if removeErr := os.Remove(alias); removeErr != nil {
			return searchRootObservation{}, removeErr
		}
		if linkErr := os.Symlink(second, alias); linkErr != nil {
			return searchRootObservation{}, linkErr
		}
		return stableSearchRootObservation(), nil
	}, defaultSearchRootLimits())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := cache.entries(t.Context(), alias); err == nil ||
		!strings.Contains(err.Error(), "binding") ||
		!strings.Contains(err.Error(), "changed after observation") {
		t.Fatalf("retargeted alias error = %v, want changed binding", err)
	}
	if len(cache.listings) != 0 || len(cache.bindings) != 0 {
		t.Fatalf("retargeted alias retained cache state: listings=%#v bindings=%#v", cache.listings, cache.bindings)
	}
}

func TestSearchRootCacheRejectsRetargetedAliasOnReuse(t *testing.T) {
	base := t.TempDir()
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	for _, root := range []string{first, second} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}

	cache := NewSearchRootCache()
	if _, err := cache.entries(t.Context(), alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}

	if _, err := cache.entries(t.Context(), alias); err == nil || !strings.Contains(err.Error(), "changed after observation") {
		t.Fatalf("retargeted alias reuse error = %v, want changed binding", err)
	}
	if len(cache.listings) != 0 || len(cache.bindings) != 0 {
		t.Fatalf("retargeted reuse retained cache state: listings=%#v bindings=%#v", cache.listings, cache.bindings)
	}
}

func TestSearchRootCacheRevalidatesEveryAliasBinding(t *testing.T) {
	base := t.TempDir()
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	for _, root := range []string{first, second} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	firstAlias := filepath.Join(base, "first-alias")
	secondAlias := filepath.Join(base, "second-alias")
	for _, alias := range []string{firstAlias, secondAlias} {
		if err := os.Symlink(first, alias); err != nil {
			t.Fatal(err)
		}
	}

	cache := NewSearchRootCache()
	for _, alias := range []string{firstAlias, secondAlias} {
		if _, err := cache.entries(t.Context(), alias); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(firstAlias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, firstAlias); err != nil {
		t.Fatal(err)
	}

	if err := cache.Validate(t.Context()); err == nil || !strings.Contains(err.Error(), "changed after observation") {
		t.Fatalf("alias binding validation error = %v, want changed binding", err)
	}
}
