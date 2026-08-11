package journal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestCaptureProjectExistingDirectoryCreatesHashEquivalentRootedBackup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	destination := filepath.Join(root, ".agents", "skills", "review")
	if err := os.MkdirAll(filepath.Join(destination, "nested"), 0o700); err != nil {
		t.Fatalf("create project directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(destination, "nested"), 0o700)
		_ = os.Chmod(destination, 0o700)
	})
	if err := os.WriteFile(filepath.Join(destination, "SKILL.md"), []byte("skill\n"), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "nested", "run"), []byte("run\n"), 0o700); err != nil {
		t.Fatalf("write nested executable: %v", err)
	}
	if err := os.Chmod(destination, 0o500); err != nil {
		t.Fatalf("set project directory mode: %v", err)
	}
	if err := os.Chmod(filepath.Join(destination, "nested"), 0o500); err != nil {
		t.Fatalf("set nested directory mode: %v", err)
	}
	contentHash, _, err := access.HashPath(context.Background(), destination)
	if err != nil {
		t.Fatalf("hash project directory: %v", err)
	}
	session := mustManifestAuthoritySession(t, root)
	defer session.root.Close()
	mutation := pathMutation{
		Kind: pathMutationReplace, Scope: target.ScopeProject,
		Destination: outputtest.Parse(t, ".agents/skills/review"),
		LiveExists:  true, LiveHash: contentHash,
		LivePathExists: true, LivePathHash: contentHash,
	}
	operationDir := t.TempDir()
	before, backupIndex, err := captureProjectExistingRecoveryBeforePath(
		context.Background(),
		journalTestFilesystem(),
		operationDir,
		0,
		mutation,
		session,
	)
	if err != nil {
		t.Fatalf("captureProjectExistingRecoveryBeforePath returned error: %v", err)
	}
	if backupIndex != 1 || before.Kind != recovery.PathKindDirectory {
		t.Fatalf("capture = index %d kind %q, want 1 directory", backupIndex, before.Kind)
	}
	backupPath := filepath.Join(operationDir, filepath.FromSlash(before.BackupPath))
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(backupPath, "nested"), 0o700)
		_ = os.Chmod(backupPath, 0o700)
	})
	backupHash, backupKind, err := access.HashPath(context.Background(), backupPath)
	if err != nil {
		t.Fatalf("hash recovery backup: %v", err)
	}
	if backupKind != artifact.ArtifactKindDirectory || backupHash != contentHash {
		t.Fatalf("backup = %s %s, want directory %s", backupKind, backupHash, contentHash)
	}
	assertProjectBackupMode(t, backupPath, 0o500)
	assertProjectBackupMode(t, filepath.Join(backupPath, "nested"), 0o500)
}

