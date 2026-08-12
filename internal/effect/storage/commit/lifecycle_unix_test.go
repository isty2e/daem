//go:build darwin || linux

package commit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

type cleanupWorkRecordingBudget struct {
	pathLimit  int
	entryLimit int
	byteLimit  int64
	paths      int
	entries    int
	bytes      int64
}

func (budget *cleanupWorkRecordingBudget) AdmitPathComponents(count int) error {
	if count < 0 || count > budget.pathLimit-budget.paths {
		return fmt.Errorf("path capacity exceeded")
	}
	budget.paths += count
	return nil
}

func (budget *cleanupWorkRecordingBudget) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	if pathComponents < 0 || pathComponents > budget.pathLimit-budget.paths ||
		entries < 0 || bytes < 0 || entries > budget.entryLimit-budget.entries ||
		bytes > budget.byteLimit-budget.bytes {
		return fmt.Errorf("physical capacity exceeded")
	}
	budget.paths += pathComponents
	budget.entries += entries
	budget.bytes += bytes
	return nil
}

func TestRootedEntryRenamePublishesExactSiblingAndOutcome(t *testing.T) {
	root := canonicalTempDir(t)
	source := filepath.Join(root, "active")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatalf("create source: %v", err)
	}
	writeTestFile(t, filepath.Join(source, "journal.json"), "journal", 0o600)

	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "active")
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatalf("capture source identity: %v", err)
	}
	request, err := NewRootedEntryRename(capability, ".retained", expected)
	if err != nil {
		t.Fatalf("NewRootedEntryRename: %v", err)
	}
	outcome, err := CommitRootedEntryRename(t.Context(), request)
	if err != nil {
		t.Fatalf("CommitRootedEntryRename: %v", err)
	}
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeComplete)
	assertClosedRootedCapability(t, capability)
	if _, err := os.Lstat(source); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("source Lstat error = %v, want not exist", err)
	}
	assertFile(t, filepath.Join(root, ".retained", "journal.json"), "journal", 0o600)
}

func TestRootedEntryRenameAdmitsExactReservedSourceWithoutWeakeningUnrootedAPI(
	t *testing.T,
) {
	root := canonicalTempDir(t)
	sourceName := ".daem-tombstone-" + strings.Repeat("a", 32)
	source := filepath.Join(root, sourceName)
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatalf("create reserved source: %v", err)
	}
	writeTestFile(t, filepath.Join(source, "journal.json"), "journal", 0o600)

	if _, err := CaptureEntryIdentity(t.Context(), source); err == nil {
		t.Fatal("unrooted identity capture admitted reserved source")
	}

	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, sourceName)
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatalf("capture exact reserved source identity: %v", err)
	}
	request, err := NewRootedEntryRename(capability, ".retained", expected)
	if err != nil {
		t.Fatalf("NewRootedEntryRename: %v", err)
	}
	outcome, err := CommitRootedEntryRename(t.Context(), request)
	if err != nil {
		t.Fatalf("CommitRootedEntryRename: %v", err)
	}
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeComplete)
	assertClosedRootedCapability(t, capability)
	if _, err := os.Lstat(source); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("reserved source Lstat error = %v, want not exist", err)
	}
	assertFile(t, filepath.Join(root, ".retained", "journal.json"), "journal", 0o600)
}

func TestRootedEntryRequestsRejectInvalidNamesAndIdentities(t *testing.T) {
	root := canonicalTempDir(t)
	source := filepath.Join(root, "active")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatalf("create source: %v", err)
	}
	other := filepath.Join(root, "other")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatalf("create other: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	expected := captureIdentity(t, source)

	for _, destinationName := range []string{"", ".", "..", "nested/name", "active"} {
		t.Run("destination "+destinationName, func(t *testing.T) {
			capability := rootedCapabilityForCommitTest(t, captured, "active")
			if _, err := NewRootedEntryRename(
				capability,
				destinationName,
				expected,
			); err == nil {
				t.Fatal("NewRootedEntryRename succeeded")
			}
			if err := capability.Validate(); err != nil {
				t.Fatalf("constructor consumed rejected capability: %v", err)
			}
			if err := capability.Close(); err != nil {
				t.Fatalf("close rejected capability: %v", err)
			}
		})
	}

	stale := captureIdentity(t, other)
	for _, construct := range []struct {
		name string
		run  func(rootedpath.CommitCapability) error
	}{
		{
			name: "rename stale identity",
			run: func(capability rootedpath.CommitCapability) error {
				_, err := NewRootedEntryRename(capability, ".retained", stale)
				return err
			},
		},
		{
			name: "cleanup stale identity",
			run: func(capability rootedpath.CommitCapability) error {
				_, err := NewRootedEntryCleanup(capability, stale, defaultTreeTraversalLimits())
				return err
			},
		},
	} {
		t.Run(construct.name, func(t *testing.T) {
			capability := rootedCapabilityForCommitTest(t, captured, "active")
			if err := construct.run(capability); err == nil {
				t.Fatal("request constructor succeeded")
			}
			if err := capability.Validate(); err != nil {
				t.Fatalf("constructor consumed rejected capability: %v", err)
			}
			if err := capability.Close(); err != nil {
				t.Fatalf("close rejected capability: %v", err)
			}
		})
	}
}

func TestRootedEntryRenameFaultsPreserveRestartClassifiableState(t *testing.T) {
	tests := []struct {
		phase         phase
		state         mutationfs.CommitOutcomeState
		sourcePresent bool
		retainedNames []string
		failureKind   mutationfs.FailureKind
	}{
		{phaseValidate, mutationfs.CommitOutcomeUncommitted, true, nil, failureUncommitted},
		{phaseRevalidateEntry, mutationfs.CommitOutcomeUncommitted, true, nil, failureUncommitted},
		{phaseCommitEntry, mutationfs.CommitOutcomeUncommitted, true, nil, failureUncommitted},
		{phaseVerifyEntry, mutationfs.CommitOutcomeIndeterminate, false, []string{".retained"}, failureIndeterminateCommit},
		{phaseSyncParent, mutationfs.CommitOutcomeIndeterminate, false, []string{".retained"}, failureIndeterminateCommit},
	}
	for _, test := range tests {
		t.Run(string(test.phase), func(t *testing.T) {
			root := canonicalTempDir(t)
			source := filepath.Join(root, "active")
			if err := os.Mkdir(source, 0o700); err != nil {
				t.Fatalf("create source: %v", err)
			}
			captured := captureRootForCommitTest(t, root)
			capability := rootedCapabilityForCommitTest(t, captured, "active")
			expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
			if err != nil {
				t.Fatalf("capture source identity: %v", err)
			}
			request, err := NewRootedEntryRename(capability, ".retained", expected)
			if err != nil {
				t.Fatalf("NewRootedEntryRename: %v", err)
			}
			outcome, err := commitRootedEntryRenameWithFaults(
				t.Context(),
				request,
				faultAt(test.phase),
			)
			assertFailure(t, err, test.failureKind, test.phase)
			assertCommitOutcome(t, outcome, test.state, test.retainedNames...)
			assertClosedRootedCapability(t, capability)
			_, sourceErr := os.Lstat(source)
			if test.sourcePresent && sourceErr != nil {
				t.Fatalf("source error = %v, want present", sourceErr)
			}
			if !test.sourcePresent && !errors.Is(sourceErr, fs.ErrNotExist) {
				t.Fatalf("source error = %v, want absent", sourceErr)
			}
			_, destinationErr := os.Lstat(filepath.Join(root, ".retained"))
			if test.sourcePresent && !errors.Is(destinationErr, fs.ErrNotExist) {
				t.Fatalf("destination error = %v, want absent", destinationErr)
			}
			if !test.sourcePresent && destinationErr != nil {
				t.Fatalf("destination error = %v, want present", destinationErr)
			}
		})
	}
}

func TestRootedEntryRenameRejectsDestinationAndSourceDrift(t *testing.T) {
	t.Run("destination exists", func(t *testing.T) {
		root := canonicalTempDir(t)
		source := filepath.Join(root, "active")
		if err := os.Mkdir(source, 0o700); err != nil {
			t.Fatalf("create source: %v", err)
		}
		if err := os.Mkdir(filepath.Join(root, ".retained"), 0o700); err != nil {
			t.Fatalf("create destination: %v", err)
		}
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, "active")
		expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
		if err != nil {
			t.Fatalf("capture source identity: %v", err)
		}
		request, err := NewRootedEntryRename(capability, ".retained", expected)
		if err != nil {
			t.Fatalf("NewRootedEntryRename: %v", err)
		}
		outcome, err := CommitRootedEntryRename(t.Context(), request)
		assertFailure(t, err, failureUncommitted, phaseValidate)
		assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
		if _, err := os.Stat(source); err != nil {
			t.Fatalf("source changed: %v", err)
		}
	})

	t.Run("source mode changed", func(t *testing.T) {
		root := canonicalTempDir(t)
		source := filepath.Join(root, "active")
		if err := os.Mkdir(source, 0o700); err != nil {
			t.Fatalf("create source: %v", err)
		}
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, "active")
		expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
		if err != nil {
			t.Fatalf("capture source identity: %v", err)
		}
		if err := os.Chmod(source, 0o755); err != nil {
			t.Fatalf("change source mode: %v", err)
		}
		request, err := NewRootedEntryRename(capability, ".retained", expected)
		if err != nil {
			t.Fatalf("NewRootedEntryRename: %v", err)
		}
		outcome, err := CommitRootedEntryRename(t.Context(), request)
		assertFailure(t, err, failureUncommitted, phaseRevalidateEntry)
		assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
		if _, err := os.Stat(source); err != nil {
			t.Fatalf("drifted source was removed: %v", err)
		}
	})
}

