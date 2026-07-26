package journal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
)

func TestCaptureGlobalExistingDirectoryUsesRootedCapabilityAndHashEquivalentBackup(t *testing.T) {
	root, destination, contentHash := rootedGlobalDirectoryFixture(t)
	action := rootedGlobalUpdateAction(contentHash)
	operationDir := t.TempDir()
	callbackUsed := false

	before, backupIndex, err := captureRecoveryBeforePath(
		context.Background(),
		operationDir,
		0,
		action,
		journalTestFilesystem(),
		func(output.Destination) (string, error) {
			return destination.LexicalPath()
		},
		nil,
		func(requested output.Destination) (rootedpath.CommitCapability, bool, error) {
			callbackUsed = true
			if requested != action.Destination {
				return nil, false, fmt.Errorf("unexpected rooted destination %q", requested)
			}
			capability, err := root.Acquire(destination)
			return capability, true, err
		},
		nil,
	)
	if err != nil {
		t.Fatalf("captureRecoveryBeforePath returned error: %v", err)
	}
	if !callbackUsed {
		t.Fatal("capture did not request retained rooted authority")
	}
	if backupIndex != 1 || before.Kind != recovery.PathKindDirectory {
		t.Fatalf("capture = index %d kind %q, want 1 directory", backupIndex, before.Kind)
	}
	backupHash, backupKind, err := access.HashPath(
		context.Background(),
		filepath.Join(operationDir, filepath.FromSlash(before.BackupPath)),
	)
	if err != nil {
		t.Fatalf("hash rooted recovery backup: %v", err)
	}
	if backupKind != artifact.ArtifactKindDirectory || backupHash != contentHash {
		t.Fatalf("backup = %s %s, want directory %s", backupKind, backupHash, contentHash)
	}
}

func TestCaptureGlobalExistingDirectoryRejectsBackupHashDifferentFromObservation(t *testing.T) {
	root, destination, contentHash := rootedGlobalDirectoryFixture(t)
	wrongHash := artifact.ContentHash("sha256:" + strings.Repeat("0", 64))
	if wrongHash == contentHash {
		t.Fatal("test fixture unexpectedly has the adversarial hash")
	}
	action := rootedGlobalUpdateAction(wrongHash)
	operationDir := t.TempDir()

	_, backupIndex, err := captureRecoveryBeforePath(
		context.Background(),
		operationDir,
		0,
		action,
		journalTestFilesystem(),
		func(output.Destination) (string, error) {
			return destination.LexicalPath()
		},
		nil,
		func(output.Destination) (rootedpath.CommitCapability, bool, error) {
			capability, err := root.Acquire(destination)
			return capability, true, err
		},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "does not match live observation") {
		t.Fatalf("capture error = %v, want copied-backup hash rejection", err)
	}
	if backupIndex != 1 {
		t.Fatalf("backup index = %d, want 1", backupIndex)
	}
	backupHash, backupKind, hashErr := access.HashPath(
		context.Background(),
		filepath.Join(operationDir, "files", "000001"),
	)
	if hashErr != nil {
		t.Fatalf("hash rejected rooted recovery backup: %v", hashErr)
	}
	if backupKind != artifact.ArtifactKindDirectory || backupHash != contentHash {
		t.Fatalf("rejected backup = %s %s, want copied directory %s", backupKind, backupHash, contentHash)
	}
}

func TestCaptureGlobalExistingDirectoryRefusesMissingStrictRootAuthority(t *testing.T) {
	_, destination, contentHash := rootedGlobalDirectoryFixture(t)
	action := rootedGlobalUpdateAction(contentHash)
	resolvedPath, err := destination.LexicalPath()
	if err != nil {
		t.Fatalf("read rooted destination path: %v", err)
	}

	_, _, err = captureRecoveryBeforePath(
		context.Background(),
		t.TempDir(),
		0,
		action,
		journalTestFilesystem(),
		func(output.Destination) (string, error) { return resolvedPath, nil },
		nil,
		func(output.Destination) (rootedpath.CommitCapability, bool, error) {
			return nil, false, nil
		},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "has no retained root authority") {
		t.Fatalf("capture error = %v, want missing strict root authority refusal", err)
	}
}

func TestCaptureGlobalFileFallbackUsesBoundedStableSnapshot(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(directory, "AGENTS.md")
	content := []byte("before\n")
	if err := os.WriteFile(hostPath, content, 0o700); err != nil {
		t.Fatal(err)
	}
	contentHash := artifact.HashFileContentWithExecutable(content, true)
	action := pathMutation{
		Kind: pathMutationReplace, Scope: target.ScopeGlobal,
		Destination: output.Destination("~/.codex/AGENTS.md"),
		LiveExists:  true, LiveHash: contentHash,
		LivePathExists: true, LivePathHash: contentHash,
	}
	operationDir := t.TempDir()

	before, backupIndex, err := captureExistingRecoveryBeforePath(
		t.Context(),
		journalTestFilesystem(),
		operationDir,
		0,
		action,
		func(output.Destination) (string, error) { return hostPath, nil },
	)
	if err != nil {
		t.Fatalf("captureExistingRecoveryBeforePath returned error: %v", err)
	}
	if backupIndex != 1 || before.Kind != recovery.PathKindFile ||
		before.PathMode == nil || before.PathMode.FileMode() != 0o700 {
		t.Fatalf("capture = index %d before %#v, want executable file backup", backupIndex, before)
	}
	backupPath := filepath.Join(operationDir, filepath.FromSlash(before.BackupPath))
	got, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("backup content = %q, want %q", got, content)
	}
}

func rootedGlobalDirectoryFixture(
	t *testing.T,
) (*rootedpath.CapturedRoot, rootedpath.Destination, artifact.ContentHash) {
	t.Helper()
	hostPath := filepath.Join(t.TempDir(), "managed")
	if err := os.Mkdir(hostPath, 0o700); err != nil {
		t.Fatalf("create global managed directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hostPath, "SKILL.md"), []byte("managed\n"), 0o600); err != nil {
		t.Fatalf("write global managed directory: %v", err)
	}
	contentHash, kind, err := access.HashPath(context.Background(), hostPath)
	if err != nil {
		t.Fatalf("hash global managed directory: %v", err)
	}
	if kind != artifact.ArtifactKindDirectory {
		t.Fatalf("managed fixture kind = %q, want directory", kind)
	}
	root, destination, err := rootedpath.CaptureDestination(hostPath)
	if err != nil {
		t.Fatalf("capture global managed destination: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, destination, contentHash
}

func rootedGlobalUpdateAction(contentHash artifact.ContentHash) pathMutation {
	return pathMutation{
		Kind: pathMutationReplace, Scope: target.ScopeGlobal,
		Destination: output.Destination("~/.agents/skills/review"),
		LiveExists:  true, LiveHash: contentHash,
		LivePathExists: true, LivePathHash: contentHash,
	}
}
