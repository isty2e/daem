//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestCommitPreparedTreePublishesCompleteTree(t *testing.T) {
	root := canonicalTempDir(t)
	staged := filepath.Join(root, "staged")
	destination := filepath.Join(root, "active")
	if err := os.MkdirAll(filepath.Join(staged, "nested"), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	writeTestFile(t, filepath.Join(staged, "root.txt"), "root", 0o600)
	writeTestFile(t, filepath.Join(staged, "nested", "child.txt"), "child", 0o640)

	request, err := NewPreparedTreeCommit(staged, destination, captureIdentity(t, staged))
	if err != nil {
		t.Fatalf("NewPreparedTreeCommit returned error: %v", err)
	}
	if err := CommitPreparedTree(context.Background(), request); err != nil {
		t.Fatalf("CommitPreparedTree returned error: %v", err)
	}
	if _, err := os.Lstat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged error = %v, want not exist", err)
	}
	assertFile(t, filepath.Join(destination, "root.txt"), "root", 0o600)
	assertFile(t, filepath.Join(destination, "nested", "child.txt"), "child", 0o640)
}

func TestCommitPreparedTreeRejectsExistingDestinationAndSymlink(t *testing.T) {
	t.Run("existing destination", func(t *testing.T) {
		root := canonicalTempDir(t)
		staged := filepath.Join(root, "staged")
		destination := filepath.Join(root, "active")
		if err := os.Mkdir(staged, 0o700); err != nil {
			t.Fatalf("Mkdir staged returned error: %v", err)
		}
		if err := os.Mkdir(destination, 0o700); err != nil {
			t.Fatalf("Mkdir destination returned error: %v", err)
		}
		request, err := NewPreparedTreeCommit(staged, destination, captureIdentity(t, staged))
		if err != nil {
			t.Fatalf("NewPreparedTreeCommit returned error: %v", err)
		}
		assertFailure(t, CommitPreparedTree(context.Background(), request), failureUncommitted, phaseValidate)
	})

	t.Run("symlink child", func(t *testing.T) {
		root := canonicalTempDir(t)
		staged := filepath.Join(root, "staged")
		destination := filepath.Join(root, "active")
		if err := os.Mkdir(staged, 0o700); err != nil {
			t.Fatalf("Mkdir returned error: %v", err)
		}
		if err := os.Symlink("outside", filepath.Join(staged, "link")); err != nil {
			t.Fatalf("Symlink returned error: %v", err)
		}
		request, err := NewPreparedTreeCommit(staged, destination, captureIdentity(t, staged))
		if err != nil {
			t.Fatalf("NewPreparedTreeCommit returned error: %v", err)
		}
		assertFailure(t, CommitPreparedTree(context.Background(), request), failureUnsupportedGuarantee, phaseValidate)
		if _, err := os.Lstat(staged); err != nil {
			t.Fatalf("staged tree error = %v, want retained", err)
		}
	})
}

func TestCommitPreparedTreeRejectsMutationAfterIdentityCapture(t *testing.T) {
	root := canonicalTempDir(t)
	staged := filepath.Join(root, "staged")
	destination := filepath.Join(root, "active")
	if err := os.Mkdir(staged, 0o700); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	expected := captureIdentity(t, staged)
	writeTestFile(t, filepath.Join(staged, "late"), "late", 0o600)
	request, err := NewPreparedTreeCommit(staged, destination, expected)
	if err != nil {
		t.Fatalf("NewPreparedTreeCommit returned error: %v", err)
	}
	assertFailure(t, CommitPreparedTree(context.Background(), request), failureUncommitted, phaseValidate)
	if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination error = %v, want not exist", statErr)
	}
}

func TestCommitPreparedTreeFaultClassification(t *testing.T) {
	tests := []struct {
		phase phase
		kind  mutationfs.FailureKind
		seen  bool
	}{
		{phase: phaseValidate, kind: failureUncommitted},
		{phase: phaseSyncTreeFile, kind: failureUncommitted},
		{phase: phaseSyncTreeDirectory, kind: failureUncommitted},
		{phase: phaseRevalidateEntry, kind: failureUncommitted},
		{phase: phaseCommitEntry, kind: failureUncommitted},
		{phase: phaseVerifyEntry, kind: failureIndeterminateCommit, seen: true},
		{phase: phaseSyncParent, kind: failureIndeterminateCommit, seen: true},
	}
	for _, test := range tests {
		t.Run(string(test.phase), func(t *testing.T) {
			root := canonicalTempDir(t)
			staged := filepath.Join(root, "staged")
			destination := filepath.Join(root, "active")
			if err := os.Mkdir(staged, 0o700); err != nil {
				t.Fatalf("Mkdir returned error: %v", err)
			}
			writeTestFile(t, filepath.Join(staged, "entry"), "payload", 0o600)
			request, err := NewPreparedTreeCommit(staged, destination, captureIdentity(t, staged))
			if err != nil {
				t.Fatalf("NewPreparedTreeCommit returned error: %v", err)
			}
			err = commitPreparedTreeWithFaults(context.Background(), request, faultAt(test.phase))
			assertFailure(t, err, test.kind, test.phase)
			_, destinationErr := os.Lstat(destination)
			if test.seen && destinationErr != nil {
				t.Fatalf("destination error = %v, want visible", destinationErr)
			}
			if !test.seen && !errors.Is(destinationErr, os.ErrNotExist) {
				t.Fatalf("destination error = %v, want not exist", destinationErr)
			}
		})
	}
}