func TestRootedEntryRenameAncestorSwapNeverMutatesReplacementParent(t *testing.T) {
	parent := canonicalTempDir(t)
	root := filepath.Join(parent, "recovery")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatalf("create outside: %v", err)
	}
	source := filepath.Join(root, "active")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatalf("create source: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "active")
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatalf("capture source identity: %v", err)
	}
	request, err := NewRootedEntryRename(capability, ".retained", expected)
	if err != nil {
		t.Fatalf("NewRootedEntryRename: %v", err)
	}
	moved := filepath.Join(outside, "moved-recovery")
	var actionErr error
	faults := faultPlan{actions: map[phase]func(){
		phaseCommitEntry: func() {
			if err := os.Rename(root, moved); err != nil {
				actionErr = err
				return
			}
			actionErr = os.Mkdir(root, 0o700)
		},
	}}
	outcome, err := commitRootedEntryRenameWithFaults(t.Context(), request, faults)
	if actionErr != nil {
		t.Fatalf("swap ancestor: %v", actionErr)
	}
	assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeIndeterminate, ".retained")
	if _, err := os.Lstat(filepath.Join(root, ".retained")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("replacement parent received destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moved, ".retained")); err != nil {
		t.Fatalf("anchored destination missing: %v", err)
	}
}

func TestRootedEntryRenameClassifiesBetweenCheckRaces(t *testing.T) {
	t.Run("source replaced before rename", func(t *testing.T) {
		root := canonicalTempDir(t)
		source := filepath.Join(root, "active")
		if err := os.Mkdir(source, 0o700); err != nil {
			t.Fatalf("create source: %v", err)
		}
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, "active")
		expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
		if err != nil {
			t.Fatalf("capture source identity: %v", err)
		}
		request, err := NewRootedEntryRename(capability, ".retained", expected)
		if err != nil {
			t.Fatalf("NewRootedEntryRename: %v", err)
		}
		var actionErr error
		faults := faultPlan{actions: map[phase]func(){
			phaseCommitEntry: func() {
				if err := os.Remove(source); err != nil {
					actionErr = err
					return
				}
				if err := os.Mkdir(source, 0o700); err != nil {
					actionErr = err
					return
				}
				actionErr = os.WriteFile(filepath.Join(source, "replacement"), []byte("keep"), 0o600)
			},
		}}
		outcome, err := commitRootedEntryRenameWithFaults(t.Context(), request, faults)
		if actionErr != nil {
			t.Fatalf("replace source: %v", actionErr)
		}
		assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
		assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeIndeterminate, ".retained")
		assertFile(t, filepath.Join(root, ".retained", "replacement"), "keep", 0o600)
	})

	t.Run("source reappears after rename", func(t *testing.T) {
		root := canonicalTempDir(t)
		source := filepath.Join(root, "active")
		if err := os.Mkdir(source, 0o700); err != nil {
			t.Fatalf("create source: %v", err)
		}
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, "active")
		expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
		if err != nil {
			t.Fatalf("capture source identity: %v", err)
		}
		request, err := NewRootedEntryRename(capability, ".retained", expected)
		if err != nil {
			t.Fatalf("NewRootedEntryRename: %v", err)
		}
		var actionErr error
		faults := faultPlan{actions: map[phase]func(){
			phaseVerifyEntry: func() {
				actionErr = os.Mkdir(source, 0o700)
			},
		}}
		outcome, err := commitRootedEntryRenameWithFaults(t.Context(), request, faults)
		if actionErr != nil {
			t.Fatalf("recreate source: %v", actionErr)
		}
		assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
		assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeIndeterminate, ".retained")
		if _, err := os.Stat(source); err != nil {
			t.Fatalf("reappeared source changed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, ".retained")); err != nil {
			t.Fatalf("renamed expected entry changed: %v", err)
		}
	})

	t.Run("opaque unix name survives outcome projection", func(t *testing.T) {
		root := canonicalTempDir(t)
		source := filepath.Join(root, "active")
		if err := os.Mkdir(source, 0o700); err != nil {
			t.Fatalf("create source: %v", err)
		}
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, "active")
		expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
		if err != nil {
			t.Fatalf("capture source identity: %v", err)
		}
		const destinationName = `opaque\name`
		request, err := NewRootedEntryRename(capability, destinationName, expected)
		if err != nil {
			t.Fatalf("NewRootedEntryRename: %v", err)
		}
		outcome, err := commitRootedEntryRenameWithFaults(
			t.Context(),
			request,
			faultAt(phaseSyncParent),
		)
		assertFailure(t, err, failureIndeterminateCommit, phaseSyncParent)
		assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeIndeterminate, destinationName)
		if _, err := os.Stat(filepath.Join(root, destinationName)); err != nil {
			t.Fatalf("opaque destination changed: %v", err)
		}
	})
}

func TestRootedEntryCleanupIsRetryableAfterPartialCancellation(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatalf("create residue: %v", err)
	}
	for _, name := range []string{"a", "b", "c"} {
		writeTestFile(t, filepath.Join(residue, name), name, 0o600)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".retained")
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatalf("capture residue identity: %v", err)
	}
	request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
	if err != nil {
		t.Fatalf("NewRootedEntryCleanup: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	faults := faultPlan{actions: map[phase]func(){
		phaseCleanupEntry: cancel,
	}}
	outcome, err := commitRootedEntryCleanupWithFaults(ctx, request, faults)
	assertFailure(t, err, failureRetainedResidue, phaseCleanupEntry)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeRetainedRecoverable, ".retained")
	entries, readErr := os.ReadDir(residue)
	if readErr != nil {
		t.Fatalf("read partial residue: %v", readErr)
	}
	if len(entries) == 0 || len(entries) == 3 {
		t.Fatalf("partial residue contains %d entries, want strict subset", len(entries))
	}

	retryCapability := rootedCapabilityForCommitTest(t, captured, ".retained")
	retryExpected, err := CaptureRootedEntryIdentity(t.Context(), retryCapability)
	if err != nil {
		t.Fatalf("capture partial residue identity: %v", err)
	}
	retry, err := NewRootedEntryCleanup(retryCapability, retryExpected, defaultTreeTraversalLimits())
	if err != nil {
		t.Fatalf("NewRootedEntryCleanup(retry): %v", err)
	}
	retryOutcome, err := CommitRootedEntryCleanup(t.Context(), retry)
	if err != nil {
		t.Fatalf("CommitRootedEntryCleanup(retry): %v", err)
	}
	assertCommitOutcome(t, retryOutcome, mutationfs.CommitOutcomeComplete)
	if _, err := os.Lstat(residue); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("residue after retry = %v, want absent", err)
	}
}

