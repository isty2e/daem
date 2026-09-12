//go:build linux

package commit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestNFSFlagsAbsenceDoesNotHideInspectionFailures(t *testing.T) {
	for _, filesystem := range []int64{unix.NFS_SUPER_MAGIC, unix.EXT4_SUPER_MAGIC} {
		for _, failure := range []error{nil, unix.ENOTTY, unix.EOPNOTSUPP, unix.EIO, unix.EACCES, unix.EPERM, unix.EBADF, unix.EINVAL} {
			err := failure
			if err != nil {
				err = fmt.Errorf("ioctl: %w", err)
			}
			want := filesystem == unix.NFS_SUPER_MAGIC && (failure == unix.ENOTTY || failure == unix.EOPNOTSUPP)
			if got := linuxNFSFlagsUnavailable(filesystem, err); got != want {
				t.Fatalf("filesystem=%x error=%v: unavailable=%v, want %v", filesystem, err, got, want)
			}
		}
	}
	if _, err := linuxFileFlags(-1); !errors.Is(err, unix.EBADF) {
		t.Fatalf("invalid file flags descriptor error = %v, want EBADF", err)
	}
}

func TestNFSRenameErrorsPreserveCauseAndUncertainty(t *testing.T) {
	if err := nfsRenameResult(nil); err != nil {
		t.Fatal(err)
	}
	for _, cause := range []error{unix.EIO, unix.EEXIST, unix.ENOENT, unix.EACCES} {
		err := nfsRenameResult(cause)
		if !errors.Is(err, cause) || !errors.Is(err, errRenameIndeterminate) {
			t.Fatalf("NFS rename error = %v, want cause and unknown outcome", err)
		}
		classified := failureBeforeVisibility(phaseCommitEntry, "/entry", err)
		assertCommitOutcome(t, outcomeFromError(classified), mutationfs.CommitOutcomeIndeterminate)
	}
}

func TestNFSUncertainFilePublicationRetainsEvidence(t *testing.T) {
	for _, replace := range []bool{false, true} {
		for _, moved := range []bool{false, true} {
			t.Run(fmt.Sprintf("replace=%t/moved=%t", replace, moved), func(t *testing.T) {
				root := canonicalTempDir(t)
				path := filepath.Join(root, "created", "entry")
				request, err := NewFileCreate(path, []byte("new"), 0o600)
				if replace {
					if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					writeTestFile(t, path, "old", 0o600)
					request, err = NewFileReplacement(path, []byte("new"), 0o600, captureIdentity(t, path))
				}
				if err != nil {
					t.Fatal(err)
				}
				faults := faultPlan{}
				if moved {
					faults.afterEffectFailures = map[phase]error{phaseCommitEntry: nfsRenameResult(unix.EIO)}
				} else {
					faults.failures = map[phase]error{phaseCommitEntry: nfsRenameResult(unix.EIO)}
				}
				err = commitFileWithFaults(t.Context(), request, faults)
				failure := assertFailure(t, err, failureIndeterminateCommit, phaseCommitEntry)
				if len(failure.residue) == 0 {
					t.Fatal("uncertain publication omitted candidate residue")
				}
				if outcomeFromError(err).State() != mutationfs.CommitOutcomeIndeterminate {
					t.Fatal("uncertain publication reported a resolved outcome")
				}
				if moved {
					assertFile(t, path, "new", 0o600)
				} else {
					entries, err := os.ReadDir(filepath.Dir(path))
					if err != nil {
						t.Fatal(err)
					}
					want := 1
					if replace {
						want++
						assertFile(t, path, "old", 0o600)
					}
					if len(entries) != want {
						t.Fatalf("retained entries = %v, want %d including the private stage", entries, want)
					}
				}
			})
		}
	}
}

