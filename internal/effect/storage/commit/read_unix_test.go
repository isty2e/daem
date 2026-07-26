//go:build darwin || linux

package commit

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func readRegularFileWithFaults(ctx context.Context, path string, faults faultPlan) ([]byte, os.FileMode, error) {
	content, mode, _, err := readRegularFileSnapshotWithFaults(ctx, path, nil, 0, faults)
	return content, mode, err
}

func TestReadRegularFileReturnsStableContentAndMode(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "state.json")
	writeTestFile(t, path, "content", 0o640)
	content, mode, err := ReadRegularFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadRegularFile returned error: %v", err)
	}
	if string(content) != "content" || mode.Perm() != 0o640 {
		t.Fatalf("snapshot = (%q, %04o), want (content, 0640)", content, mode.Perm())
	}
}

func TestReadRegularFileSnapshotOwnsContentAndMode(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "state.json")
	writeTestFile(t, path, "content", 0o640)

	snapshot, err := ReadRegularFileSnapshot(t.Context(), path)
	if err != nil {
		t.Fatalf("ReadRegularFileSnapshot returned error: %v", err)
	}
	first := snapshot.Content()
	first[0] = 'X'
	if got := string(snapshot.Content()); got != "content" {
		t.Fatalf("snapshot content = %q after caller mutation, want content", got)
	}
	if snapshot.Mode().Perm() != 0o640 {
		t.Fatalf("snapshot mode = %04o, want 0640", snapshot.Mode().Perm())
	}
}

func TestReadRegularFileSnapshotUpToEnforcesPayloadBound(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "bounded.txt")
	writeTestFile(t, path, "12345", 0o600)

	snapshot, err := ReadRegularFileSnapshotUpTo(t.Context(), path, 5)
	if err != nil || string(snapshot.Content()) != "12345" {
		t.Fatalf("ReadRegularFileSnapshotUpTo exact bound = %q, %v", snapshot.Content(), err)
	}
	if _, err := ReadRegularFileSnapshotUpTo(t.Context(), path, 4); err == nil {
		t.Fatal("ReadRegularFileSnapshotUpTo oversized file returned nil error")
	}
	if _, err := ReadRegularFileSnapshotUpTo(t.Context(), path, 0); err == nil {
		t.Fatal("ReadRegularFileSnapshotUpTo zero bound returned nil error")
	}
}

func TestReadRegularFileSnapshotUpToRejectsFileGrowthAfterInitialObservation(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "growing.txt")
	writeTestFile(t, path, "1234", 0o600)
	var growOnce sync.Once
	faults := faultPlan{actions: map[phase]func(){
		phaseReadPayload: func() {
			growOnce.Do(func() {
				file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Errorf("open growing file: %v", err)
					return
				}
				if _, err := file.WriteString("5678"); err != nil {
					t.Errorf("grow file: %v", err)
				}
				if err := file.Close(); err != nil {
					t.Errorf("close growing file: %v", err)
				}
			})
		},
	}}

	_, _, _, err := readRegularFileSnapshotWithFaults(t.Context(), path, nil, 5, faults)
	assertFailure(t, err, failureUncommitted, phaseReadPayload)
}

func TestReadRegularFileRejectsFinalSymlink(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "target")
	writeTestFile(t, target, "secret", 0o600)
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, _, err := ReadRegularFile(context.Background(), link)
	assertFailure(t, err, failureUncommitted, phaseValidate)
}

func TestReadRegularFileDetectsFinalEntryReplacement(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "state.json")
	displaced := filepath.Join(root, "displaced.json")
	writeTestFile(t, path, "before", 0o600)
	var actionErr error
	_, _, err := readRegularFileWithFaults(context.Background(), path, faultPlan{actions: map[phase]func(){
		phaseRevalidateEntry: func() {
			actionErr = os.Rename(path, displaced)
			if actionErr == nil {
				actionErr = os.Symlink(displaced, path)
			}
		},
	}})
	if actionErr != nil {
		t.Fatalf("replace final entry: %v", actionErr)
	}
	assertFailure(t, err, failureUncommitted, phaseRevalidateEntry)
}

func TestReadRegularFileFaultClassification(t *testing.T) {
	for _, failedPhase := range []phase{phaseValidate, phaseReadPayload, phaseRevalidateEntry} {
		t.Run(string(failedPhase), func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "journal.json")
			writeTestFile(t, path, "evidence", 0o600)
			content, _, err := readRegularFileWithFaults(context.Background(), path, faultAt(failedPhase))
			assertFailure(t, err, failureUncommitted, failedPhase)
			if content != nil {
				t.Fatalf("content = %q, want nil after fault", content)
			}
		})
	}
}