func TestRootedEntryCleanupPollsCancellationAfterEveryDirectoryRead(t *testing.T) {
	for _, cancelAfterRead := range []int{1, 2, 3, 4, 5} {
		t.Run(fmt.Sprintf("read_%d", cancelAfterRead), func(t *testing.T) {
			root := canonicalTempDir(t)
			residue := filepath.Join(root, ".retained")
			if err := os.Mkdir(residue, 0o700); err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(residue, "victim")
			writeTestFile(t, victim, "planned", 0o600)

			captured := captureRootForCommitTest(t, root)
			capability := rootedCapabilityForCommitTest(t, captured, ".retained")
			expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
			if err != nil {
				t.Fatal(err)
			}
			limits, err := mutationfs.NewTreeTraversalLimits(1, 0, int64(len("planned")))
			if err != nil {
				t.Fatal(err)
			}
			request, err := NewRootedEntryCleanup(capability, expected, limits)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(t.Context())
			reads := 0
			childSteps := 0
			childStepsAtCancel := -1
			outcome, err := commitRootedEntryCleanupWithFaults(ctx, request, faultPlan{
				afterCleanupDirectoryRead: func() {
					reads++
					if reads == cancelAfterRead {
						childStepsAtCancel = childSteps
						cancel()
					}
				},
				beforeCleanupChild: func() { childSteps++ },
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cleanup error = %v, want context cancellation", err)
			}
			assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
			if childStepsAtCancel < 0 || childSteps != childStepsAtCancel {
				t.Fatalf(
					"child filesystem steps after cancellation = %d, steps at cancel = %d",
					childSteps,
					childStepsAtCancel,
				)
			}
			assertFile(t, victim, "planned", 0o600)
		})
	}
}

func TestRootedEntryCleanupConsumesStorageOwnedWorkEnvelopeBeforeEffects(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, "scope", ".retained")
	inner := filepath.Join(residue, "inner")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(residue, "root-file"), "root", 0o600)
	writeTestFile(t, filepath.Join(inner, "nested-file"), "nested", 0o600)
	if err := os.Chmod(inner, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(residue, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(residue, 0o700)
		_ = os.Chmod(inner, 0o700)
	})

	const entries = 3
	const bytes = int64(len("root") + len("nested"))
	limits, err := mutationfs.NewTreeTraversalLimits(entries, 1, bytes)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := mutationfs.NewRootedCleanupWorkEnvelope(
		mutationfs.EntryKindDirectory,
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	parentValidationWork := 2*absolutePathDepthForTest(root) + 1
	cleanupPathWork, err := envelope.PathWork(parentValidationWork)
	if err != nil {
		t.Fatal(err)
	}
	budget := &cleanupWorkRecordingBudget{
		pathLimit:  cleanupPathWork + absolutePathDepthForTest(residue),
		entryLimit: envelope.EntryWork(),
		byteLimit:  envelope.ByteWork(),
	}
	captured := captureRootForCommitTest(t, root)
	capability, entryAuthority := boundedRootedCapabilityForCommitTest(
		t,
		captured,
		"scope/.retained",
		budget,
	)
	defer entryAuthority.Close()
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatal(err)
	}
	budget.paths = 0
	request, err := NewRootedEntryCleanup(capability, expected, limits)
	if err != nil {
		t.Fatal(err)
	}
	childSteps := 0
	outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
		beforeCleanupChild: func() { childSteps++ },
	})
	if err != nil {
		t.Fatalf("CommitRootedEntryCleanup: %v", err)
	}
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeComplete)
	if childSteps != 7*entries {
		t.Fatalf("recursive child steps = %d, want %d", childSteps, 7*entries)
	}
	if budget.entries != envelope.EntryWork() || budget.bytes != envelope.ByteWork() {
		t.Fatalf(
			"storage work = entries:%d bytes:%d, want entries:%d bytes:%d",
			budget.entries,
			budget.bytes,
			envelope.EntryWork(),
			envelope.ByteWork(),
		)
	}
	destinationWork := absolutePathDepthForTest(residue)
	maximumPathWork := cleanupPathWork + destinationWork
	if budget.paths <= destinationWork || budget.paths > maximumPathWork {
		t.Fatalf(
			"path work = %d, want destination access plus namespace work within %d..%d",
			budget.paths,
			destinationWork+1,
			maximumPathWork,
		)
	}
}

func TestRootedEntryCleanupNamespaceEnvelopeCoversRestrictiveDirectoryChain(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, "scope", ".retained")
	first := filepath.Join(residue, "first")
	second := filepath.Join(first, "second")
	third := filepath.Join(second, "third")
	if err := os.MkdirAll(third, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{third, second, first, residue} {
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, path := range []string{residue, first, second, third} {
			_ = os.Chmod(path, 0o700)
		}
	})

	const entries = 3
	limits, err := mutationfs.NewTreeTraversalLimits(entries, entries, 0)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := mutationfs.NewRootedCleanupWorkEnvelope(
		mutationfs.EntryKindDirectory,
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantNamespaceWork := 14*entries + 18
	namespaceWork, err := envelope.PathWork(1)
	if err != nil {
		t.Fatal(err)
	}
	if namespaceWork != wantNamespaceWork {
		t.Fatalf(
			"namespace envelope = %d, want independently derived %d",
			namespaceWork,
			wantNamespaceWork,
		)
	}
	parentValidationWork := 2*absolutePathDepthForTest(root) + 1
	cleanupPathWork, err := envelope.PathWork(parentValidationWork)
	if err != nil {
		t.Fatal(err)
	}
	budget := &cleanupWorkRecordingBudget{
		pathLimit:  cleanupPathWork + absolutePathDepthForTest(residue),
		entryLimit: envelope.EntryWork(),
		byteLimit:  envelope.ByteWork(),
	}
	captured := captureRootForCommitTest(t, root)
	capability, entryAuthority := boundedRootedCapabilityForCommitTest(
		t,
		captured,
		"scope/.retained",
		budget,
	)
	defer entryAuthority.Close()
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatal(err)
	}
	budget.paths = 0
	request, err := NewRootedEntryCleanup(capability, expected, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CommitRootedEntryCleanup(t.Context(), request); err != nil {
		t.Fatalf("CommitRootedEntryCleanup: %v", err)
	}
	// OpenRootDirectory consumes the absolute destination once; production
	// reserves that access separately from cleanup's retained parent-chain
	// envelope.
	wantPathWork := cleanupPathWork + absolutePathDepthForTest(residue)
	if budget.paths != wantPathWork {
		t.Fatalf(
			"path work = %d, want destination access plus exact namespace envelope %d",
			budget.paths,
			wantPathWork,
		)
	}
}

func absolutePathDepthForTest(path string) int {
	trimmed := strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator))
	return strings.Count(trimmed, string(filepath.Separator)) + 1
}