func TestNFSUncertainTreePublicationDoesNotAbortEvidence(t *testing.T) {
	for _, moved := range []bool{false, true} {
		t.Run(fmt.Sprintf("moved=%t", moved), func(t *testing.T) {
			root := canonicalTempDir(t)
			captured := captureRootForCommitTest(t, root)
			capability := rootedCapabilityForCommitTest(t, captured, "created/tree")
			prepared := prepareRootedTreeForTest(t, capability)
			stage := prepared.stagePath
			faults := faultPlan{}
			if moved {
				faults.afterEffectFailures = map[phase]error{phaseCommitEntry: nfsRenameResult(unix.EEXIST)}
			} else {
				faults.failures = map[phase]error{phaseCommitEntry: nfsRenameResult(unix.EIO)}
			}
			err := commitPreparedRootedTreeWithFaults(t.Context(), prepared, faults)
			assertFailure(t, err, failureIndeterminateCommit, phaseCommitEntry)
			assertCommitOutcome(t, outcomeFromError(err), mutationfs.CommitOutcomeIndeterminate, filepath.Base(stage))
			assertClosedRootedCapability(t, capability)
			if err := prepared.Abort(t.Context()); err != nil {
				t.Fatal(err)
			}
			retained := stage
			if moved {
				retained = filepath.Join(root, "created", "tree")
			}
			assertFile(t, filepath.Join(retained, "entry"), "payload", 0o600)
		})
	}
}

func TestNFSUncertainAncestorPublicationIsNotCleaned(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "created", "entry")
	faults := faultPlan{afterEffectFailures: map[phase]error{phasePublishAncestor: nfsRenameResult(unix.EEXIST)}}
	err := prepareCommitParentWithFaults(t.Context(), path, faults, nil)
	failure := assertFailure(t, err, failureIndeterminateCommit, phaseCreateAncestors)
	if len(failure.residue) < 2 {
		t.Fatalf("candidate ancestor residue = %v, want both possible names", failure.residue)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("published ancestor was cleaned: %v", err)
	}
}

func TestNFSUncertainRootedRenameRetainsDestination(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "active")
	writeTestFile(t, path, "payload", 0o600)
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "active")
	expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRootedEntryRename(capability, ".retained", expected)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := commitRootedEntryRenameWithFaults(t.Context(), request, faultPlan{
		afterEffectFailures: map[phase]error{phaseCommitEntry: nfsRenameResult(unix.ENOENT)},
	})
	assertFailure(t, err, failureIndeterminateCommit, phaseCommitEntry)
	assertCommitOutcome(t, outcome, mutationfs.CommitOutcomeIndeterminate, ".retained")
	assertFile(t, filepath.Join(root, ".retained"), "payload", 0o600)
	assertClosedRootedCapability(t, capability)
}

func TestNFSUncertainRemovalRenameRetainsTombstoneOrCleanup(t *testing.T) {
	for _, failedPhase := range []phase{phaseCommitTombstone, phasePromoteCleanup} {
		t.Run(string(failedPhase), func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "active")
			writeTestFile(t, path, "payload", 0o600)
			names, err := mutationfs.NewLogicalRemovalNames(
				".daem-tombstone-1123456789abcdef0123456789abcdef",
				".daem-cleanup-1123456789abcdef0123456789abcdef",
			)
			if err != nil {
				t.Fatal(err)
			}
			captured := captureRootForCommitTest(t, root)
			capability := rootedCapabilityForCommitTest(t, captured, "active")
			expected, err := CaptureRootedEntryIdentity(t.Context(), capability)
			if err != nil {
				t.Fatal(err)
			}
			request, err := NewRootedLogicalRemovalWithResidue(capability, expected, names, defaultTreeTraversalLimits())
			if err != nil {
				t.Fatal(err)
			}
			err = commitLogicalRemovalWithFaults(t.Context(), request, faultPlan{
				afterEffectFailures: map[phase]error{failedPhase: nfsRenameResult(unix.EIO)},
			})
			assertFailure(t, err, failureIndeterminateCommit, failedPhase)
			retained := names.Residue()
			if failedPhase == phasePromoteCleanup {
				retained = names.Cleanup()
				assertCommitOutcome(t, outcomeFromError(err), mutationfs.CommitOutcomeIndeterminate, names.Cleanup(), names.Residue())
			} else {
				assertCommitOutcome(t, outcomeFromError(err), mutationfs.CommitOutcomeIndeterminate, names.Residue())
			}
			assertFile(t, filepath.Join(root, retained), "payload", 0o600)
			assertClosedRootedCapability(t, capability)
		})
	}
}
