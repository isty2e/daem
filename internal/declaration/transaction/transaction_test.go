package transaction

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCommitTransactionWritesManifestAndLockfile(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	stateDir := filepath.Join(root, ".daem")
	writeAdapterFixture(t, manifestPath, "manifest-before", 0o644)

	err := CommitTransaction(context.Background(), TransactionInput{
		ManifestPath:  manifestPath,
		LockfilePath:  lockfilePath,
		StateDir:      stateDir,
		ManifestBytes: []byte("manifest-after"),
		LockfileBytes: []byte("lock-after"),
	})
	if err != nil {
		t.Fatalf("CommitTransaction returned error: %v", err)
	}
	assertAdapterContent(t, manifestPath, "manifest-after")
	assertAdapterContent(t, lockfilePath, "lock-after")
	if info, err := os.Stat(manifestPath); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("manifest mode = %#v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "metadata-transaction")); !os.IsNotExist(err) {
		t.Fatalf("transaction evidence remains: %v", err)
	}
}

func TestCommitTransactionRetainsUnchangedLockfile(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	writeAdapterFixture(t, manifestPath, "manifest-before", 0o600)
	writeAdapterFixture(t, lockfilePath, "lock-before", 0o600)

	err := CommitTransaction(context.Background(), TransactionInput{
		ManifestPath:      manifestPath,
		LockfilePath:      lockfilePath,
		StateDir:          filepath.Join(root, ".daem"),
		ManifestBytes:     []byte("manifest-after"),
		LockfileBytes:     []byte("ignored"),
		SkipLockfileWrite: true,
	})
	if err != nil {
		t.Fatalf("CommitTransaction returned error: %v", err)
	}
	assertAdapterContent(t, manifestPath, "manifest-after")
	assertAdapterContent(t, lockfilePath, "lock-before")
}

func writeAdapterFixture(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func assertAdapterContent(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("%s = %q, want %q", path, content, want)
	}
}