func TestRootedEntryCleanupRejectsInsufficientAggregateWorkBeforeModeRepair(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	if err := os.Mkdir(residue, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(residue, 0o700) })
	limits, err := mutationfs.NewTreeTraversalLimits(0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := mutationfs.NewRootedCleanupWorkEnvelope(
		mutationfs.EntryKindDirectory,
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	budget := &cleanupWorkRecordingBudget{
		pathLimit:  1 << 20,
		entryLimit: envelope.EntryWork() - 1,
		byteLimit:  envelope.ByteWork(),
	}
	captured := captureRootForCommitTest(t, root)
	capability, entryAuthority := boundedRootedCapabilityForCommitTest(
		t,
		captured,
		".retained",
		budget,
	)
	defer entryAuthority.Close()
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRootedEntryCleanup(capability, expected, limits)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := CommitRootedEntryCleanup(t.Context(), request)
	assertFailure(t, err, failureUncommitted, phaseValidate)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
	info, statErr := os.Stat(residue)
	if statErr != nil || info.Mode().Perm() != 0 {
		t.Fatalf("residue mode = %v, err=%v, want 0000", info, statErr)
	}
}

func TestRootedEntryCleanupRejectsInsufficientPathWorkBeforeModeRepair(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	if err := os.Mkdir(residue, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(residue, 0o700) })
	limits, err := mutationfs.NewTreeTraversalLimits(0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := mutationfs.NewRootedCleanupWorkEnvelope(
		mutationfs.EntryKindDirectory,
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	parentValidationWork := 2 * absolutePathDepthForTest(root)
	pathWork, err := envelope.PathWork(parentValidationWork)
	if err != nil {
		t.Fatal(err)
	}
	budget := &cleanupWorkRecordingBudget{
		pathLimit:  pathWork - 1,
		entryLimit: envelope.EntryWork(),
		byteLimit:  envelope.ByteWork(),
	}
	captured := captureRootForCommitTest(t, root)
	capability, entryAuthority := boundedRootedCapabilityForCommitTest(
		t,
		captured,
		".retained",
		budget,
	)
	defer entryAuthority.Close()
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatal(err)
	}
	budget.paths = 0
	request, err := NewRootedEntryCleanup(capability, expected, limits)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := CommitRootedEntryCleanup(t.Context(), request)
	assertFailure(t, err, failureUncommitted, phaseValidate)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
	info, statErr := os.Stat(residue)
	if statErr != nil || info.Mode().Perm() != 0 {
		t.Fatalf("residue mode = %v, err=%v, want 0000", info, statErr)
	}
}

func TestJournaledLogicalRemovalMovesValidatedResidueToRetryableCleanupStage(t *testing.T) {
	root := canonicalTempDir(t)
	active := filepath.Join(root, "active")
	if err := os.Mkdir(active, 0o700); err != nil {
		t.Fatalf("create active directory: %v", err)
	}
	for _, name := range []string{"a", "b", "c"} {
		writeTestFile(t, filepath.Join(active, name), name, 0o600)
	}
	names, err := mutationfs.NewLogicalRemovalNames(
		".daem-tombstone-0123456789abcdef0123456789abcdef",
		".daem-cleanup-0123456789abcdef0123456789abcdef",
	)
	if err != nil {
		t.Fatalf("construct logical removal names: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "active")
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatalf("capture active identity: %v", err)
	}
	request, err := NewRootedLogicalRemovalWithResidue(
		capability, expected, names, defaultTreeTraversalLimits(),
	)
	if err != nil {
		t.Fatalf("NewRootedLogicalRemovalWithResidue: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	faults := faultPlan{actions: map[phase]func(){phaseCleanupEntry: cancel}}
	outcome, err := func() (mutationfs.CommitOutcome, error) {
		failure := commitLogicalRemovalWithFaults(ctx, request, faults)
		return outcomeFromError(failure), failure
	}()
	assertFailure(t, err, failureRetainedResidue, phaseCleanupTombstone)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeRetainedRecoverable, names.Cleanup())
	if _, err := os.Lstat(active); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("active entry after partial cleanup = %v, want absent", err)
	}
	if _, err := os.Lstat(filepath.Join(root, names.Residue())); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("pre-cleanup residue after promotion = %v, want absent", err)
	}
	cleanupPath := filepath.Join(root, names.Cleanup())
	entries, err := os.ReadDir(cleanupPath)
	if err != nil {
		t.Fatalf("read partial cleanup stage: %v", err)
	}
	if len(entries) == 0 || len(entries) == 3 {
		t.Fatalf("partial cleanup stage contains %d entries, want strict subset", len(entries))
	}

	retryCapability := rootedCapabilityForCommitTest(t, captured, names.Cleanup())
	retryExpected, err := CaptureRootedEntryIdentity(t.Context(), retryCapability)
	if err != nil {
		t.Fatalf("capture partial cleanup identity: %v", err)
	}
	retry, err := NewRootedRemovalStageCleanup(
		retryCapability, retryExpected, names, defaultTreeTraversalLimits(),
	)
	if err != nil {
		t.Fatalf("NewRootedRemovalStageCleanup(retry): %v", err)
	}
	if _, err := CommitRootedEntryCleanup(t.Context(), retry); err != nil {
		t.Fatalf("CommitRootedEntryCleanup(retry): %v", err)
	}
	if _, err := os.Lstat(cleanupPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cleanup stage after retry = %v, want absent", err)
	}
}

func TestJournaledLogicalRemovalRetainsDirectoryThatExceedsExactForwardLimit(t *testing.T) {
	root := canonicalTempDir(t)
	active := filepath.Join(root, "active")
	if err := os.Mkdir(active, 0o700); err != nil {
		t.Fatalf("create active directory: %v", err)
	}
	writeTestFile(t, filepath.Join(active, "appeared"), "", 0o600)
	names, err := mutationfs.NewLogicalRemovalNames(
		".daem-tombstone-0123456789abcdef0123456789abcdef",
		".daem-cleanup-0123456789abcdef0123456789abcdef",
	)
	if err != nil {
		t.Fatalf("construct logical removal names: %v", err)
	}
	limits, err := mutationfs.NewTreeTraversalLimits(0, 0, 0)
	if err != nil {
		t.Fatalf("construct exact empty traversal limit: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "active")
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatalf("capture active identity: %v", err)
	}
	request, err := NewRootedLogicalRemovalWithResidue(capability, expected, names, limits)
	if err != nil {
		t.Fatalf("construct bounded logical removal: %v", err)
	}
	outcome, err := CommitLogicalRemovalWithOutcome(t.Context(), request)
	assertFailure(t, err, failureRetainedResidue, phaseCleanupTombstone)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeRetainedRecoverable, names.Cleanup())
	retained := filepath.Join(root, names.Cleanup(), "appeared")
	if _, statErr := os.Stat(retained); statErr != nil {
		t.Fatalf("entry beyond exact forward limit was deleted: %v", statErr)
	}
}

func TestJournaledLogicalRemovalRetainsFileThatExceedsExactForwardLimit(t *testing.T) {
	root := canonicalTempDir(t)
	active := filepath.Join(root, "active")
	writeTestFile(t, active, "appeared", 0o600)
	names, err := mutationfs.NewLogicalRemovalNames(
		".daem-tombstone-1123456789abcdef0123456789abcdef",
		".daem-cleanup-1123456789abcdef0123456789abcdef",
	)
	if err != nil {
		t.Fatalf("construct logical removal names: %v", err)
	}
	limits, err := mutationfs.NewTreeTraversalLimits(0, 0, 0)
	if err != nil {
		t.Fatalf("construct exact empty traversal limit: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "active")
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatalf("capture active identity: %v", err)
	}
	request, err := NewRootedLogicalRemovalWithResidue(capability, expected, names, limits)
	if err != nil {
		t.Fatalf("construct bounded logical removal: %v", err)
	}
	outcome, err := CommitLogicalRemovalWithOutcome(t.Context(), request)
	assertFailure(t, err, failureRetainedResidue, phaseCleanupTombstone)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeRetainedRecoverable, names.Cleanup())
	retained := filepath.Join(root, names.Cleanup())
	content, readErr := os.ReadFile(retained)
	if readErr != nil {
		t.Fatalf("read retained file beyond exact limit: %v", readErr)
	}
	if string(content) != "appeared" {
		t.Fatalf("retained file content = %q, want appeared", content)
	}
}

func TestJournaledLogicalRemovalRejectsEitherOccupiedPrivateSlotBeforeVisibility(t *testing.T) {
	for _, occupiedRole := range []string{"residue", "cleanup"} {
		t.Run(occupiedRole, func(t *testing.T) {
			root := canonicalTempDir(t)
			active := filepath.Join(root, "active")
			writeTestFile(t, active, "managed", 0o600)
			names, err := mutationfs.NewLogicalRemovalNames(
				".daem-tombstone-fedcba9876543210fedcba9876543210",
				".daem-cleanup-fedcba9876543210fedcba9876543210",
			)
			if err != nil {
				t.Fatalf("construct logical removal names: %v", err)
			}
			occupiedName := names.Residue()
			if occupiedRole == "cleanup" {
				occupiedName = names.Cleanup()
			}
			writeTestFile(t, filepath.Join(root, occupiedName), "foreign", 0o600)

			captured := captureRootForCommitTest(t, root)
			capability := rootedCapabilityForCommitTest(t, captured, "active")
			expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
			if err != nil {
				t.Fatalf("capture active identity: %v", err)
			}
			request, err := NewRootedLogicalRemovalWithResidue(
				capability, expected, names, defaultTreeTraversalLimits(),
			)
			if err != nil {
				t.Fatalf("NewRootedLogicalRemovalWithResidue: %v", err)
			}
			err = CommitLogicalRemoval(t.Context(), request)
			assertFailure(t, err, failureUncommitted, phaseCommitTombstone)
			content, err := os.ReadFile(active)
			if err != nil || string(content) != "managed" {
				t.Fatalf("active entry changed before collision rejection: content=%q err=%v", content, err)
			}
			content, err = os.ReadFile(filepath.Join(root, occupiedName))
			if err != nil || string(content) != "foreign" {
				t.Fatalf("occupied private slot changed: content=%q err=%v", content, err)
			}
		})
	}
}

func TestRootedEntryCleanupRemovesExactRegularFile(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	writeTestFile(t, residue, "residue", 0o600)
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".retained")
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatalf("capture residue identity: %v", err)
	}
	request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
	if err != nil {
		t.Fatalf("NewRootedEntryCleanup: %v", err)
	}
	outcome, err := CommitRootedEntryCleanup(t.Context(), request)
	if err != nil {
		t.Fatalf("CommitRootedEntryCleanup: %v", err)
	}
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeComplete)
	if _, err := os.Lstat(residue); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("residue after cleanup = %v, want absent", err)
	}
}

func TestRootedEntryCleanupPreflightsSpecialChildrenBeforeDeletingSiblings(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	preserved := filepath.Join(residue, "a-preserved")
	writeTestFile(t, preserved, "keep", 0o600)
	special := filepath.Join(residue, "z-special")
	if err := unix.Mkfifo(special, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(residue, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(residue, 0o700) })

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
	outcome, err := CommitRootedEntryCleanup(t.Context(), request)
	assertFailure(t, err, failureUnsupportedGuarantee, phaseCleanupEntry)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
	assertFile(t, preserved, "keep", 0o600)
	if info, statErr := os.Lstat(special); statErr != nil || info.Mode()&fs.ModeNamedPipe == 0 {
		t.Fatalf("special child changed: info=%v err=%v", info, statErr)
	}
	if info, statErr := os.Stat(residue); statErr != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("preflight changed root mode: info=%v err=%v", info, statErr)
	}
}

func TestRootedEntryCleanupClassifiesCaptureTimeModeRepairAsMutation(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	special := filepath.Join(residue, "special")
	if err := unix.Mkfifo(special, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(residue, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(residue, 0o700) })

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
	outcome, err := CommitRootedEntryCleanup(t.Context(), request)
	assertFailure(t, err, failureRetainedResidue, phaseCleanupEntry)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeRetainedRecoverable, ".retained")
	if info, statErr := os.Stat(residue); statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("repaired cleanup root mode = %v, err=%v, want 0700", info, statErr)
	}
	if info, statErr := os.Lstat(special); statErr != nil || info.Mode()&fs.ModeNamedPipe == 0 {
		t.Fatalf("special child changed: info=%v err=%v", info, statErr)
	}
}

