//go:build darwin || linux

package execute

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func TestRecoveryBackupReadsEmptyFileWithReservedProbeCapacity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "empty")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	work, err := recovery.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	backup := boundRecoveryBackupForTest(
		t,
		root,
		"empty",
		recovery.PathKindFile,
		string(artifact.HashFileContent(nil)),
		work,
		1,
	)

	content, err := backup.readFile(t.Context())
	if err != nil {
		t.Fatalf("read empty recovery backup: %v", err)
	}
	if len(content) != 0 {
		t.Fatalf("empty recovery backup length = %d, want 0", len(content))
	}
}

func TestRecoveryBackupCopiesEmptyDirectoryWithExactZeroWork(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "empty")
	if err := os.Mkdir(path, 0o750); err != nil {
		t.Fatal(err)
	}
	hash, kind, err := access.HashPath(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	work, err := recovery.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	backup := boundRecoveryBackupForTest(t, root, "empty", string(kind), string(hash), work, 1)
	writer := &recordingRecoveryTreeWriter{}

	if err := backup.copyDirectory(t.Context(), writer); err != nil {
		t.Fatalf("copy empty recovery directory: %v", err)
	}
	if writer.rootMode.Perm() != 0o750 || writer.entries != 0 {
		t.Fatalf(
			"empty recovery directory = mode:%04o entries:%d, want 0750/0",
			writer.rootMode.Perm(),
			writer.entries,
		)
	}
}

func TestRollbackBackupCopiesEmptyDirectoryWithProofAllowance(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "empty")
	if err := os.Mkdir(path, 0o750); err != nil {
		t.Fatal(err)
	}
	hash, kind, err := access.HashPath(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := newRollbackBackup(path, "empty", string(kind), string(hash))
	if err != nil {
		t.Fatal(err)
	}
	work, err := recovery.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	writer := &recordingRecoveryTreeWriter{}

	if err := (boundedRollbackDirectorySource{backup: backup, work: work}).copyDirectory(
		t.Context(),
		writer,
	); err != nil {
		t.Fatalf("copy empty rollback directory: %v", err)
	}
	if writer.rootMode.Perm() != 0o750 || writer.entries != 0 {
		t.Fatalf(
			"empty rollback directory = mode:%04o entries:%d, want 0750/0",
			writer.rootMode.Perm(),
			writer.entries,
		)
	}
}

func TestRollbackBackupCopiesZeroByteFilesWithZeroSemanticBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tree")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "empty"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	hash, kind, err := access.HashPath(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := newRollbackBackup(path, "zero-byte-tree", string(kind), string(hash))
	if err != nil {
		t.Fatal(err)
	}
	work, err := recovery.NewArtifactWork(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	writer := &recordingRecoveryTreeWriter{}

	if err := (boundedRollbackDirectorySource{backup: backup, work: work}).copyDirectory(
		t.Context(),
		writer,
	); err != nil {
		t.Fatalf("copy zero-byte rollback tree: %v", err)
	}
	if writer.entries != 1 {
		t.Fatalf("zero-byte rollback tree entries = %d, want 1", writer.entries)
	}
}

func TestRecoveryBackupRejectsDirectoryGrowthBeyondExactWork(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tree")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	hash, kind, err := access.HashPath(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	work, err := recovery.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	backup := boundRecoveryBackupForTest(t, root, "tree", string(kind), string(hash), work, 1)
	if err := os.WriteFile(filepath.Join(path, "appeared"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := backup.copyDirectory(t.Context(), &recordingRecoveryTreeWriter{}); err == nil {
		t.Fatal("recovery directory growth was accepted")
	}
}

func TestRecoveryBackupRejectsExecutableModeDrift(t *testing.T) {
	root := t.TempDir()
	content := []byte("content")
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	work, err := recovery.NewArtifactWork(0, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	backup := boundRecoveryBackupForTest(
		t,
		root,
		"file",
		recovery.PathKindFile,
		string(artifact.HashFileContentWithExecutable(content, false)),
		work,
		1,
	)
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := backup.readFile(t.Context()); err == nil {
		t.Fatal("recovery file executable-mode drift was accepted")
	}
}

func TestRecoveryBackupReservesEverySharedUse(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "shared")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	work, err := recovery.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	backup := boundRecoveryBackupForTest(
		t,
		root,
		"shared",
		recovery.PathKindFile,
		string(artifact.HashFileContent(nil)),
		work,
		2,
	)

	for index := range 2 {
		if _, err := backup.readFile(t.Context()); err != nil {
			t.Fatalf("read shared recovery backup use %d: %v", index, err)
		}
	}
}

func TestRecoveryBackupPreservesCancellation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	work, err := recovery.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	backup := boundRecoveryBackupForTest(
		t,
		root,
		"file",
		recovery.PathKindFile,
		string(artifact.HashFileContent(nil)),
		work,
		1,
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := backup.readFile(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled recovery backup error = %v, want context cancellation", err)
	}
}

func boundRecoveryBackupForTest(
	t *testing.T,
	rootPath string,
	relativePath string,
	kind string,
	contentHash string,
	work recovery.ArtifactWork,
	uses int,
) recoveryBackup {
	t.Helper()
	canonicalRoot, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(
		canonicalRoot,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	authority, err := root.AuthorityBounded(budget)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination(relativePath)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatal(err)
	}
	backupAuthority, err := newRecoveryBackupAuthority(
		root,
		destination,
		relativePath,
		kind,
		contentHash,
		work,
	)
	if err != nil {
		t.Fatal(err)
	}
	for range uses {
		if err := root.ReserveDestinationAccess(
			destination,
			recovery.MaximumPhysicalPathDepth,
			recoveryBackupPathReservation{budget: budget},
		); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case recovery.PathKindFile:
			err = budget.ReserveBackupFileExecution(work)
		case recovery.PathKindDirectory:
			err = budget.ReserveBackupDirectoryExecution(work)
		default:
			t.Fatalf("unsupported backup kind %q", kind)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	execution, err := budget.BeginReservedBackupExecution()
	if err != nil {
		t.Fatal(err)
	}
	backup, err := backupAuthority.bind(testFilesystem(), execution)
	if err != nil {
		t.Fatal(err)
	}
	return backup
}

type recordingRecoveryTreeWriter struct {
	rootMode fs.FileMode
	entries  int
}

func (writer *recordingRecoveryTreeWriter) SetRootMode(mode fs.FileMode) error {
	writer.rootMode = mode.Perm()
	return nil
}

func (writer *recordingRecoveryTreeWriter) CreateDirectory(
	mutationfs.TreeRelativePath,
	fs.FileMode,
) error {
	writer.entries++
	return nil
}

func (writer *recordingRecoveryTreeWriter) WriteFile(
	_ mutationfs.TreeRelativePath,
	_ fs.FileMode,
	content io.Reader,
) error {
	if _, err := io.Copy(io.Discard, content); err != nil {
		return err
	}
	writer.entries++
	return nil
}
