package access_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func TestHashPathFileChangesWithContent(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "hook.py")

	if err := os.WriteFile(filePath, []byte("print('one')\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	firstHash, artifactKind, err := access.HashPath(context.Background(), filePath)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	if artifactKind != artifact.ArtifactKindFile {
		t.Fatalf("artifactKind = %q, want file", artifactKind)
	}

	if err := os.WriteFile(filePath, []byte("print('two')\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	secondHash, _, err := access.HashPath(context.Background(), filePath)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	if firstHash == secondHash {
		t.Fatalf("content hash did not change: %q", firstHash)
	}
}

func TestHashFileContentMatchesNonExecutableHashPathFile(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "settings.json")
	content := []byte("{\"hooks\":{}}\n")
	if err := os.WriteFile(filePath, content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	pathHash, artifactKind, err := access.HashPath(context.Background(), filePath)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	if artifactKind != artifact.ArtifactKindFile {
		t.Fatalf("artifactKind = %q, want file", artifactKind)
	}
	contentHash := artifact.HashFileContent(content)
	if contentHash != pathHash {
		t.Fatalf("HashFileContent = %q, HashPath = %q", contentHash, pathHash)
	}
}

func TestHashPathFileIgnoresReadWritePermissionBits(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "hook.py")

	if err := os.WriteFile(filePath, []byte("print('one')\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	firstHash, _, err := access.HashPath(context.Background(), filePath)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	if err := os.Chmod(filePath, 0o644); err != nil {
		t.Fatalf("Chmod returned error: %v", err)
	}

	secondHash, _, err := access.HashPath(context.Background(), filePath)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	if firstHash != secondHash {
		t.Fatalf("read/write permission bits changed hash: %q != %q", firstHash, secondHash)
	}
}

func TestHashPathFileChangesWithExecutableBit(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "hook.sh")

	if err := os.WriteFile(filePath, []byte("echo one\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	firstHash, _, err := access.HashPath(context.Background(), filePath)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	if err := os.Chmod(filePath, 0o700); err != nil {
		t.Fatalf("Chmod returned error: %v", err)
	}

	secondHash, _, err := access.HashPath(context.Background(), filePath)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	if firstHash == secondHash {
		t.Fatalf("executable bit did not change hash: %q", firstHash)
	}
}

func TestHashPathDirectoryIsDeterministic(t *testing.T) {
	tempDir := t.TempDir()
	writeTestFile(t, tempDir, "nested/b.txt", "bravo\n")
	writeTestFile(t, tempDir, "a.txt", "alpha\n")
	writeTestFile(t, tempDir, "nested/c.txt", "charlie\n")

	firstHash, artifactKind, err := access.HashPath(context.Background(), tempDir)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	if artifactKind != artifact.ArtifactKindDirectory {
		t.Fatalf("artifactKind = %q, want directory", artifactKind)
	}

	secondHash, _, err := access.HashPath(context.Background(), tempDir)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	if firstHash != secondHash {
		t.Fatalf("directory hash was not deterministic: %q != %q", firstHash, secondHash)
	}
}

func TestHashPathDirectoryIgnoresCreationOrder(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()

	writeTestFile(t, firstDir, "nested/b.txt", "bravo\n")
	writeTestFile(t, firstDir, "a.txt", "alpha\n")
	writeTestFile(t, firstDir, "nested/c.txt", "charlie\n")

	writeTestFile(t, secondDir, "nested/c.txt", "charlie\n")
	writeTestFile(t, secondDir, "a.txt", "alpha\n")
	writeTestFile(t, secondDir, "nested/b.txt", "bravo\n")

	firstHash, _, err := access.HashPath(context.Background(), firstDir)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	secondHash, _, err := access.HashPath(context.Background(), secondDir)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	if firstHash != secondHash {
		t.Fatalf("creation order changed hash: %q != %q", firstHash, secondHash)
	}
}

func TestHashPathHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := access.HashPath(ctx, t.TempDir())
	if err == nil {
		t.Fatal("HashPath returned nil error")
	}

	if err != context.Canceled {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestHashPathDirectoryChangesWithNestedContent(t *testing.T) {
	tempDir := t.TempDir()
	writeTestFile(t, tempDir, "SKILL.md", "---\nname: demo\n---\n")
	writeTestFile(t, tempDir, "scripts/run.sh", "echo one\n")

	firstHash, _, err := access.HashPath(context.Background(), tempDir)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	writeTestFile(t, tempDir, "scripts/run.sh", "echo two\n")

	secondHash, _, err := access.HashPath(context.Background(), tempDir)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	if firstHash == secondHash {
		t.Fatalf("directory hash did not change: %q", firstHash)
	}
}

func TestHashPathDirectoryIgnoresRootMetadata(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()

	writeTestFile(t, firstDir, "SKILL.md", "---\nname: demo\n---\n")
	writeTestFile(t, secondDir, "SKILL.md", "---\nname: demo\n---\n")

	if err := os.Chmod(firstDir, 0o700); err != nil {
		t.Fatalf("Chmod returned error: %v", err)
	}

	if err := os.Chmod(secondDir, 0o755); err != nil {
		t.Fatalf("Chmod returned error: %v", err)
	}

	firstHash, _, err := access.HashPath(context.Background(), firstDir)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	secondHash, _, err := access.HashPath(context.Background(), secondDir)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}

	if firstHash != secondHash {
		t.Fatalf("hash should ignore root metadata: %q != %q", firstHash, secondHash)
	}
}

func TestHashPathRejectsMissingPath(t *testing.T) {
	_, _, err := access.HashPath(context.Background(), filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("HashPath returned nil error")
	}

	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error = %q, want missing path diagnostic", err)
	}
}

func TestHashPathRejectsSymlink(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "target.txt")
	linkPath := filepath.Join(tempDir, "link.txt")

	if err := os.WriteFile(targetPath, []byte("target\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("symlinks are unavailable on this platform: %v", err)
	}

	_, _, err := access.HashPath(context.Background(), linkPath)
	if err == nil {
		t.Fatal("HashPath returned nil error")
	}

	if !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("error = %q, want symlink diagnostic", err)
	}
}

func TestHashPathRejectsSymlinkInsideDirectory(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "target.txt")
	linkPath := filepath.Join(tempDir, "link.txt")

	if err := os.WriteFile(targetPath, []byte("target\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("symlinks are unavailable on this platform: %v", err)
	}

	_, _, err := access.HashPath(context.Background(), tempDir)
	if err == nil {
		t.Fatal("HashPath returned nil error")
	}

	if !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("error = %q, want symlink diagnostic", err)
	}
}

func writeTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