func TestRootedEntryCleanupRejectsDescendantReplacementFromSnapshot(t *testing.T) {
	tests := []struct {
		name        string
		create      func(*testing.T, string)
		replace     func(*testing.T, string)
		assertAlive func(*testing.T, string)
	}{
		{
			name: "regular file",
			create: func(t *testing.T, path string) {
				writeTestFile(t, path, "planned", 0o600)
			},
			replace: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, path, "replacement", 0o640)
			},
			assertAlive: func(t *testing.T, path string) {
				assertFile(t, path, "replacement", 0o640)
			},
		},
		{
			name: "directory",
			create: func(t *testing.T, path string) {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
			replace: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, filepath.Join(path, "foreign"), "replacement", 0o600)
			},
			assertAlive: func(t *testing.T, path string) {
				assertFile(t, filepath.Join(path, "foreign"), "replacement", 0o600)
			},
		},
		{
			name: "symlink",
			create: func(t *testing.T, path string) {
				if err := os.Symlink("planned", path); err != nil {
					t.Fatal(err)
				}
			},
			replace: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("replacement", path); err != nil {
					t.Fatal(err)
				}
			},
			assertAlive: func(t *testing.T, path string) {
				target, err := os.Readlink(path)
				if err != nil || target != "replacement" {
					t.Fatalf("replacement symlink = %q, %v", target, err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			residue := filepath.Join(root, ".retained")
			if err := os.Mkdir(residue, 0o700); err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(residue, "victim")
			test.create(t, victim)
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
			var replace sync.Once
			outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
				actions: map[phase]func(){
					phaseCleanupEntry: func() {
						replace.Do(func() { test.replace(t, victim) })
					},
				},
			})
			assertFailure(t, err, failureRetainedResidue, phaseCleanupEntry)
			assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeRetainedRecoverable, ".retained")
			test.assertAlive(t, victim)
		})
	}
}

func TestRootedEntryCleanupRejectsAncestorRelocationBeforeDescendantUnlink(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	moved := filepath.Join(root, ".moved")
	victim := filepath.Join(residue, "victim")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, victim, "planned", 0o600)
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
	var relocate sync.Once
	outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
		actions: map[phase]func(){
			phaseCleanupEntry: func() {
				relocate.Do(func() {
					if renameErr := os.Rename(residue, moved); renameErr != nil {
						t.Fatal(renameErr)
					}
					if mkdirErr := os.Mkdir(residue, 0o700); mkdirErr != nil {
						t.Fatal(mkdirErr)
					}
					writeTestFile(t, filepath.Join(residue, "foreign"), "replacement", 0o600)
				})
			},
		},
	})
	assertFailure(t, err, failureIndeterminateCommit, phaseCleanupEntry)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeIndeterminate, ".retained")
	assertFile(t, filepath.Join(moved, "victim"), "planned", 0o600)
	assertFile(t, filepath.Join(residue, "foreign"), "replacement", 0o600)
}

func TestRootedEntryCleanupRejectsDestinationParentRelocationBeforeDescendantUnlink(t *testing.T) {
	root := canonicalTempDir(t)
	destinationParent := filepath.Join(root, "scope")
	residue := filepath.Join(destinationParent, ".retained")
	movedParent := filepath.Join(root, "moved-scope")
	victim := filepath.Join(residue, "victim")
	if err := os.MkdirAll(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, victim, "planned", 0o600)
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "scope/.retained")
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
	if err != nil {
		t.Fatal(err)
	}
	var relocate sync.Once
	outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
		actions: map[phase]func(){
			phaseCleanupEntry: func() {
				relocate.Do(func() {
					if renameErr := os.Rename(destinationParent, movedParent); renameErr != nil {
						t.Fatal(renameErr)
					}
					if mkdirErr := os.Mkdir(destinationParent, 0o700); mkdirErr != nil {
						t.Fatal(mkdirErr)
					}
					writeTestFile(t, filepath.Join(destinationParent, "foreign"), "replacement", 0o600)
				})
			},
		},
	})
	assertFailure(t, err, failureUncommitted, phaseCleanupEntry)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
	assertFile(t, filepath.Join(movedParent, ".retained", "victim"), "planned", 0o600)
	assertFile(t, filepath.Join(destinationParent, "foreign"), "replacement", 0o600)
}

func TestRootedEntryCleanupRejectsDestinationParentRelocationBeforeModeRepair(t *testing.T) {
	root := canonicalTempDir(t)
	destinationParent := filepath.Join(root, "scope")
	residue := filepath.Join(destinationParent, ".retained")
	movedParent := filepath.Join(root, "moved-scope")
	if err := os.MkdirAll(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(residue, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(residue, 0o700)
		_ = os.Chmod(filepath.Join(movedParent, ".retained"), 0o700)
	})
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "scope/.retained")
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
	if err != nil {
		t.Fatal(err)
	}
	var relocate sync.Once
	outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
		actions: map[phase]func(){
			phaseApplyMode: func() {
				relocate.Do(func() {
					if renameErr := os.Rename(destinationParent, movedParent); renameErr != nil {
						t.Fatal(renameErr)
					}
					if mkdirErr := os.Mkdir(destinationParent, 0o700); mkdirErr != nil {
						t.Fatal(mkdirErr)
					}
				})
			},
		},
	})
	assertFailure(t, err, failureUncommitted, phaseApplyMode)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
	if info, statErr := os.Stat(filepath.Join(movedParent, ".retained")); statErr != nil || info.Mode().Perm() != 0 {
		t.Fatalf("moved cleanup root mode = %v, err=%v, want 0000", info, statErr)
	}
}

func TestRootedEntryCleanupRejectsMiddleAncestorRelocationBeforeDescendantUnlink(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	outer := filepath.Join(residue, "outer")
	middle := filepath.Join(outer, "middle")
	moved := filepath.Join(outer, "moved")
	inner := filepath.Join(middle, "inner")
	victim := filepath.Join(inner, "victim")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, victim, "planned", 0o600)
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
	var relocate sync.Once
	outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
		actions: map[phase]func(){
			phaseCleanupEntry: func() {
				relocate.Do(func() {
					if renameErr := os.Rename(middle, moved); renameErr != nil {
						t.Fatal(renameErr)
					}
					if mkdirErr := os.Mkdir(middle, 0o700); mkdirErr != nil {
						t.Fatal(mkdirErr)
					}
					writeTestFile(t, filepath.Join(middle, "foreign"), "replacement", 0o600)
				})
			},
		},
	})
	assertFailure(t, err, failureUncommitted, phaseCleanupEntry)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
	assertFile(t, filepath.Join(moved, "inner", "victim"), "planned", 0o600)
	assertFile(t, filepath.Join(middle, "foreign"), "replacement", 0o600)
}

func TestRootedEntryCleanupRejectsNamespaceAndContentDriftBeforeUnlink(t *testing.T) {
	tests := []struct {
		name        string
		drift       func(*testing.T, string)
		assert      func(*testing.T, string)
		failureKind mutationfs.FailureKind
		outcomeKind mutationfs.CommitOutcomeState
	}{
		{
			name: "new sibling",
			drift: func(t *testing.T, residue string) {
				writeTestFile(t, filepath.Join(residue, "foreign"), "external", 0o600)
			},
			assert: func(t *testing.T, residue string) {
				assertFile(t, filepath.Join(residue, "foreign"), "external", 0o600)
			},
			failureKind: failureRetainedResidue,
			outcomeKind: mutationfs.CommitOutcomeRetainedRecoverable,
		},
		{
			name: "in-place content",
			drift: func(t *testing.T, residue string) {
				writeTestFile(t, filepath.Join(residue, "victim"), "changed", 0o600)
			},
			assert: func(t *testing.T, residue string) {
				assertFile(t, filepath.Join(residue, "victim"), "changed", 0o600)
			},
			failureKind: failureUncommitted,
			outcomeKind: mutationfs.CommitOutcomeUncommitted,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			residue := filepath.Join(root, ".retained")
			if err := os.Mkdir(residue, 0o700); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(residue, "victim"), "planned", 0o600)
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
			var drift sync.Once
			outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
				actions: map[phase]func(){
					phaseCleanupEntry: func() {
						drift.Do(func() { test.drift(t, residue) })
					},
				},
			})
			assertFailure(t, err, test.failureKind, phaseCleanupEntry)
			if test.outcomeKind == mutationfs.CommitOutcomeUncommitted {
				assertCommitOutcome(t, outcome, test.outcomeKind)
			} else {
				assertCommitOutcome(t, outcome, test.outcomeKind, ".retained")
			}
			test.assert(t, residue)
		})
	}
}

func TestRootedEntryCleanupSealsDeepContentBeforeFirstUnlink(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	victim := filepath.Join(residue, "nested", "inner", "victim")
	if err := os.MkdirAll(filepath.Dir(victim), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(residue, "first"), "first", 0o600)
	writeTestFile(t, victim, "planned", 0o600)
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
	outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
		actions: map[phase]func(){
			phaseRevalidateCleanup: func() {
				writeTestFile(t, victim, "changed", 0o600)
			},
		},
	})
	assertFailure(t, err, failureUncommitted, phaseRevalidateCleanup)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
	assertFile(t, filepath.Join(residue, "first"), "first", 0o600)
	assertFile(t, victim, "changed", 0o600)
}

