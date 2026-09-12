//go:build darwin || linux

package commit

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/sys/unix"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/test/testkit/fsclock"
)

func TestCleanupRejectsChangedChildBeforeEffectsWithUnchangedRoot(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(residue, "entry")
	writeTestFile(t, victim, "preserved", 0o600)
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".retained")
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
	if err != nil {
		t.Fatal(err)
	}
	var before, after unix.Stat_t
	if err := unix.Stat(residue, &before); err != nil {
		t.Fatal(err)
	}
	var mutate sync.Once
	outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
		actions: map[phase]func(){
			phaseCleanupEntry: func() {
				mutate.Do(func() {
					fsclock.WaitForTick(t, victim)
					if err := os.Chmod(victim, 0o640); err != nil {
						t.Fatal(err)
					}
				})
			},
		},
	})
	assertFailure(t, err, failureUncommitted, phaseCleanupEntry)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
	assertFile(t, victim, "preserved", 0o640)
	if err := unix.Stat(residue, &after); err != nil {
		t.Fatal(err)
	}
	if before.Ino != after.Ino || before.Ctim != after.Ctim {
		t.Fatal("child-only chmod changed the cleanup root version")
	}
}
