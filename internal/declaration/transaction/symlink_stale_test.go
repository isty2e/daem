package transaction

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestCapturedDeclarationRejectsEquivalentContentRetarget(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	selected := filepath.Join(root, "daem.toml")
	lockfile := filepath.Join(root, "daem.lock.toml")
	writeAdapterFixture(t, first, "same", 0o600)
	writeAdapterFixture(t, second, "same", 0o600)
	writeAdapterFixture(t, lockfile, "lock-before", 0o600)
	if err := os.Symlink("first", selected); err != nil {
		t.Fatal(err)
	}
	manifestTarget := captureAdapterTarget(t, selected)
	lockTarget := captureAdapterTarget(t, lockfile)
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("second", selected); err != nil {
		t.Fatal(err)
	}
	err := CommitTransaction(context.Background(), TransactionInput{
		ManifestTarget: manifestTarget,
		LockfileTarget: lockTarget,
		StateDir:       filepath.Join(root, ".daem"),
		ManifestBytes:  []byte("must-not-write"),
		LockfileBytes:  []byte("must-not-write"),
	})
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("retarget error = %v, want stale snapshot", err)
	}
	assertAdapterContent(t, first, "same")
	assertAdapterContent(t, second, "same")
	assertAdapterContent(t, lockfile, "lock-before")
}
