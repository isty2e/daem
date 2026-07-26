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

func TestCommitFileDoesNotFollowSymlinkAncestor(t *testing.T) {
	root := canonicalTempDir(t)
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	linkedParent := filepath.Join(root, "linked")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}
	target := filepath.Join(linkedParent, "state.json")
	request, err := NewFileCreate(target, []byte("unsafe"), 0o600)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	assertFailure(t, CommitFile(context.Background(), request), failureUncommitted, phaseCreateAncestors)
	if _, err := os.Lstat(filepath.Join(realParent, "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("real destination error = %v, want not exist", err)
	}
}

func TestCommitFileDetectsAncestorSwapWithoutRedirectingEffect(t *testing.T) {
	tests := []struct {
		name        string
		actionPhase phase
		kind        mutationfs.FailureKind
		failedPhase phase
		movedFile   bool
	}{
		{name: "before visibility", actionPhase: phaseCreateAncestors, kind: failureUncommitted, failedPhase: phaseValidate},
		{
			name:        "after visibility",
			actionPhase: phaseVerifyEntry,
			kind:        failureIndeterminateCommit,
			failedPhase: phaseVerifyEntry,
			movedFile:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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
			target := filepath.Join(activeParent, "state.json")
			attackerTarget := filepath.Join(attackerParent, "state.json")
			writeTestFile(t, attackerTarget, "attacker", 0o600)
			request, err := NewFileCreate(target, []byte("payload"), 0o600)
			if err != nil {
				t.Fatalf("NewFileCreate returned error: %v", err)
			}
			var actionErr error
			faults := faultPlan{actions: map[phase]func(){
				test.actionPhase: func() {
					if err := os.Rename(activeParent, movedParent); err != nil {
						actionErr = err
						return
					}
					actionErr = os.Symlink(attackerParent, activeParent)
				},
			}}
			err = commitFileWithFaults(context.Background(), request, faults)
			if actionErr != nil {
				t.Fatalf("ancestor swap returned error: %v", actionErr)
			}
			assertFailure(t, err, test.kind, test.failedPhase)
			assertFile(t, attackerTarget, "attacker", 0o600)
			_, movedErr := os.Lstat(filepath.Join(movedParent, "state.json"))
			if test.movedFile && movedErr != nil {
				t.Fatalf("moved destination error = %v, want visible indeterminate commit", movedErr)
			}
			if !test.movedFile && !errors.Is(movedErr, os.ErrNotExist) {
				t.Fatalf("moved destination error = %v, want not exist", movedErr)
			}
		})
	}
}

func TestCommitFileCreateDoesNotReplaceConcurrentDestination(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "state.json")
	request, err := NewFileCreate(target, []byte("managed"), 0o600)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	var actionErr error
	faults := faultPlan{actions: map[phase]func(){
		phaseCommitEntry: func() { actionErr = os.WriteFile(target, []byte("external"), 0o640) },
	}}
	err = commitFileWithFaults(context.Background(), request, faults)
	if actionErr != nil {
		t.Fatalf("concurrent destination create returned error: %v", actionErr)
	}
	assertFailure(t, err, failureUncommitted, phaseCommitEntry)
	assertFile(t, target, "external", 0o640)
	assertNoPrivateEntries(t, root)
}

func TestCommitFileReplacementNeverFollowsFinalSymlinkRace(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "state.json")
	referent := filepath.Join(root, "external.json")
	writeTestFile(t, target, "before", 0o600)
	writeTestFile(t, referent, "external", 0o600)
	request, err := NewFileReplacement(target, []byte("managed"), 0o600, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewFileReplacement returned error: %v", err)
	}
	var actionErr error
	faults := faultPlan{actions: map[phase]func(){
		phaseCommitEntry: func() {
			if err := os.Remove(target); err != nil {
				actionErr = err
				return
			}
			actionErr = os.Symlink(referent, target)
		},
	}}
	if err := commitFileWithFaults(context.Background(), request, faults); err != nil {
		t.Fatalf("commitFileWithFaults returned error: %v", err)
	}
	if actionErr != nil {
		t.Fatalf("final entry swap returned error: %v", actionErr)
	}
	assertFile(t, target, "managed", 0o600)
	assertFile(t, referent, "external", 0o600)
}

func TestCommitFileReplacementRejectsChangeBeforeFinalRevalidation(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "state.json")
	writeTestFile(t, target, "before", 0o600)
	request, err := NewFileReplacement(target, []byte("managed"), 0o600, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewFileReplacement returned error: %v", err)
	}
	var actionErr error
	faults := faultPlan{actions: map[phase]func(){
		phaseRevalidateEntry: func() {
			if err := os.Remove(target); err != nil {
				actionErr = err
				return
			}
			actionErr = os.WriteFile(target, []byte("external"), 0o640)
		},
	}}
	err = commitFileWithFaults(context.Background(), request, faults)
	if actionErr != nil {
		t.Fatalf("late replacement returned error: %v", actionErr)
	}
	assertFailure(t, err, failureUncommitted, phaseRevalidateEntry)
	assertFile(t, target, "external", 0o640)
	assertNoPrivateEntries(t, root)
}

func TestCommitFileReplacementRejectsInPlaceChangeAfterIdentityCapture(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "state.json")
	writeTestFile(t, target, "before", 0o600)
	expected := captureIdentity(t, target)
	if err := os.WriteFile(target, []byte("external"), 0o600); err != nil {
		t.Fatalf("external in-place write returned error: %v", err)
	}
	request, err := NewFileReplacement(target, []byte("desired"), 0o600, expected)
	if err != nil {
		t.Fatalf("NewFileReplacement returned error: %v", err)
	}
	assertFailure(t, CommitFile(context.Background(), request), failureUncommitted, phaseValidate)
	assertFile(t, target, "external", 0o600)
}
