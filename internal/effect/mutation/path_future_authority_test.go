//go:build darwin || linux || windows

package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func TestFutureReferentAuthorityMatchesTwoStepObservation(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "physical", "nested")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "physical", "settings.json")
	if err := os.WriteFile(file, []byte("unchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	assertMatchesTwoStep := func(t *testing.T, selected, want string) {
		t.Helper()
		oldBudget := &pathAuthorityTestBudget{limit: 100_000}
		resolved, err := rootedpath.ResolveFutureReferentPathBounded(selected, 256, oldBudget)
		if err != nil || resolved != want {
			t.Fatalf("two-step resolved path = %q, %v; want %q", resolved, err, want)
		}
		oldAuthority, err := ObserveDirectoryEntryAuthorityBounded(resolved, 256, oldBudget)
		if err != nil {
			t.Fatal(err)
		}

		got, authority, err := ObserveFutureReferentAuthorityBounded(selected, 256, &pathAuthorityTestBudget{limit: 100_000})
		if err != nil || got != want || !authority.Equal(oldAuthority) {
			t.Fatalf("combined path/authority = %q, %v; want %q and equal authority", got, err, want)
		}
		gotReference, gotExact := authority.Reference()
		oldReference, oldExact := oldAuthority.Reference()
		if gotExact != oldExact || (gotExact && !gotReference.Equal(oldReference)) {
			t.Fatal("combined observation changed exact lookup spelling")
		}

		scoped := NewFutureReferentAuthorityObserverBounded(256, &pathAuthorityTestBudget{limit: 100_000})
		for range 2 {
			got, authority, err := scoped(selected)
			if err != nil || got != want || !authority.Equal(oldAuthority) {
				t.Fatalf("scoped path/authority = %q, %v; want %q and equal authority", got, err, want)
			}
			gotReference, gotExact := authority.Reference()
			if gotExact != oldExact || (gotExact && !gotReference.Equal(oldReference)) {
				t.Fatal("scoped observation changed exact lookup spelling")
			}
		}
	}

	for _, test := range []struct {
		name string
		path string
	}{
		{"existing file", file},
		{"existing directory", directory},
		{"missing leaf", filepath.Join(directory, "new.json")},
		{"missing ancestors", filepath.Join(root, "future", "nested", "new.json")},
		{"non-ASCII future", filepath.Join(directory, "Futuré.json")},
	} {
		t.Run(test.name, func(t *testing.T) { assertMatchesTwoStep(t, test.path, test.path) })
	}

	t.Run("directory-only existing", func(t *testing.T) {
		assertMatchesTwoStep(t, directory+string(filepath.Separator), directory)
	})
	t.Run("directory-only future", func(t *testing.T) {
		future := filepath.Join(root, "future-directory")
		assertMatchesTwoStep(t, future+string(filepath.Separator), future)
	})

	t.Run("aliases", func(t *testing.T) {
		for _, link := range []struct{ name, target string }{
			{"directory-alias", filepath.Join("physical", "nested")},
			{"file-alias", file},
			{"future-alias", filepath.Join("future", "new.json")},
		} {
			if err := os.Symlink(link.target, filepath.Join(root, link.name)); err != nil {
				if runtime.GOOS == "windows" {
					t.Skipf("symlink setup is unavailable: %v", err)
				}
				t.Fatal(err)
			}
		}
		for _, test := range []struct{ name, path, want string }{
			{"final directory", filepath.Join(root, "directory-alias"), directory},
			{"final file", filepath.Join(root, "file-alias"), file},
			{"dangling final", filepath.Join(root, "future-alias"), filepath.Join(root, "future", "new.json")},
			{"missing after alias", filepath.Join(root, "directory-alias", "new.json"), filepath.Join(directory, "new.json")},
			{"alias then parent", filepath.Join(root, "directory-alias") + string(filepath.Separator) + ".." + string(filepath.Separator) + "settings.json", file},
		} {
			t.Run(test.name, func(t *testing.T) { assertMatchesTwoStep(t, test.path, test.want) })
		}
	})
}

func TestFutureReferentAuthorityRemovesRepeatedTraversal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "future", "nested", "settings.json")
	oldBudget := &pathAuthorityTestBudget{limit: 100_000}
	resolved, err := rootedpath.ResolveFutureReferentPathBounded(path, 256, oldBudget)
	if err != nil {
		t.Fatal(err)
	}
	oldAuthority, err := ObserveDirectoryEntryAuthorityBounded(resolved, 256, oldBudget)
	if err != nil {
		t.Fatal(err)
	}

	budget := &pathAuthorityTestBudget{limit: 100_000}
	got, authority, err := ObserveFutureReferentAuthorityBounded(path, 256, budget)
	if err != nil || got != resolved || !authority.Equal(oldAuthority) {
		t.Fatalf("combined observation changed path or authority: %q, %v", got, err)
	}
	if budget.visits >= oldBudget.visits {
		t.Fatalf("combined traversal charged %d components; two-step charged %d", budget.visits, oldBudget.visits)
	}
	if _, err := os.Lstat(filepath.Join(root, "future")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("future observation changed the filesystem: %v", err)
	}
}

