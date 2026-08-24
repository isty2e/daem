//go:build windows

package commit

import (
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type windowsBackupExactReservation struct {
	budget *recovery.PhysicalWorkBudget
}

func (reservation windowsBackupExactReservation) AdmitPathComponents(count int) error {
	return reservation.budget.ReserveBackupPathComponents(count)
}

type windowsBackupExactSink struct {
	entries int
	bytes   int64
}

func (sink *windowsBackupExactSink) VisitRoot(fs.FileMode) error {
	return nil
}

func (sink *windowsBackupExactSink) VisitDirectory(mutationfs.TreeRelativePath, fs.FileMode) error {
	sink.entries++
	return nil
}

func (sink *windowsBackupExactSink) VisitRegularFile(
	_ mutationfs.TreeRelativePath,
	_ fs.FileMode,
	size int64,
	content io.Reader,
) error {
	written, err := io.Copy(io.Discard, content)
	if err != nil {
		return err
	}
	if written != size {
		return fmt.Errorf("backup sink consumed %d of %d bytes", written, size)
	}
	sink.entries++
	sink.bytes += size
	return nil
}

// TestWindowsRecoveryBackupExactBudgetAdmitsSnapshotAndSettle reproduces the
// recovery backup algebra end to end: exact reservation, capacity transfer,
// bounded acquisition, rooted snapshot, and final settle must all succeed for
// one valid non-empty directory.
func TestWindowsRecoveryBackupExactBudgetAdmitsSnapshotAndSettle(t *testing.T) {
	ctx := t.Context()
	rootPath := t.TempDir()
	backupDirectory := filepath.Join(rootPath, "backup")

	var preparation AncestorCleanup
	if err := preparation.PrepareParent(ctx, filepath.Join(backupDirectory, "placeholder")); err != nil {
		t.Fatalf("prepare canonical backup directory: %v", err)
	}
	preparation.Close()

	payloads := map[string]string{"first.json": "payload", "second.json": "content"}
	totalBytes := int64(0)
	for name, payload := range payloads {
		request, err := NewFileCreate(filepath.Join(backupDirectory, name), []byte(payload), 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := CommitFile(ctx, request); err != nil {
			t.Fatal(err)
		}
		totalBytes += int64(len(payload))
	}

	budget, err := recovery.NewPhysicalWorkBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRootNoFollowBounded(rootPath, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	authority, err := root.AuthorityBounded(budget)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := rootedpath.NewRelativeDestination("backup")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.ReserveDestinationAccess(
		destination,
		recovery.MaximumPhysicalPathDepth,
		windowsBackupExactReservation{budget: budget},
	); err != nil {
		t.Fatal(err)
	}
	work, err := recovery.NewArtifactWork(len(payloads), totalBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupDirectoryExecution(work); err != nil {
		t.Fatal(err)
	}
	execution, err := budget.BeginReservedBackupExecution()
	if err != nil {
		t.Fatal(err)
	}
	capability, err := root.AcquireBounded(destination, recovery.MaximumPhysicalPathDepth, execution)
	if err != nil {
		t.Fatal(err)
	}
	limits, err := mutationfs.NewTreeTraversalLimits(work.Entries(), recovery.MaximumArtifactTreeDepth, work.Bytes())
	if err != nil {
		_ = capability.Close()
		t.Fatal(err)
	}
	sink := &windowsBackupExactSink{}
	_, snapshotErr := SnapshotRootedDirectory(ctx, capability, limits, sink)
	closeErr := capability.Close()
	if snapshotErr != nil || closeErr != nil {
		t.Fatalf("exact-budget rooted snapshot = %v, %v, want success", snapshotErr, closeErr)
	}
	if sink.entries != len(payloads) || sink.bytes != totalBytes {
		t.Fatalf("snapshot work = entries=%d bytes=%d, want entries=%d bytes=%d",
			sink.entries, sink.bytes, len(payloads), totalBytes)
	}
	actual, err := recovery.NewArtifactWork(sink.entries, sink.bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.AdmitTree(actual); err != nil {
		t.Fatalf("exact-budget settle rejected valid backup work: %v", err)
	}
}
