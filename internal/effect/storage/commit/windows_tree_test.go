//go:build windows

package commit

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func TestWindowsPreparedRootedTreeCommitAndSnapshot(t *testing.T) {
	root := t.TempDir()
	destination := root + `\tree`
	capability := acquireWindowsTestCommitCapability(t, destination)
	directory := mustWindowsTreePath(t, "nested")
	file := mustWindowsTreePath(t, "nested", "entry.txt")
	prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.SetRootMode(0o755); err != nil {
			return err
		}
		if err := writer.CreateDirectory(directory, 0o755); err != nil {
			return err
		}
		return writer.WriteFile(file, 0o644, strings.NewReader("payload"))
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome, identity, err := prepared.CommitWithPublishedIdentity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State() != mutationfs.CommitOutcomeComplete || identity.Kind() != mutationfs.EntryKindDirectory {
		t.Fatalf("prepared tree outcome = %q identity = %#v", outcome.State(), identity)
	}
	assertWindowsRegularFileSnapshot(t, destination+`\nested\entry.txt`, "payload", 0o644)
	snapshot, err := SnapshotDirectory(t.Context(), destination, 10)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RootMode() != 0o755 || len(snapshot.Entries()) != 1 || snapshot.Entries()[0].Name() != "nested" {
		t.Fatalf("prepared tree root snapshot = mode %04o entries %+v", snapshot.RootMode(), snapshot.Entries())
	}
	assertNoWindowsStorageResidue(t, root)
}

func TestWindowsPreparedRootedTreeRejectsDescendantReplacementAndPreservesItOnAbort(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "tree")
	prepared, err := PrepareRootedTree(
		t.Context(),
		acquireWindowsTestCommitCapability(t, destination),
		func(writer mutationfs.RootedTreeWriter) error {
			return writer.WriteFile(mustWindowsTreePath(t, "entry"), 0o600, strings.NewReader("payload"))
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(prepared.stagePath, "entry")
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	replacement, err := NewFileCreate(entry, []byte("payload"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), replacement); err != nil {
		t.Fatal(err)
	}
	_, err = prepared.CommitWithOutcome(t.Context())
	if !hasStorageFailureKind(err, mutationfs.FailureRetainedResidue) {
		t.Fatalf("replacement commit error = %v, want retained residue", err)
	}
	if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("replacement destination = %v, want absent", statErr)
	}
	content, readErr := os.ReadFile(entry)
	if readErr != nil || string(content) != "payload" {
		t.Fatalf("replacement entry = %q, %v; want preserved", content, readErr)
	}
}

func TestWindowsPreparedRootedTreeRejectsHardLinkedFile(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "tree")
	prepared, err := PrepareRootedTree(
		t.Context(),
		acquireWindowsTestCommitCapability(t, destination),
		func(writer mutationfs.RootedTreeWriter) error {
			return writer.WriteFile(mustWindowsTreePath(t, "entry"), 0o600, strings.NewReader("payload"))
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Link(filepath.Join(prepared.stagePath, "entry"), alias); err != nil {
		t.Fatal(err)
	}
	_, err = prepared.CommitWithOutcome(t.Context())
	if !hasStorageFailureKind(err, mutationfs.FailureRetainedResidue) {
		t.Fatalf("hard-linked commit error = %v, want retained residue", err)
	}
	if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("hard-linked destination = %v, want absent", statErr)
	}
	content, readErr := os.ReadFile(alias)
	if readErr != nil || string(content) != "payload" {
		t.Fatalf("external alias = %q, %v; want preserved", content, readErr)
	}
}