func TestFutureReferentAuthorityRejectsWithoutPartialEvidence(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		path   string
		depth  int
		budget *pathAuthorityTestBudget
	}{
		{"empty selector", "", 256, &pathAuthorityTestBudget{limit: 100_000}},
		{"relative selector", "relative", 256, &pathAuthorityTestBudget{limit: 100_000}},
		{"NUL selector", root + "\x00", 256, &pathAuthorityTestBudget{limit: 100_000}},
		{"filesystem root", filepath.VolumeName(root) + string(filepath.Separator), 256, &pathAuthorityTestBudget{limit: 100_000}},
		{"non-directory ancestor", filepath.Join(file, "child"), 256, &pathAuthorityTestBudget{limit: 100_000}},
		{"directory-only file", file + string(filepath.Separator), 256, &pathAuthorityTestBudget{limit: 100_000}},
		{"missing budget", file, 256, nil},
		{"exhausted budget", file, 256, &pathAuthorityTestBudget{}},
		{"depth limit", file, 1, &pathAuthorityTestBudget{limit: 100_000}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var budget rootedpath.PhysicalTraversalBudget
			var oldBudget rootedpath.PhysicalTraversalBudget
			if test.budget != nil {
				budget = test.budget
				oldBudget = &pathAuthorityTestBudget{limit: test.budget.limit}
			}
			resolved, oldErr := rootedpath.ResolveFutureReferentPathBounded(test.path, test.depth, oldBudget)
			if oldErr == nil {
				_, oldErr = ObserveDirectoryEntryAuthorityBounded(resolved, test.depth, oldBudget)
			}
			if oldErr == nil {
				t.Fatal("two-step failure fixture unexpectedly succeeded")
			}
			got, authority, err := ObserveFutureReferentAuthorityBounded(test.path, test.depth, budget)
			if err == nil || got != "" || !authority.exact.IsZero() || !authority.provisional.IsZero() || authority.locator != "" {
				t.Fatalf("failed observation supplied partial evidence: %q, %+v, %v", got, authority, err)
			}
			var oldFailure, failure *rootedpath.Failure
			oldTyped := errors.As(oldErr, &oldFailure)
			typed := errors.As(err, &failure)
			if oldTyped != typed || (typed && failure.Kind() != oldFailure.Kind()) {
				t.Fatalf("combined failure changed category: %v; two-step %v", err, oldErr)
			}

			var scopedBudget rootedpath.PhysicalTraversalBudget
			if test.budget != nil {
				scopedBudget = &pathAuthorityTestBudget{limit: test.budget.limit}
			}
			scoped := NewFutureReferentAuthorityObserverBounded(test.depth, scopedBudget)
			got, authority, err = scoped(test.path)
			if err == nil || got != "" || !authority.exact.IsZero() || !authority.provisional.IsZero() || authority.locator != "" {
				t.Fatalf("failed scoped observation supplied evidence: %q, %+v, %v", got, authority, err)
			}
			failure = nil
			typed = errors.As(err, &failure)
			if oldTyped != typed || (typed && failure.Kind() != oldFailure.Kind()) {
				t.Fatalf("scoped failure changed category: %v; two-step %v", err, oldErr)
			}
		})
	}
}

func TestFutureReferentAuthorityRejectsAliasToFilesystemRoot(t *testing.T) {
	root := t.TempDir()
	volumeRoot := filepath.VolumeName(root) + string(filepath.Separator)
	alias := filepath.Join(root, "root-alias")
	if err := os.Symlink(volumeRoot, alias); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink setup is unavailable: %v", err)
		}
		t.Fatal(err)
	}
	oldBudget := &pathAuthorityTestBudget{limit: 100_000}
	resolved, err := rootedpath.ResolveFutureReferentPathBounded(alias, 256, oldBudget)
	if err != nil || resolved != volumeRoot {
		t.Fatalf("root alias = %q, %v; want %q", resolved, err, volumeRoot)
	}
	_, oldErr := ObserveDirectoryEntryAuthorityBounded(resolved, 256, oldBudget)
	var oldFailure *rootedpath.Failure
	if !errors.As(oldErr, &oldFailure) || oldFailure.Kind() != rootedpath.FailureInvalidDestination {
		t.Fatalf("two-step root authority was not refused: %v", oldErr)
	}

	got, authority, err := ObserveFutureReferentAuthorityBounded(alias, 256, &pathAuthorityTestBudget{limit: 100_000})
	var failure *rootedpath.Failure
	if got != "" || !authority.exact.IsZero() || !authority.provisional.IsZero() || authority.locator != "" || !errors.As(err, &failure) || failure.Kind() != oldFailure.Kind() {
		t.Fatalf("root alias supplied authority or changed refusal: %q, %+v, %v", got, authority, err)
	}
}
