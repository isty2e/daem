//go:build darwin || linux

package transaction

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/declarationartifact"
)

func TestReadonlyLinkedLockRollsBackAndDoesNotFenceNextWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged replacement permission checks")
	}
	root := t.TempDir()
	project := filepath.Join(root, "project")
	readonly := filepath.Join(root, "z-readonly")
	for _, dir := range []string{project, readonly} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(project, "daem.toml")
	lockfile := filepath.Join(project, "daem.lock.toml")
	writeAdapterFixture(t, filepath.Join(root, "a-manifest"), "manifest-before", 0o640)
	writeAdapterFixture(t, filepath.Join(readonly, "lock"), "lock-before", 0o400)
	if err := os.Symlink("../a-manifest", manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../z-readonly/lock", lockfile); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readonly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o700) })
	stateDir := filepath.Join(project, ".daem")
	err := CommitTransaction(context.Background(), TransactionInput{
		ManifestTarget: captureAdapterTarget(t, manifest),
		LockfileTarget: captureAdapterTarget(t, lockfile),
		StateDir:       stateDir,
		ManifestBytes:  []byte("manifest-after"),
		LockfileBytes:  []byte("lock-after"),
	})
	var replacement declarationartifact.ReplacementError
	if !errors.Is(err, fs.ErrPermission) || !errors.As(err, &replacement) {
		t.Fatalf("write error = %v, want declaration target permission classification", err)
	}
	assertAdapterContent(t, manifest, "manifest-before")
	assertAdapterContent(t, lockfile, "lock-before")
	if _, err := os.Lstat(filepath.Join(stateDir, "metadata-transaction")); !os.IsNotExist(err) {
		t.Fatalf("rollback retained a marker: %v", err)
	}
	for _, file := range []struct{ path, link string }{
		{manifest, "../a-manifest"}, {lockfile, "../z-readonly/lock"},
	} {
		if link, err := os.Readlink(file.path); err != nil || link != file.link {
			t.Fatalf("selected link changed: %q, %v", link, err)
		}
	}
	err = CommitTransaction(context.Background(), TransactionInput{
		ManifestTarget:    captureAdapterTarget(t, manifest),
		LockfileTarget:    captureAdapterTarget(t, lockfile),
		StateDir:          stateDir,
		ManifestBytes:     []byte("next-write"),
		SkipLockfileWrite: true,
	})
	if err != nil {
		t.Fatalf("retained readonly lockfile blocked next write: %v", err)
	}
	assertAdapterContent(t, manifest, "next-write")
	assertAdapterContent(t, lockfile, "lock-before")
}
