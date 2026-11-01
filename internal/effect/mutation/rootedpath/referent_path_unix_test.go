//go:build darwin || linux

package rootedpath

import (
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
