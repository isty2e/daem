//go:build darwin || linux

package fsclock

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWaitForTickSeparatesChangesWithoutTouchingFixture(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "entry")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	var before, rootBefore unix.Stat_t
	if err := unix.Stat(path, &before); err != nil {
		t.Fatal(err)
	}
	if err := unix.Stat(root, &rootBefore); err != nil {
		t.Fatal(err)
	}

	WaitForTick(t, path)

	var unchanged, rootAfter unix.Stat_t
	if err := unix.Stat(path, &unchanged); err != nil {
		t.Fatal(err)
	}
	if err := unix.Stat(root, &rootAfter); err != nil {
		t.Fatal(err)
	}
	if unchanged.Ctim != before.Ctim || rootBefore.Ctim != rootAfter.Ctim {
		t.Fatal("clock probe changed the fixture or its parent")
	}
	if err := os.WriteFile(path, []byte("after!"), 0o600); err != nil {
		t.Fatal(err)
	}
	var after unix.Stat_t
	if err := unix.Stat(path, &after); err != nil {
		t.Fatal(err)
	}
	if after.Ctim == before.Ctim {
		t.Fatal("in-place mutation retained the pre-tick ctime")
	}
}