func TestCommitPreparedTreeFaultsAtLaterTreeSyncOccurrences(t *testing.T) {
	tests := []struct {
		phase      phase
		occurrence int
	}{
		{phase: phaseSyncTreeFile, occurrence: 2},
		{phase: phaseSyncTreeDirectory, occurrence: 2},
	}
	for _, test := range tests {
		t.Run(string(test.phase), func(t *testing.T) {
			root := canonicalTempDir(t)
			staged := filepath.Join(root, "staged")
			destination := filepath.Join(root, "active")
			if err := os.MkdirAll(filepath.Join(staged, "nested"), 0o700); err != nil {
				t.Fatalf("MkdirAll returned error: %v", err)
			}
			writeTestFile(t, filepath.Join(staged, "first"), "first", 0o600)
			writeTestFile(t, filepath.Join(staged, "nested", "second"), "second", 0o600)
			request, err := NewPreparedTreeCommit(staged, destination, captureIdentity(t, staged))
			if err != nil {
				t.Fatalf("NewPreparedTreeCommit returned error: %v", err)
			}

			failures := make(map[phase]error)
			count := 0
			faults := faultPlan{
				failures: failures,
				actions: map[phase]func(){
					test.phase: func() {
						count++
						if count == test.occurrence {
							failures[test.phase] = errors.New("injected later tree sync fault")
						}
					},
				},
			}
			err = commitPreparedTreeWithFaults(context.Background(), request, faults)
			assertFailure(t, err, failureUncommitted, test.phase)
			if count != test.occurrence {
				t.Fatalf("sync occurrence count = %d, want %d", count, test.occurrence)
			}
			if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("destination error = %v, want not exist", statErr)
			}
			if _, statErr := os.Lstat(staged); statErr != nil {
				t.Fatalf("staged tree error = %v, want retained", statErr)
			}
		})
	}
}

func TestCommitPreparedTreeClassifiesUnsupportedSyncByVisibility(t *testing.T) {
	tests := []struct {
		phase phase
		kind  mutationfs.FailureKind
		seen  bool
	}{
		{phase: phaseSyncTreeFile, kind: failureUnsupportedGuarantee},
		{phase: phaseSyncParent, kind: failureIndeterminateCommit, seen: true},
	}
	for _, test := range tests {
		t.Run(string(test.phase), func(t *testing.T) {
			root := canonicalTempDir(t)
			staged := filepath.Join(root, "staged")
			destination := filepath.Join(root, "active")
			if err := os.Mkdir(staged, 0o700); err != nil {
				t.Fatalf("Mkdir returned error: %v", err)
			}
			writeTestFile(t, filepath.Join(staged, "entry"), "payload", 0o600)
			request, err := NewPreparedTreeCommit(staged, destination, captureIdentity(t, staged))
			if err != nil {
				t.Fatalf("NewPreparedTreeCommit returned error: %v", err)
			}
			faults := faultPlan{failures: map[phase]error{
				test.phase: unsupported("injected unsupported sync", nil),
			}}
			err = commitPreparedTreeWithFaults(context.Background(), request, faults)
			assertFailure(t, err, test.kind, test.phase)
			_, statErr := os.Lstat(destination)
			if test.seen && statErr != nil {
				t.Fatalf("destination error = %v, want visible", statErr)
			}
			if !test.seen && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("destination error = %v, want not exist", statErr)
			}
		})
	}
}

func TestCommitPreparedTreeAncestorSwapDoesNotPublishToAttacker(t *testing.T) {
	root := canonicalTempDir(t)
	activeParent := filepath.Join(root, "owned")
	movedParent := filepath.Join(root, "moved")
	attackerParent := filepath.Join(root, "attacker")
	if err := os.Mkdir(activeParent, 0o700); err != nil {
		t.Fatalf("Mkdir active parent returned error: %v", err)
	}
	if err := os.Mkdir(attackerParent, 0o700); err != nil {
		t.Fatalf("Mkdir attacker parent returned error: %v", err)
	}
	staged := filepath.Join(activeParent, "staged")
	destination := filepath.Join(activeParent, "active")
	if err := os.Mkdir(staged, 0o700); err != nil {
		t.Fatalf("Mkdir staged returned error: %v", err)
	}
	writeTestFile(t, filepath.Join(staged, "payload"), "managed", 0o600)
	attackerDestination := filepath.Join(attackerParent, "active")
	if err := os.Mkdir(attackerDestination, 0o700); err != nil {
		t.Fatalf("Mkdir attacker destination returned error: %v", err)
	}
	writeTestFile(t, filepath.Join(attackerDestination, "payload"), "attacker", 0o600)
	request, err := NewPreparedTreeCommit(staged, destination, captureIdentity(t, staged))
	if err != nil {
		t.Fatalf("NewPreparedTreeCommit returned error: %v", err)
	}
	var actionErr error
	faults := faultPlan{actions: map[phase]func(){
		phaseCommitEntry: func() {
			if err := os.Rename(activeParent, movedParent); err != nil {
				actionErr = err
				return
			}
			actionErr = os.Symlink(attackerParent, activeParent)
		},
	}}
	err = commitPreparedTreeWithFaults(context.Background(), request, faults)
	if actionErr != nil {
		t.Fatalf("ancestor swap returned error: %v", actionErr)
	}
	assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
	assertFile(t, filepath.Join(attackerDestination, "payload"), "attacker", 0o600)
	assertFile(t, filepath.Join(movedParent, "active", "payload"), "managed", 0o600)
}