func TestCaptureProjectExistingDirectoryRejectsAncestorSymlinkWithoutBackup(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "skills", "review"), 0o700); err != nil {
		t.Fatalf("create outside destination: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".agents")); err != nil {
		t.Fatalf("create project ancestor symlink: %v", err)
	}
	session := mustManifestAuthoritySession(t, root)
	defer session.root.Close()
	mutation := pathMutation{
		Kind: pathMutationReplace, Scope: target.ScopeProject,
		Destination: outputtest.Parse(t, ".agents/skills/review"),
		LiveExists:  true, LiveHash: "sha256:unreachable",
		LivePathExists: true, LivePathHash: "sha256:unreachable",
	}
	operationDir := t.TempDir()
	_, _, err := captureProjectExistingRecoveryBeforePath(
		context.Background(),
		journalTestFilesystem(),
		operationDir,
		0,
		mutation,
		session,
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureAncestorSymlink) {
		t.Fatalf("error = %v, want %s", err, rootedpath.FailureAncestorSymlink)
	}
	entries, readErr := os.ReadDir(operationDir)
	if readErr != nil {
		t.Fatalf("read operation directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("blocked capture left backup entries: %v", entries)
	}
}

func TestCaptureProjectExistingFileRejectsOversizedRecoveryBackup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(root, "large.bin")
	file, err := os.OpenFile(hostPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(recovery.MaximumRecoveryBackupFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	session := mustManifestAuthoritySession(t, root)
	defer session.root.Close()
	mutation := pathMutation{
		Kind: pathMutationReplace, Scope: target.ScopeProject,
		Destination: outputtest.Parse(t, "large.bin"),
		LiveExists:  true, LiveHash: artifact.ContentHash("sha256:" + strings.Repeat("0", 64)),
		LivePathExists: true,
		LivePathHash:   artifact.ContentHash("sha256:" + strings.Repeat("0", 64)),
	}
	operationDir := t.TempDir()

	_, _, err = captureProjectExistingRecoveryBeforePath(
		t.Context(),
		journalTestFilesystem(),
		operationDir,
		0,
		mutation,
		session,
	)
	if err == nil || !strings.Contains(err.Error(), "exceeds 134217728 bytes") {
		t.Fatalf("capture error = %v, want bounded recovery backup rejection", err)
	}
	if entries, readErr := os.ReadDir(operationDir); readErr != nil || len(entries) != 0 {
		t.Fatalf("blocked capture entries = %v, error = %v; want none", entries, readErr)
	}
}

func TestObserveProjectRecoveryPathRejectsOversizedRegularFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(root, "large.bin")
	file, err := os.OpenFile(hostPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(recovery.MaximumRecoveryBackupFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	session := mustManifestAuthoritySession(t, root)
	defer session.root.Close()

	observation, err := observeProjectRecoveryPath(
		t.Context(),
		"large.bin",
		"",
		nil,
		journalTestFilesystem(),
		session,
		journalTestCodecs(),
		recoveryBackupBudgetForTest(t),
	)
	if err != nil {
		t.Fatalf("observe oversized project recovery path: %v", err)
	}
	if !observation.Exists || !strings.Contains(observation.Error, "exceeds 134217728 bytes") {
		t.Fatalf("observation = %#v, want bounded regular-file error", observation)
	}
}

func TestObserveProjectRecoveryPathUsesRootedDirectoryHashAndBlocksSymlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	destination := filepath.Join(root, "tree")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "entry"), []byte("payload"), 0o600); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	wantHash, _, err := access.HashPath(context.Background(), destination)
	if err != nil {
		t.Fatalf("hash directory: %v", err)
	}
	session := mustManifestAuthoritySession(t, root)
	defer session.root.Close()
	observation, err := observeProjectRecoveryPath(
		context.Background(),
		"tree",
		"",
		nil,
		journalTestFilesystem(),
		session,
		journalTestCodecs(),
		recoveryBackupBudgetForTest(t),
	)
	if err != nil {
		t.Fatalf("observe project recovery directory: %v", err)
	}
	if observation.Error != "" || observation.Kind != recovery.PathKindDirectory ||
		observation.ContentHash != string(wantHash) {
		t.Fatalf("observation = %#v, want rooted directory hash %s", observation, wantHash)
	}

	if err := os.Rename(destination, filepath.Join(root, "moved")); err != nil {
		t.Fatalf("move directory: %v", err)
	}
	if err := os.Symlink("moved", destination); err != nil {
		t.Fatalf("create final symlink: %v", err)
	}
	symlinkObservation, err := observeProjectRecoveryPath(
		context.Background(),
		"tree",
		"",
		nil,
		journalTestFilesystem(),
		session,
		journalTestCodecs(),
		recoveryBackupBudgetForTest(t),
	)
	if err != nil {
		t.Fatalf("observe symlinked project recovery path: %v", err)
	}
	if symlinkObservation.Error != "" ||
		symlinkObservation.Kind != recovery.PathKindSymlink ||
		symlinkObservation.LinkTarget != "moved" {
		t.Fatalf("symlink observation = %#v, want target moved", symlinkObservation)
	}
}

func TestObserveProjectRecoveryPathReportsRootedAbsence(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	session := mustManifestAuthoritySession(t, root)
	defer session.root.Close()

	observation, err := observeProjectRecoveryPath(
		context.Background(),
		"missing",
		"",
		nil,
		journalTestFilesystem(),
		session,
		journalTestCodecs(),
		recoveryBackupBudgetForTest(t),
	)
	if err != nil {
		t.Fatalf("observe absent project recovery path: %v", err)
	}
	if observation.Error != "" || observation.Exists || observation.Path != "missing" {
		t.Fatalf("missing observation = %#v", observation)
	}
}

func mustManifestAuthoritySession(t *testing.T, root string) *manifestAuthoritySession {
	t.Helper()
	captured := mustJournalProjectRoot(t, root)
	session, err := newManifestAuthoritySession(captured, false)
	if err != nil {
		_ = captured.Close()
		t.Fatalf("newManifestAuthoritySession returned error: %v", err)
	}
	return session
}

func assertProjectBackupMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat project backup %q: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want.Perm() {
		t.Fatalf("project backup %q mode = %04o, want %04o", path, got, want.Perm())
	}
}