func TestRootedEntryCleanupDoesNotChmodReplacementDirectory(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	victim := filepath.Join(residue, "victim")
	moved := filepath.Join(residue, "moved")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(victim, 0o500); err != nil {
		t.Fatal(err)
	}
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
	var replace sync.Once
	outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
		actions: map[phase]func(){
			phaseApplyMode: func() {
				replace.Do(func() {
					if renameErr := os.Rename(victim, moved); renameErr != nil {
						t.Fatal(renameErr)
					}
					if mkdirErr := os.Mkdir(victim, 0o500); mkdirErr != nil {
						t.Fatal(mkdirErr)
					}
				})
			},
		},
	})
	assertFailure(t, err, failureRetainedResidue, phaseApplyMode)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeRetainedRecoverable, ".retained")
	if info, statErr := os.Stat(victim); statErr != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("replacement mode = %v, err=%v, want 0500", info, statErr)
	}
}

func TestRootedEntryCleanupDoesNotChmodRestrictiveReplacementDirectory(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	moved := filepath.Join(root, "moved")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(residue, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(residue, 0o700)
		_ = os.Chmod(moved, 0o700)
	})
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
	var replace sync.Once
	outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
		actions: map[phase]func(){
			phaseApplyMode: func() {
				replace.Do(func() {
					if renameErr := os.Rename(residue, moved); renameErr != nil {
						t.Fatal(renameErr)
					}
					if mkdirErr := os.Mkdir(residue, 0o700); mkdirErr != nil {
						t.Fatal(mkdirErr)
					}
					if chmodErr := os.Chmod(residue, 0); chmodErr != nil {
						t.Fatal(chmodErr)
					}
				})
			},
		},
	})
	assertFailure(t, err, failureIndeterminateCommit, phaseApplyMode)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeIndeterminate, ".retained")
	for _, path := range []string{residue, moved} {
		if info, statErr := os.Stat(path); statErr != nil || info.Mode().Perm() != 0 {
			t.Fatalf("directory %q mode = %v, err=%v, want 0000", path, info, statErr)
		}
	}
}

func TestRootedEntryCleanupClassifiesOpenedDirectoryModeRepairAsMutation(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	victim := filepath.Join(residue, "victim")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(victim, 0o500); err != nil {
		t.Fatal(err)
	}
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
	outcome, err := commitRootedEntryCleanupWithFaults(
		t.Context(),
		request,
		faultAt(phaseCleanupEntry),
	)
	assertFailure(t, err, failureRetainedResidue, phaseCleanupEntry)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeRetainedRecoverable, ".retained")
	if info, statErr := os.Stat(victim); statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("planned directory mode = %v, err=%v, want 0700", info, statErr)
	}
}

func TestRootedRemovalStageCleanupHonorsOperationTraversalLimit(t *testing.T) {
	root := canonicalTempDir(t)
	cleanupPath := filepath.Join(root, ".daem-cleanup-0123456789abcdef0123456789abcdef")
	if err := os.Mkdir(cleanupPath, 0o700); err != nil {
		t.Fatalf("create cleanup stage: %v", err)
	}
	for _, name := range []string{"first", "second"} {
		if err := os.WriteFile(filepath.Join(cleanupPath, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write cleanup child: %v", err)
		}
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, filepath.Base(cleanupPath))
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatalf("capture cleanup stage identity: %v", err)
	}
	names, err := mutationfs.NewLogicalRemovalNames(
		".daem-tombstone-0123456789abcdef0123456789abcdef",
		filepath.Base(cleanupPath),
	)
	if err != nil {
		t.Fatalf("construct removal names: %v", err)
	}
	limits, err := mutationfs.NewTreeTraversalLimits(1, 64, 64)
	if err != nil {
		t.Fatalf("construct traversal limits: %v", err)
	}
	request, err := NewRootedRemovalStageCleanup(capability, expected, names, limits)
	if err != nil {
		t.Fatalf("construct cleanup request: %v", err)
	}
	if _, err := CommitRootedEntryCleanup(t.Context(), request); err == nil {
		t.Fatal("cleanup accepted a tree larger than its operation reservation")
	}
	if _, err := os.Lstat(cleanupPath); err != nil {
		t.Fatalf("cleanup stage changed after bounded preflight failure: %v", err)
	}
}

func TestRootedRemovalStageCleanupHonorsByteLimitDuringPreflight(t *testing.T) {
	root := canonicalTempDir(t)
	cleanupPath := filepath.Join(root, ".daem-cleanup-1123456789abcdef0123456789abcdef")
	if err := os.Mkdir(cleanupPath, 0o700); err != nil {
		t.Fatalf("create cleanup stage: %v", err)
	}
	writeTestFile(t, filepath.Join(cleanupPath, "payload"), "too-large", 0o600)

	request := rootedRemovalStageCleanupForTest(t, root, cleanupPath, 4, 8)
	if _, err := CommitRootedEntryCleanup(t.Context(), request); err == nil {
		t.Fatal("cleanup accepted a tree larger than its byte reservation")
	}
	assertFile(t, filepath.Join(cleanupPath, "payload"), "too-large", 0o600)
}

func TestRootedRemovalStageCleanupHonorsByteLimitDuringDeletion(t *testing.T) {
	root := canonicalTempDir(t)
	cleanupPath := filepath.Join(root, ".daem-cleanup-2123456789abcdef0123456789abcdef")
	if err := os.Mkdir(cleanupPath, 0o700); err != nil {
		t.Fatalf("create cleanup stage: %v", err)
	}
	first := filepath.Join(cleanupPath, "a-first")
	second := filepath.Join(cleanupPath, "z-second")
	writeTestFile(t, first, "", 0o600)
	writeTestFile(t, second, "", 0o600)

	request := rootedRemovalStageCleanupForTest(t, root, cleanupPath, 4, 1)
	var growSecond sync.Once
	outcome, err := commitRootedEntryCleanupWithFaults(t.Context(), request, faultPlan{
		actions: map[phase]func(){
			phaseCleanupEntry: func() {
				growSecond.Do(func() {
					if writeErr := os.WriteFile(second, []byte("expanded"), 0o600); writeErr != nil {
						t.Errorf("grow second cleanup entry: %v", writeErr)
					}
				})
			},
		},
	})
	if err == nil {
		t.Fatal("cleanup accepted deletion-pass byte growth beyond its reservation")
	}
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeRetainedRecoverable, filepath.Base(cleanupPath))
	assertFile(t, second, "expanded", 0o600)
}

func TestRootedRemovalStageRegularFileHonorsByteLimit(t *testing.T) {
	for _, test := range []struct {
		name         string
		content      string
		maximumBytes int64
		wantRemoved  bool
	}{
		{name: "zero byte", content: "", maximumBytes: 0, wantRemoved: true},
		{name: "exact limit", content: "exact", maximumBytes: 5, wantRemoved: true},
		{name: "over limit", content: "beyond", maximumBytes: 5, wantRemoved: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			cleanupPath := filepath.Join(root, ".daem-cleanup-3123456789abcdef0123456789abcdef")
			writeTestFile(t, cleanupPath, test.content, 0o600)

			request := rootedRemovalStageCleanupForTest(t, root, cleanupPath, 0, test.maximumBytes)
			_, err := CommitRootedEntryCleanup(t.Context(), request)
			if test.wantRemoved {
				if err != nil {
					t.Fatalf("cleanup regular root: %v", err)
				}
				if _, statErr := os.Lstat(cleanupPath); !os.IsNotExist(statErr) {
					t.Fatalf("regular cleanup root remains: %v", statErr)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "regular-file bytes") {
				t.Fatalf("over-limit regular cleanup error = %v", err)
			}
			assertFile(t, cleanupPath, test.content, 0o600)
		})
	}
}

func TestRootedEntryCleanupRemovesRestrictiveEntries(t *testing.T) {
	for _, test := range []struct {
		name   string
		create func(*testing.T, string)
	}{
		{
			name: "regular root",
			create: func(t *testing.T, path string) {
				writeTestFile(t, path, "payload", 0o600)
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "directory tree",
			create: func(t *testing.T, path string) {
				nested := filepath.Join(path, "nested")
				if err := os.MkdirAll(nested, 0o700); err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(nested, "payload")
				writeTestFile(t, file, "payload", 0o600)
				for _, entry := range []string{file, nested, path} {
					if err := os.Chmod(entry, 0); err != nil {
						t.Fatal(err)
					}
				}
				t.Cleanup(func() {
					_ = os.Chmod(path, 0o700)
					_ = os.Chmod(nested, 0o700)
				})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			residue := filepath.Join(root, ".retained")
			test.create(t, residue)
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
			outcome, err := CommitRootedEntryCleanup(t.Context(), request)
			if err != nil {
				t.Fatalf("cleanup restrictive entry: %v", err)
			}
			assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeComplete)
			if _, err := os.Lstat(residue); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("restrictive cleanup root remains: %v", err)
			}
		})
	}
}

