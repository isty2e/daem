//go:build windows

package commit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

type windowsRecordingTreeSink struct {
	contents map[string][]byte
}

func (sink *windowsRecordingTreeSink) VisitRoot(fs.FileMode) error {
	return nil
}

func (sink *windowsRecordingTreeSink) VisitDirectory(mutationfs.TreeRelativePath, fs.FileMode) error {
	return nil
}

func (sink *windowsRecordingTreeSink) VisitRegularFile(
	path mutationfs.TreeRelativePath,
	_ fs.FileMode,
	size int64,
	content io.Reader,
) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return fmt.Errorf("recording sink read %d of %d bytes", len(data), size)
	}
	sink.contents[path.Path()] = data
	return nil
}

type windowsPartialTreeSink struct {
	maximum int64
}

func (sink *windowsPartialTreeSink) VisitRoot(fs.FileMode) error { return nil }

func (sink *windowsPartialTreeSink) VisitDirectory(mutationfs.TreeRelativePath, fs.FileMode) error {
	return nil
}

func (sink *windowsPartialTreeSink) VisitRegularFile(
	_ mutationfs.TreeRelativePath,
	_ fs.FileMode,
	size int64,
	content io.Reader,
) error {
	if sink.maximum <= 0 {
		return nil
	}
	limited := sink.maximum
	if limited > size {
		limited = size
	}
	_, err := io.CopyN(io.Discard, content, limited)
	return err
}

func prepareWindowsStreamingTree(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	destination := filepath.Join(root, "tree")
	payload := strings.Repeat("windows-streaming-payload;", 64)
	prepared, err := PrepareRootedTree(
		t.Context(),
		acquireWindowsTestCommitCapability(t, destination),
		func(writer mutationfs.RootedTreeWriter) error {
			if err := writer.SetRootMode(0o700); err != nil {
				return err
			}
			if err := writer.CreateDirectory(mustWindowsTreePath(t, "nested"), 0o700); err != nil {
				return err
			}
			return writer.WriteFile(mustWindowsTreePath(t, "nested", "entry"), 0o600, strings.NewReader(payload))
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepared.CommitWithPublishedIdentity(t.Context()); err != nil {
		t.Fatal(err)
	}
	return destination, payload
}

func TestWindowsRootedTreeSnapshotStreamsContentToSink(t *testing.T) {
	destination, payload := prepareWindowsStreamingTree(t)
	sink := &windowsRecordingTreeSink{contents: map[string][]byte{}}
	limits, err := mutationfs.NewTreeTraversalLimits(10, 4, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SnapshotRootedDirectory(t.Context(), acquireWindowsTestCommitCapability(t, destination), limits, sink); err != nil {
		t.Fatalf("rooted snapshot = %v, want success", err)
	}
	if len(sink.contents) != 1 {
		t.Fatalf("snapshot visited %d regular files, want 1", len(sink.contents))
	}
	if got := string(sink.contents["nested/entry"]); got != payload {
		t.Fatalf("snapshot content = %q, want published payload", got)
	}
}

func TestWindowsRootedTreeSnapshotRejectsSloppySinks(t *testing.T) {
	destination, _ := prepareWindowsStreamingTree(t)
	limits, err := mutationfs.NewTreeTraversalLimits(10, 4, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SnapshotRootedDirectory(
		t.Context(),
		acquireWindowsTestCommitCapability(t, destination),
		limits,
		&windowsPartialTreeSink{},
	); err == nil || !strings.Contains(err.Error(), "consumed") {
		t.Fatalf("sloppy sink snapshot = %v, want consumed-bytes failure", err)
	}
	if _, err := SnapshotRootedDirectory(
		t.Context(),
		acquireWindowsTestCommitCapability(t, destination),
		limits,
		&windowsPartialTreeSink{maximum: 4},
	); err == nil || !strings.Contains(err.Error(), "consumed") {
		t.Fatalf("partial sink snapshot = %v, want consumed-bytes failure", err)
	}
}

func TestWindowsRootedTreeValidationDoesNotMaterializePayload(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "tree")
	payload := bytes.Repeat([]byte("windows-validation-metadata-only;"), 256*1024) // 8 MiB
	prepared, err := PrepareRootedTree(
		t.Context(),
		acquireWindowsTestCommitCapability(t, destination),
		func(writer mutationfs.RootedTreeWriter) error {
			if err := writer.SetRootMode(0o700); err != nil {
				return err
			}
			return writer.WriteFile(mustWindowsTreePath(t, "payload"), 0o600, bytes.NewReader(payload))
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepared.CommitWithPublishedIdentity(t.Context()); err != nil {
		t.Fatal(err)
	}

	limits, err := mutationfs.NewTreeTraversalLimits(10, 4, 1<<24)
	if err != nil {
		t.Fatal(err)
	}
	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := ValidateRootedDirectoryTree(t.Context(), acquireWindowsTestCommitCapability(t, destination), limits); err != nil {
		t.Fatalf("rooted validation = %v, want success", err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated >= uint64(len(payload)) {
		t.Fatalf("metadata-only validation allocated %d bytes, materializing the %d-byte payload", allocated, len(payload))
	}
}

func TestWindowsRootedCleanupPreCancelledContextDoesNotConsumeBudget(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, ".retained")
	prepared, err := PrepareRootedTree(
		t.Context(),
		acquireWindowsTestCommitCapability(t, destination),
		func(writer mutationfs.RootedTreeWriter) error {
			if err := writer.SetRootMode(0o700); err != nil {
				return err
			}
			return writer.WriteFile(mustWindowsTreePath(t, "child.json"), 0o600, strings.NewReader("payload"))
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepared.CommitWithPublishedIdentity(t.Context()); err != nil {
		t.Fatal(err)
	}

	budget := &windowsAccountingRecordingBudget{}
	capability := acquireWindowsAccountingCapability(t, root, ".retained", budget)
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		_ = capability.Close()
		t.Fatal(err)
	}
	limits, err := mutationfs.NewTreeTraversalLimits(4, 2, 4096)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRootedEntryCleanup(capability, expected, limits)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = CommitRootedEntryCleanup(ctx, request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled cleanup = %v, want context.Canceled", err)
	}
	if budget.pathComponents != 0 || budget.entries != 0 || budget.bytes != 0 {
		t.Fatalf(
			"pre-cancelled cleanup consumed budget components=%d entries=%d bytes=%d, want none",
			budget.pathComponents,
			budget.entries,
			budget.bytes,
		)
	}
}