func TestWindowsPreparedRootedTreeCloseFailureRetainsPublishedDestinationEvidence(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "tree")
	capability := acquireWindowsTestCommitCapability(t, destination)
	authorizedDestination, err := rootedCapabilityPath(capability)
	if err != nil {
		_ = capability.Close()
		t.Fatal(err)
	}
	prepared, err := PrepareRootedTree(
		t.Context(),
		capability,
		func(writer mutationfs.RootedTreeWriter) error {
			return writer.WriteFile(mustWindowsTreePath(t, "entry"), 0o600, strings.NewReader("payload"))
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = prepared.commitWithPublishedIdentityAndFaults(
		t.Context(),
		faultPlan{failures: map[phase]error{phaseClosePayload: errors.New("injected close failure")}},
	)
	if !hasStorageFailureKind(err, mutationfs.FailureIndeterminateCommit) {
		t.Fatalf("close failure = %v, want indeterminate", err)
	}
	var storageFailure *failure
	if !errors.As(err, &storageFailure) {
		t.Fatalf("close failure type = %T, want storage failure", err)
	}
	residue := storageFailure.retainedResidue()
	if len(residue) != 1 || residue[0] != authorizedDestination {
		t.Fatalf("close failure residue = %v, want %q", residue, authorizedDestination)
	}
}

func TestWindowsPreparedRootedTreeAbortAndPopulateFailureRemoveStage(t *testing.T) {
	for _, testCase := range []struct {
		name string
		run  func(*testing.T, rootedpath.CommitCapability, string) error
	}{
		{
			name: "abort",
			run: func(t *testing.T, capability rootedpath.CommitCapability, destination string) error {
				prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
					return writer.WriteFile(mustWindowsTreePath(t, "entry"), 0o600, strings.NewReader("payload"))
				})
				if err != nil {
					return err
				}
				return prepared.Abort(t.Context())
			},
		},
		{
			name: "populate failure",
			run: func(t *testing.T, capability rootedpath.CommitCapability, destination string) error {
				_, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
					if writeErr := writer.WriteFile(mustWindowsTreePath(t, "entry"), 0o600, strings.NewReader("payload")); writeErr != nil {
						return writeErr
					}
					return errors.New("populate failed")
				})
				if err == nil {
					return errors.New("populate failure unexpectedly succeeded")
				}
				return nil
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			destination := root + `\tree`
			if err := testCase.run(t, acquireWindowsTestCommitCapability(t, destination), destination); err != nil {
				t.Fatal(err)
			}
			assertNoWindowsStorageResidue(t, root)
		})
	}
}

func TestWindowsPreparedRootedTreeWriteFileFailureRemovesPartialChild(t *testing.T) {
	t.Run("payload error", func(t *testing.T) {
		root := t.TempDir()
		destination := root + `\tree`
		_, err := PrepareRootedTree(t.Context(), acquireWindowsTestCommitCapability(t, destination), func(writer mutationfs.RootedTreeWriter) error {
			return writer.WriteFile(
				mustWindowsTreePath(t, "partial"),
				0o600,
				&windowsPartialPayloadReader{remaining: []byte("partial-payload"), fail: errors.New("injected payload failure")},
			)
		})
		if err == nil || !strings.Contains(err.Error(), "injected payload failure") {
			t.Fatalf("PrepareRootedTree error = %v, want injected payload failure", err)
		}
		if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed populate published destination: %v", statErr)
		}
		assertNoWindowsStorageResidue(t, root)
	})
	t.Run("cancellation", func(t *testing.T) {
		root := t.TempDir()
		destination := root + `\tree`
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		_, err := PrepareRootedTree(ctx, acquireWindowsTestCommitCapability(t, destination), func(writer mutationfs.RootedTreeWriter) error {
			return writer.WriteFile(
				mustWindowsTreePath(t, "partial"),
				0o600,
				&windowsCancelAfterBytesReader{remaining: []byte("partial-payload"), cancel: cancel},
			)
		})
		if err == nil || (!errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled")) {
			t.Fatalf("PrepareRootedTree error = %v, want cancellation", err)
		}
		if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed populate published destination: %v", statErr)
		}
		assertNoWindowsStorageResidue(t, root)
	})
}

type windowsPartialPayloadReader struct {
	remaining []byte
	fail      error
}

func (reader *windowsPartialPayloadReader) Read(buffer []byte) (int, error) {
	if len(reader.remaining) == 0 {
		return 0, reader.fail
	}
	count := copy(buffer, reader.remaining)
	reader.remaining = reader.remaining[count:]
	return count, nil
}

type windowsCancelAfterBytesReader struct {
	remaining []byte
	cancel    context.CancelFunc
}

func (reader *windowsCancelAfterBytesReader) Read(buffer []byte) (int, error) {
	if len(reader.remaining) == 0 {
		reader.cancel()
		return 0, io.EOF
	}
	count := copy(buffer, reader.remaining)
	reader.remaining = reader.remaining[count:]
	if len(reader.remaining) == 0 {
		reader.cancel()
	}
	return count, nil
}

func acquireWindowsTestCommitCapability(t *testing.T, path string) rootedpath.CommitCapability {
	t.Helper()
	root, destination, err := rootedpath.CaptureDestination(path)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := root.Acquire(destination)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	return capability
}

func mustWindowsTreePath(t *testing.T, components ...string) mutationfs.TreeRelativePath {
	t.Helper()
	path, err := mutationfs.NewTreeRelativePath(components...)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
