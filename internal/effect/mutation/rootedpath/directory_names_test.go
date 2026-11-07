//go:build darwin || linux || windows

package rootedpath

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

type directoryNamesBudget struct {
	visits       int
	entries      int
	entryLimit   int
	entryFailure error
	pathFailure  error
	afterVisit   func(int) error
	afterEntries func() error
}

func (budget *directoryNamesBudget) AdmitPathComponents(count int) error {
	budget.visits += count
	if budget.afterVisit != nil {
		if err := budget.afterVisit(budget.visits); err != nil {
			return err
		}
	}
	if count > 0 {
		return budget.pathFailure
	}
	return nil
}

func (budget *directoryNamesBudget) AdmitPhysicalWork(components, entries int, bytes int64) error {
	if components != 0 || bytes != 0 {
		return errors.New("directory names charged file bytes or duplicate path work")
	}
	budget.entries += entries
	if budget.entryFailure != nil && budget.entries > budget.entryLimit {
		return budget.entryFailure
	}
	if entries != 0 && budget.afterEntries != nil {
		hook := budget.afterEntries
		budget.afterEntries = nil
		return hook()
	}
	return nil
}

func TestReadDirectoryNamesDistinguishesPresentEmptyAndAbsent(t *testing.T) {
	base := referentTestRoot(t)
	for _, test := range []struct {
		name    string
		present bool
		names   []string
	}{
		{name: "empty", present: true},
		{name: "populated", present: true, names: []string{"alpha.json", "nested", "zeta.js"}},
		{name: "missing/child"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(base, filepath.FromSlash(test.name))
			if test.present {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range test.names {
				var err error
				if name == "nested" {
					err = os.Mkdir(filepath.Join(path, name), 0o700)
				} else {
					err = os.WriteFile(filepath.Join(path, name), []byte("unread fixture"), 0o600)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			budget := &directoryNamesBudget{}
			snapshot, err := ReadDirectoryNamesBounded(t.Context(), path, 256, budget)
			if err != nil || snapshot.Present() != test.present || snapshot.PhysicalPath() != path || !slices.Equal(snapshot.Names(), test.names) || budget.entries != len(test.names) || budget.visits == 0 {
				t.Fatalf("directory observation: %+v, entries=%d, visits=%d, %v", snapshot, budget.entries, budget.visits, err)
			}
			if names := snapshot.Names(); len(names) != 0 {
				names[0] = "changed"
				if !slices.Equal(snapshot.Names(), test.names) {
					t.Fatal("directory observation exposes mutable name storage")
				}
			}
			if !test.present {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("read-only directory observation created the future path", err)
				}
			}
		})
	}
}

func TestReadDirectoryNamesFollowsDirectoryAliasesWithoutOpeningChildTargets(t *testing.T) {
	base := referentTestRoot(t)
	directory := filepath.Join(base, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	referentTestSymlink(t, filepath.Join(base, "unavailable"), filepath.Join(directory, "dangling.js"))
	referentTestSymlink(t, directory, filepath.Join(base, "alias"))
	snapshot, err := ReadDirectoryNamesBounded(t.Context(), filepath.Join(base, "alias"), 256, &directoryNamesBudget{})
	if err != nil || !snapshot.Present() || snapshot.PhysicalPath() != directory || !slices.Equal(snapshot.Names(), []string{"dangling.js"}) {
		t.Fatal("directory aliases or no-follow child enumeration changed", snapshot, err)
	}
	future := filepath.Join(base, "future", "directory")
	referentTestSymlink(t, future, filepath.Join(base, "future-alias"))
	snapshot, err = ReadDirectoryNamesBounded(t.Context(), filepath.Join(base, "future-alias"), 256, &directoryNamesBudget{})
	if err != nil || snapshot.Present() || snapshot.PhysicalPath() != future || len(snapshot.Names()) != 0 {
		t.Fatal("dangling directory selector did not retain its absent future location", snapshot, err)
	}
	if _, err := os.Stat(future); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("future directory alias target was created", err)
	}
}

func TestReadDirectoryNamesDoesNotFilterPOSIXControlOrBackslashNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path bytes are not Windows component spellings")
	}
	base := referentTestRoot(t)
	names := []string{"back\\slash.js", "line\nbreak.js"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(base, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := ReadDirectoryNamesBounded(t.Context(), base, 256, &directoryNamesBudget{})
	if err != nil || !slices.Equal(snapshot.Names(), names) {
		t.Fatal("directory-name observation silently filtered path data", snapshot, err)
	}
}

func TestReadDirectoryNamesRefusesFilesAndInvalidIngressWithoutEmptySuccess(t *testing.T) {
	base := referentTestRoot(t)
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative", "bad\x00selector", file, filepath.Join(file, "child"), filepath.VolumeName(base) + string(filepath.Separator)} {
		snapshot, err := ReadDirectoryNamesBounded(t.Context(), path, 256, &directoryNamesBudget{})
		if err == nil || snapshot.PhysicalPath() != "" || snapshot.Present() || len(snapshot.Names()) != 0 {
			t.Fatal("invalid or unavailable directory became a checked-empty result", path, snapshot, err)
		}
		if filepath.Dir(path) == path && !hasFailureKind(err, FailureInvalidRoot) {
			t.Fatal("filesystem-root admission failed in an unrelated walker branch", err)
		}
	}
}

