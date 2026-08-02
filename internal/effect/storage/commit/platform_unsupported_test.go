//go:build !darwin && !linux

package commit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUnsupportedPlatformFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daem-state")
	request, err := NewFileCreate(path, []byte("payload"), 0o600)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	treePath := filepath.Join(filepath.Dir(path), "active-tree")
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "capture identity", run: func() error {
			_, captureErr := CaptureEntryIdentity(context.Background(), path)
			return captureErr
		}},
		{name: "prepare parent", run: func() error {
			err := PrepareCommitParent(context.Background(), path)
			return err
		}},
		{name: "read regular file", run: func() error {
			_, _, readErr := ReadRegularFile(context.Background(), path)
			return readErr
		}},
		{name: "read regular file snapshot", run: func() error {
			_, readErr := ReadRegularFileSnapshot(context.Background(), path)
			return readErr
		}},
		{name: "read bounded regular file snapshot", run: func() error {
			_, readErr := ReadRegularFileSnapshotUpTo(context.Background(), path, 64)
			return readErr
		}},
		{name: "snapshot directory", run: func() error {
			_, readErr := SnapshotDirectory(context.Background(), path, 16)
			return readErr
		}},
		{name: "file create", run: func() error {
			return CommitFile(context.Background(), request)
		}},
		{name: "file replace", run: func() error {
			return CommitFile(context.Background(), FileCommit{path: path, policy: filePolicyReplaceExpected})
		}},
		{name: "tree publish", run: func() error {
			return CommitPreparedTree(context.Background(), PreparedTreeCommit{destination: treePath})
		}},
		{name: "logical removal", run: func() error {
			return CommitLogicalRemoval(context.Background(), LogicalRemoval{path: path})
		}},
		{name: "rooted entry rename", run: func() error {
			_, renameErr := CommitRootedEntryRename(
				context.Background(),
				RootedEntryRename{sourcePath: path},
			)
			return renameErr
		}},
		{name: "rooted entry cleanup", run: func() error {
			_, cleanupErr := CommitRootedEntryCleanup(
				context.Background(),
				RootedEntryCleanup{path: path},
			)
			return cleanupErr
		}},
		{name: "prepared rooted tree outcome", run: func() error {
			prepared := &PreparedRootedTree{destination: treePath}
			_, commitErr := prepared.CommitWithOutcome(context.Background())
			return commitErr
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertUnsupportedFailure(t, test.run())
		})
	}
	for _, candidate := range []string{path, treePath} {
		if _, statErr := os.Lstat(candidate); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("unsupported adapter changed %q: %v", candidate, statErr)
		}
	}
}

func assertUnsupportedFailure(t *testing.T, err error) {
	t.Helper()
	var failure *failure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want *failure", err)
	}
	if failure.Kind() != failureUnsupportedGuarantee {
		t.Fatalf("kind = %s, want %s", failure.Kind(), failureUnsupportedGuarantee)
	}
	if failure.failedPhase() != string(phaseUnsupported) {
		t.Fatalf("phase = %s, want %s", failure.failedPhase(), phaseUnsupported)
	}
}
