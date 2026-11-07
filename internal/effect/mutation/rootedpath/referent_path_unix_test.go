//go:build darwin || linux

package rootedpath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestResolveReferentPathBoundedPreservesRawAliasTargetSemantics(t *testing.T) {
	base := referentTestRoot(t)
	other := filepath.Join(base, "other")
	if err := os.MkdirAll(filepath.Join(other, "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{filepath.Join(base, "settings"), filepath.Join(other, "settings")} {
		if err := os.WriteFile(file, []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	referentTestSymlink(t, "other/deep", filepath.Join(base, "jump"))
	alias := filepath.Join(base, "alias")
	referentTestSymlink(t, "jump/../settings", alias)
	resolved, err := ResolveReferentPathBounded(alias, 256, &referentTestBudget{limit: 10_000})
	if err != nil || resolved != filepath.Join(other, "settings") {
		t.Fatalf("raw target resolved to %q, %v; want physical dotdot semantics", resolved, err)
	}

	for index, target := range []string{"settings/", "settings/.", "settings/../settings", "missing/../settings"} {
		path := filepath.Join(base, string(rune('a'+index)))
		referentTestSymlink(t, target, path)
		if resolved, err := ResolveReferentPathBounded(path, 256, &referentTestBudget{limit: 10_000}); err == nil || resolved != "" {
			t.Fatalf("directory-requiring target %q returned %q, %v", target, resolved, err)
		}
	}
}

func TestRootSelectorUsesPhysicalParentsInsteadOfLexicalCleaning(t *testing.T) {
	base := referentTestRoot(t)
	actual := filepath.Join(base, "actual", "codex")
	for _, directory := range []string{filepath.Join(base, "actual", "inner"), actual, filepath.Join(base, "codex")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "link")
	referentTestSymlink(t, "actual/inner", link)
	for _, test := range []struct {
		selector string
		want     string
	}{
		{link + "/../codex", actual},
		{link + "/../../codex", filepath.Join(base, "codex")},
		{actual + "/", actual},
		{actual + "/.", actual},
		{actual + "/future/settings.json", filepath.Join(actual, "future", "settings.json")},
		{"/", "/"},
	} {
		got, err := ResolveRootSelectorBounded(test.selector, 256, &referentTestBudget{limit: 10_000})
		if err != nil || got != test.want {
			t.Errorf("selector %q = %q, %v; want %q", test.selector, got, err, test.want)
		}
	}
	if _, err := os.Lstat(filepath.Join(actual, "future")); !os.IsNotExist(err) {
		t.Fatalf("read-only resolution created a missing directory: %v", err)
	}
}

func TestOnlyReadOnlyRootSelectorsAdmitAnAliasToTheFilesystemRoot(t *testing.T) {
	alias := filepath.Join(referentTestRoot(t), "filesystem-root")
	referentTestSymlink(t, "/", alias)
	if got, err := ResolveReferentPathBounded(alias, 256, &referentTestBudget{limit: 10_000}); err == nil || got != "" {
		t.Fatalf("canonical entry resolver admitted a filesystem root: %q, %v", got, err)
	}
	if got, err := ResolveRootSelectorBounded(alias, 256, &referentTestBudget{limit: 10_000}); err != nil || got != "/" {
		t.Fatalf("read-only filesystem-root comparison = %q, %v", got, err)
	}
	if root, err := CaptureCanonicalRootBounded(alias, 256, &referentTestBudget{limit: 10_000}); err == nil || root != nil {
		if root != nil {
			_ = root.Close()
		}
		t.Fatal("a filesystem-root selector became mutation authority", err)
	}
}

func TestRootSelectorChargesLiteralComponentsAndOneFilesystemRootVisit(t *testing.T) {
	base := referentTestRoot(t)
	for _, directory := range []string{filepath.Join(base, "actual", "inner"), filepath.Join(base, "actual", "codex")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "link")
	referentTestSymlink(t, "actual/inner", link)
	control := &referentTestBudget{limit: 10_000}
	if _, err := ResolveRootSelectorBounded(link, 256, control); err != nil {
		t.Fatal(err)
	}
	visits := control.visits + 3
	selector := link + "/./../codex"
	exact := &referentTestBudget{limit: visits}
	got, err := ResolveRootSelectorBounded(selector, 256, exact)
	if err != nil || got != filepath.Join(base, "actual", "codex") || exact.visits != visits {
		t.Fatalf("literal component work = %q, %v after %d visits; want %d", got, err, exact.visits, visits)
	}
	if got, err := ResolveRootSelectorBounded(selector, 256, &referentTestBudget{limit: visits - 1}); !errors.Is(err, errReferentTestBudget) || got != "" {
		t.Fatalf("literal component budget bypass = %q, %v", got, err)
	}
	rootBudget := &referentTestBudget{limit: 1}
	if got, err := ResolveRootSelectorBounded("/", 256, rootBudget); err != nil || got != "/" || rootBudget.visits != 1 {
		t.Fatalf("filesystem-root work = %q, %v after %d visits", got, err, rootBudget.visits)
	}
}

func TestRootSelectorCannotInventAMissingPhysicalParent(t *testing.T) {
	base := referentTestRoot(t)
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{
		base + "/missing/../file",
		base + "/missing/child/../../file",
		file + "/../file",
		file + "/",
	} {
		if got, err := ResolveRootSelectorBounded(selector, 256, &referentTestBudget{limit: 10_000}); err == nil || got != "" {
			t.Errorf("unavailable directory selector %q returned %q, %v", selector, got, err)
		}
	}
	if content, err := os.ReadFile(file); err != nil || string(content) != "unchanged" {
		t.Fatalf("read-only refusal changed the file: %q, %v", content, err)
	}
}

func TestResolveReferentPathBoundedNeedsOnlyParentSearchPermission(t *testing.T) {
	base := referentTestRoot(t)
	parent := filepath.Join(base, "search-only")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(parent, 0o700); err != nil {
			t.Error(err)
		}
	})
	file := filepath.Join(parent, "control\nname")
	if err := os.WriteFile(file, []byte("private"), 0o000); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(parent, "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o100); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, fifo} {
		resolved, err := ResolveReferentPathBounded(path, 256, &referentTestBudget{limit: 10_000})
		if err != nil || resolved != path {
			t.Fatalf("search-only observation returned %q, %v; want %q", resolved, err, path)
		}
	}
}