func rootedRemovalStageCleanupForTest(
	t *testing.T,
	root string,
	cleanupPath string,
	maximumEntries int,
	maximumBytes int64,
) RootedEntryCleanup {
	t.Helper()
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, filepath.Base(cleanupPath))
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatalf("capture cleanup stage identity: %v", err)
	}
	names, err := mutationfs.NewLogicalRemovalNames(
		strings.Replace(filepath.Base(cleanupPath), ".daem-cleanup-", ".daem-tombstone-", 1),
		filepath.Base(cleanupPath),
	)
	if err != nil {
		t.Fatalf("construct removal names: %v", err)
	}
	limits, err := mutationfs.NewTreeTraversalLimits(maximumEntries, 64, maximumBytes)
	if err != nil {
		t.Fatalf("construct traversal limits: %v", err)
	}
	request, err := NewRootedRemovalStageCleanup(capability, expected, names, limits)
	if err != nil {
		t.Fatalf("construct cleanup request: %v", err)
	}
	return request
}

func TestRootedEntryCleanupFaultClassification(t *testing.T) {
	tests := []struct {
		name          string
		fault         phase
		state         mutationfs.CommitOutcomeState
		failureKind   mutationfs.FailureKind
		residueExists bool
		retainedNames []string
	}{
		{
			name:          "pre-effect snapshot seal",
			fault:         phaseRevalidateCleanup,
			state:         mutationfs.CommitOutcomeUncommitted,
			failureKind:   failureUncommitted,
			residueExists: true,
		},
		{
			name:          "before child removal",
			fault:         phaseCleanupEntry,
			state:         mutationfs.CommitOutcomeUncommitted,
			failureKind:   failureUncommitted,
			residueExists: true,
		},
		{
			name:          "absence sync",
			fault:         phaseSyncCleanupParent,
			state:         mutationfs.CommitOutcomeIndeterminate,
			failureKind:   failureIndeterminateCommit,
			retainedNames: []string{".retained"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			residue := filepath.Join(root, ".retained")
			if err := os.Mkdir(residue, 0o700); err != nil {
				t.Fatalf("create residue: %v", err)
			}
			writeTestFile(t, filepath.Join(residue, "entry"), "payload", 0o600)
			captured := captureRootForCommitTest(t, root)
			capability := rootedCapabilityForCommitTest(t, captured, ".retained")
			expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
			if err != nil {
				t.Fatalf("capture residue identity: %v", err)
			}
			request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
			if err != nil {
				t.Fatalf("NewRootedEntryCleanup: %v", err)
			}
			outcome, err := commitRootedEntryCleanupWithFaults(
				t.Context(),
				request,
				faultAt(test.fault),
			)
			assertFailure(t, err, test.failureKind, test.fault)
			assertCommitOutcome(t, outcome, test.state, test.retainedNames...)
			_, statErr := os.Lstat(residue)
			if test.residueExists && statErr != nil {
				t.Fatalf("residue error = %v, want present", statErr)
			}
			if !test.residueExists && !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("residue error = %v, want absent", statErr)
			}
		})
	}
}

func TestRootedEntryCleanupRejectsReplacementSymlinkAndSpecialEntry(t *testing.T) {
	t.Run("replacement", func(t *testing.T) {
		root := canonicalTempDir(t)
		residue := filepath.Join(root, ".retained")
		if err := os.Mkdir(residue, 0o700); err != nil {
			t.Fatalf("create residue: %v", err)
		}
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, ".retained")
		expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
		if err != nil {
			t.Fatalf("capture residue identity: %v", err)
		}
		if err := os.Remove(residue); err != nil {
			t.Fatalf("remove original residue: %v", err)
		}
		if err := os.Mkdir(residue, 0o700); err != nil {
			t.Fatalf("create replacement residue: %v", err)
		}
		writeTestFile(t, filepath.Join(residue, "keep"), "replacement", 0o600)
		request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
		if err != nil {
			t.Fatalf("NewRootedEntryCleanup: %v", err)
		}
		outcome, err := CommitRootedEntryCleanup(t.Context(), request)
		assertFailure(t, err, failureUncommitted, phaseValidate)
		assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
		assertFile(t, filepath.Join(residue, "keep"), "replacement", 0o600)
	})

	t.Run("symlink", func(t *testing.T) {
		root := canonicalTempDir(t)
		outside := filepath.Join(root, "outside")
		if err := os.Mkdir(outside, 0o700); err != nil {
			t.Fatalf("create outside: %v", err)
		}
		writeTestFile(t, filepath.Join(outside, "keep"), "outside", 0o600)
		residue := filepath.Join(root, ".retained")
		if err := os.Symlink(outside, residue); err != nil {
			t.Fatalf("create residue symlink: %v", err)
		}
		expected := captureIdentity(t, residue)
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, ".retained")
		if _, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits()); err == nil {
			t.Fatal("NewRootedEntryCleanup accepted symlink")
		}
		if err := capability.Close(); err != nil {
			t.Fatalf("close rejected capability: %v", err)
		}
		assertFile(t, filepath.Join(outside, "keep"), "outside", 0o600)
	})

	t.Run("special", func(t *testing.T) {
		root := canonicalTempDir(t)
		path := filepath.Join(root, ".retained")
		if err := unix.Mkfifo(path, 0o600); err != nil {
			t.Fatalf("create FIFO: %v", err)
		}
		var stat unix.Stat_t
		if err := unix.Lstat(path, &stat); err != nil {
			t.Fatalf("lstat FIFO: %v", err)
		}
		expected := identityFromStat(path, &stat)
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, ".retained")
		if _, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits()); err == nil {
			t.Fatal("NewRootedEntryCleanup accepted special entry")
		}
		if err := capability.Close(); err != nil {
			t.Fatalf("close rejected capability: %v", err)
		}
		if info, err := os.Lstat(path); err != nil || info.Mode()&fs.ModeNamedPipe == 0 {
			t.Fatalf("special entry changed: info=%v err=%v", info, err)
		}
	})

	t.Run("symlink substitution", func(t *testing.T) {
		root := canonicalTempDir(t)
		residue := filepath.Join(root, ".retained")
		if err := os.Mkdir(residue, 0o700); err != nil {
			t.Fatalf("create residue: %v", err)
		}
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, ".retained")
		expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
		if err != nil {
			t.Fatalf("capture residue identity: %v", err)
		}
		outside := filepath.Join(root, "outside")
		if err := os.Mkdir(outside, 0o700); err != nil {
			t.Fatalf("create outside: %v", err)
		}
		writeTestFile(t, filepath.Join(outside, "keep"), "outside", 0o600)
		if err := os.Remove(residue); err != nil {
			t.Fatalf("remove original residue: %v", err)
		}
		if err := os.Symlink(outside, residue); err != nil {
			t.Fatalf("substitute residue symlink: %v", err)
		}
		request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
		if err != nil {
			t.Fatalf("NewRootedEntryCleanup: %v", err)
		}
		outcome, err := CommitRootedEntryCleanup(t.Context(), request)
		assertFailure(t, err, failureUncommitted, phaseValidate)
		assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
		assertFile(t, filepath.Join(outside, "keep"), "outside", 0o600)
	})

	t.Run("special substitution", func(t *testing.T) {
		root := canonicalTempDir(t)
		residue := filepath.Join(root, ".retained")
		if err := os.Mkdir(residue, 0o700); err != nil {
			t.Fatalf("create residue: %v", err)
		}
		captured := captureRootForCommitTest(t, root)
		capability := rootedCapabilityForCommitTest(t, captured, ".retained")
		expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
		if err != nil {
			t.Fatalf("capture residue identity: %v", err)
		}
		if err := os.Remove(residue); err != nil {
			t.Fatalf("remove original residue: %v", err)
		}
		if err := unix.Mkfifo(residue, 0o600); err != nil {
			t.Fatalf("substitute residue FIFO: %v", err)
		}
		request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
		if err != nil {
			t.Fatalf("NewRootedEntryCleanup: %v", err)
		}
		outcome, err := CommitRootedEntryCleanup(t.Context(), request)
		assertFailure(t, err, failureUnsupportedGuarantee, phaseValidate)
		assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeUncommitted)
		if info, err := os.Lstat(residue); err != nil || info.Mode()&fs.ModeNamedPipe == 0 {
			t.Fatalf("substituted special entry changed: info=%v err=%v", info, err)
		}
	})
}