func TestReadDirectoryNamesDiscardsDataOnBudgetCancellationOrBindingChange(t *testing.T) {
	for _, scenario := range []string{"path limit", "entry limit", "validation visit limit", "cancel", "cancel during failed walk", "replace directory"} {
		t.Run(scenario, func(t *testing.T) {
			base := referentTestRoot(t)
			directory := filepath.Join(base, "directory")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "entry"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			limit := errors.New("directory observation allocation exhausted")
			budget := &directoryNamesBudget{}
			replaced := false
			switch scenario {
			case "path limit":
				budget.pathFailure = limit
			case "entry limit":
				budget.entryFailure = limit
			case "validation visit limit":
				budget.afterEntries = func() error { budget.pathFailure = limit; return nil }
			case "cancel":
				budget.afterEntries = func() error { cancel(); return nil }
			case "cancel during failed walk":
				budget.pathFailure = limit
				budget.afterVisit = func(int) error { cancel(); return nil }
			case "replace directory":
				budget.afterEntries = func() error {
					if err := os.Rename(directory, directory+"-original"); err != nil {
						return err
					}
					if err := os.Mkdir(directory, 0o700); err != nil {
						return err
					}
					replaced = true
					return nil
				}
			}
			snapshot, err := ReadDirectoryNamesBounded(ctx, directory, 256, budget)
			if err == nil || snapshot.PhysicalPath() != "" || snapshot.Present() || len(snapshot.Names()) != 0 {
				t.Fatal("failed observation exposed partial directory data", snapshot, err)
			}
			switch scenario {
			case "path limit", "entry limit", "validation visit limit":
				if !errors.Is(err, limit) {
					t.Fatal("allocation failure was reclassified", err)
				}
			case "cancel", "cancel during failed walk":
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation became unavailable coverage", err)
				}
			case "replace directory":
				if !replaced || !hasFailureKind(err, FailureRootReplaced) {
					t.Fatal("fixture did not detect the replaced physical directory binding", err)
				}
			}
		})
	}
}

func TestReadDirectoryNamesRejectsDetachedAbsentPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows walker already revalidates absent-prefix bindings")
	}
	base := referentTestRoot(t)
	parent := filepath.Join(base, "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(parent, "missing")
	depth, err := absolutePathDepth(selected)
	if err != nil {
		t.Fatal(err)
	}
	at := depth
	replaced := false
	budget := &directoryNamesBudget{}
	budget.afterVisit = func(visits int) error {
		if visits != at || replaced {
			return nil
		}
		if err := os.Rename(parent, parent+"-original"); err != nil {
			return err
		}
		if err := os.MkdirAll(selected, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(selected, "entry.js"), nil, 0o600); err != nil {
			return err
		}
		replaced = true
		return nil
	}
	snapshot, err := ReadDirectoryNamesBounded(t.Context(), selected, 256, budget)
	if !replaced || !hasFailureKind(err, FailureRootReplaced) || snapshot.PhysicalPath() != "" || snapshot.Present() || len(snapshot.Names()) != 0 {
		t.Fatal("detached old directory supplied checked-empty protection for its replacement", snapshot, err)
	}
	if _, err := os.Stat(filepath.Join(selected, "entry.js")); err != nil {
		t.Fatal("replacement fixture did not contain the supposedly absent code directory", err)
	}
}
