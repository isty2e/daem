//go:build windows

package commit

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func createWindowsStorageTestJunction(t *testing.T, link string, target string) {
	t.Helper()
	output, err := exec.Command("cmd", "/C", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create junction %q -> %q: %v: %s", link, target, err, output)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}

func TestWindowsUnrootedStorageRejectsReparseAncestors(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	alias := filepath.Join(root, "alias")
	createWindowsStorageTestJunction(t, alias, outside)
	destination := filepath.Join(alias, "state", "state.json")

	assertOutsideUntouched := func() {
		t.Helper()
		if _, statErr := os.Lstat(filepath.Join(outside, "state")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("outside referent affected by unrooted operation: %v", statErr)
		}
	}

	t.Run("commit file", func(t *testing.T) {
		request, err := NewFileCreate(destination, []byte("payload"), 0o600)
		if err != nil {
			t.Fatal(err)
		}
		commitErr := CommitFile(t.Context(), request)
		if commitErr == nil {
			t.Fatal("unrooted commit through reparse ancestor succeeded")
		}
		if !hasStorageFailureKind(commitErr, mutationfs.FailureUncommitted) {
			t.Fatalf("failure kind = %v, want uncommitted", commitErr)
		}
		var boundary *rootedpath.Failure
		if !errors.As(commitErr, &boundary) || boundary.Kind() != rootedpath.FailureRootReplaced {
			t.Fatalf("boundary failure = %v, want rooted %v", commitErr, rootedpath.FailureRootReplaced)
		}
		assertOutsideUntouched()
		assertNoWindowsStorageResidue(t, root)
	})

	t.Run("prepare parent", func(t *testing.T) {
		err := PrepareCommitParent(t.Context(), destination)
		if err == nil {
			t.Fatal("ancestor preparation through reparse ancestor succeeded")
		}
		assertOutsideUntouched()
		assertNoWindowsStorageResidue(t, root)
	})

	t.Run("capture identity", func(t *testing.T) {
		if _, err := CaptureEntryIdentity(t.Context(), destination); err == nil {
			t.Fatal("identity capture through reparse ancestor succeeded")
		}
		assertOutsideUntouched()
	})
}