func TestRootedEntryCleanupConcurrentAttemptsNeverDeleteReplacement(t *testing.T) {
	root := canonicalTempDir(t)
	residue := filepath.Join(root, ".retained")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatalf("create residue: %v", err)
	}
	for _, name := range []string{"a", "b", "c"} {
		writeTestFile(t, filepath.Join(residue, name), name, 0o600)
	}
	captured := captureRootForCommitTest(t, root)

	type attempt struct {
		request RootedEntryCleanup
	}
	attempts := make([]attempt, 2)
	for index := range attempts {
		capability := rootedCapabilityForCommitTest(t, captured, ".retained")
		expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
		if err != nil {
			t.Fatalf("capture identity %d: %v", index, err)
		}
		request, err := NewRootedEntryCleanup(capability, expected, defaultTreeTraversalLimits())
		if err != nil {
			t.Fatalf("NewRootedEntryCleanup %d: %v", index, err)
		}
		attempts[index] = attempt{request: request}
	}

	var wait sync.WaitGroup
	errorsByAttempt := make([]error, len(attempts))
	for index := range attempts {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, errorsByAttempt[index] = CommitRootedEntryCleanup(
				context.Background(),
				attempts[index].request,
			)
		}()
	}
	wait.Wait()
	successes := 0
	for _, err := range errorsByAttempt {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful cleanup attempts = %d, want 1; errors=%v", successes, errorsByAttempt)
	}
	if _, err := os.Lstat(residue); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("residue after concurrent cleanup = %v, want absent", err)
	}
}

func TestRootedRecordReplacementReportsTemporaryAndVisibilityOutcomes(t *testing.T) {
	t.Run("temporary residue", func(t *testing.T) {
		root := canonicalTempDir(t)
		record := filepath.Join(root, "retirement.json")
		writeTestFile(t, record, "prepared", 0o600)
		request, err := NewFileReplacement(
			record,
			[]byte("finalizing"),
			0o600,
			captureIdentity(t, record),
		)
		if err != nil {
			t.Fatalf("NewFileReplacement: %v", err)
		}
		faults := faultPlan{failures: map[phase]error{
			phaseWritePayload:     errors.New("injected write failure"),
			phaseCleanupTemporary: errors.New("injected cleanup failure"),
		}}
		err = commitFileWithFaults(t.Context(), request, faults)
		outcome := outcomeFromError(err)
		if outcome.State() != mutationfs.CommitOutcomeRetainedRecoverable {
			t.Fatalf(
				"commit outcome state = %q, want %q",
				outcome.State(),
				mutationfs.CommitOutcomeRetainedRecoverable,
			)
		}
		assertFile(t, record, "prepared", 0o600)
		retained := outcome.RetainedNames()
		if len(retained) != 1 || !directoryHasPrefix(t, root, temporaryPrefix) {
			t.Fatalf("retained names = %v, want one temporary", retained)
		}
	})

	t.Run("post-visibility sync", func(t *testing.T) {
		root := canonicalTempDir(t)
		record := filepath.Join(root, "retirement.json")
		writeTestFile(t, record, "prepared", 0o600)
		request, err := NewFileReplacement(
			record,
			[]byte("finalizing"),
			0o600,
			captureIdentity(t, record),
		)
		if err != nil {
			t.Fatalf("NewFileReplacement: %v", err)
		}
		err = commitFileWithFaults(t.Context(), request, faultAt(phaseSyncParent))
		outcome := outcomeFromError(err)
		assertFailure(t, err, failureIndeterminateCommit, phaseSyncParent)
		assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeIndeterminate)
		assertFile(t, record, "finalizing", 0o600)
	})
}

func TestPreparedRootedTreeCommitWithOutcomePublishesPrivateTree(t *testing.T) {
	root := canonicalTempDir(t)
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "control")
	prepared, err := PrepareRootedTree(
		t.Context(),
		capability,
		func(writer mutationfs.RootedTreeWriter) error {
			if err := writer.SetRootMode(0o700); err != nil {
				return err
			}
			return writer.WriteFile(
				treePathForTest(t, "record"),
				0o600,
				bytes.NewReader([]byte("prepared")),
			)
		},
	)
	if err != nil {
		t.Fatalf("PrepareRootedTree: %v", err)
	}
	outcome, err := prepared.CommitWithOutcome(t.Context())
	if err != nil {
		t.Fatalf("CommitWithOutcome: %v", err)
	}
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeComplete)
	assertFile(t, filepath.Join(root, "control", "record"), "prepared", 0o600)
	info, err := os.Stat(filepath.Join(root, "control"))
	if err != nil {
		t.Fatalf("stat control: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("control mode = %04o, want 0700", info.Mode().Perm())
	}
}

func TestPreparedRootedTreeOutcomeReportsRetainedStage(t *testing.T) {
	root := canonicalTempDir(t)
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "control")
	prepared, err := PrepareRootedTree(
		t.Context(),
		capability,
		func(writer mutationfs.RootedTreeWriter) error {
			if err := writer.SetRootMode(0o700); err != nil {
				return err
			}
			return writer.WriteFile(
				treePathForTest(t, "record"),
				0o600,
				bytes.NewReader([]byte("prepared")),
			)
		},
	)
	if err != nil {
		t.Fatalf("PrepareRootedTree: %v", err)
	}
	stageName := prepared.stageName
	stagePath := prepared.stagePath
	faults := faultPlan{failures: map[phase]error{
		phaseValidate:         errors.New("injected validation failure"),
		phaseCleanupTemporary: errors.New("injected cleanup failure"),
	}}
	err = commitPreparedRootedTreeWithFaults(t.Context(), prepared, faults)
	assertFailure(t, err, failureUncommitted, phaseValidate)
	assertCommitOutcome(
		t,
		outcomeFromError(err),
		mutationfs.CommitOutcomeRetainedRecoverable,
		stageName,
	)
	assertClosedRootedCapability(t, capability)
	if _, err := os.Stat(stagePath); err != nil {
		t.Fatalf("retained stage missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "control")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("control publication = %v, want absent", err)
	}
}

func TestAdapterExposesRootedEntryCommitProtocol(t *testing.T) {
	root := canonicalTempDir(t)
	active := filepath.Join(root, "active")
	if err := os.Mkdir(active, 0o700); err != nil {
		t.Fatalf("create active entry: %v", err)
	}
	record := filepath.Join(root, "record")
	writeTestFile(t, record, "before", 0o600)
	captured := captureRootForCommitTest(t, root)
	adapter := Adapter{}

	renameCapability := rootedCapabilityForCommitTest(t, captured, "active")
	activeIdentity, err := adapter.CaptureRootedEntryIdentity(t.Context(), renameCapability)
	if err != nil {
		t.Fatalf("capture active identity: %v", err)
	}
	renameOutcome, err := adapter.RenameRootedEntry(
		t.Context(),
		renameCapability,
		".retained",
		activeIdentity,
	)
	if err != nil {
		t.Fatalf("RenameRootedEntry: %v", err)
	}
	assertCommitOutcome(t, renameOutcome, mutationfs.CommitOutcomeComplete)

	replaceCapability := rootedCapabilityForCommitTest(t, captured, "record")
	recordIdentity, err := adapter.CaptureRootedEntryIdentity(t.Context(), replaceCapability)
	if err != nil {
		t.Fatalf("capture record identity: %v", err)
	}
	replaceOutcome, err := adapter.ReplaceRootedFileWithOutcome(
		t.Context(),
		replaceCapability,
		[]byte("after"),
		0o600,
		recordIdentity,
	)
	if err != nil {
		t.Fatalf("ReplaceRootedFileWithOutcome: %v", err)
	}
	assertCommitOutcome(t, replaceOutcome, mutationfs.CommitOutcomeComplete)
	assertFile(t, record, "after", 0o600)

	cleanupCapability := rootedCapabilityForCommitTest(t, captured, ".retained")
	residueIdentity, err := adapter.CaptureRootedEntryIdentity(t.Context(), cleanupCapability)
	if err != nil {
		t.Fatalf("capture residue identity: %v", err)
	}
	cleanupOutcome, err := adapter.CleanupRootedEntry(
		t.Context(),
		cleanupCapability,
		residueIdentity,
		defaultTreeTraversalLimits(),
	)
	if err != nil {
		t.Fatalf("CleanupRootedEntry: %v", err)
	}
	assertCommitOutcome(t, cleanupOutcome, mutationfs.CommitOutcomeComplete)
	if _, err := os.Lstat(filepath.Join(root, ".retained")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cleaned residue = %v, want absent", err)
	}
}

func assertCommitOutcome(
	t *testing.T,
	outcome mutationfs.CommitOutcome,
	wantState mutationfs.CommitOutcomeState,
	wantRetained ...string,
) {
	t.Helper()
	if outcome.State() != wantState {
		t.Fatalf("commit outcome state = %q, want %q", outcome.State(), wantState)
	}
	gotRetained := outcome.RetainedNames()
	slices.Sort(gotRetained)
	slices.Sort(wantRetained)
	if !slices.Equal(gotRetained, wantRetained) {
		t.Fatalf("commit outcome retained names = %v, want %v", gotRetained, wantRetained)
	}
	if mutated := outcome.RetainedNames(); len(mutated) != 0 {
		mutated[0] = "mutated"
		if slices.Equal(mutated, outcome.RetainedNames()) {
			t.Fatal("commit outcome retained names alias internal storage")
		}
	}
}
