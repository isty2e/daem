package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"golang.org/x/sys/unix"
)

func TestRetireActiveJournalUsesCanonicalProtocolAndRejectsStaleReplay(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, _ = captureInventoryJournal(t, recoveryRoot, "retire-active")
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
	root := captureRetirementTestRoot(t, recoveryRoot)

	if err := retireActiveJournalForTest(t.Context(), plan, root, filesystem); err != nil {
		t.Fatalf("RetireActiveJournal: %v", err)
	}
	wantOperations := []string{
		"publish_control",
		"rename_active",
		"advance_record",
		"cleanup_residue",
		"rename_control",
		"cleanup_gc",
	}
	if !slices.Equal(filesystem.operations, wantOperations) {
		t.Fatalf("operations = %v, want %v", filesystem.operations, wantOperations)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateClean)

	filesystem.operations = nil
	err := retireActiveJournalForTest(t.Context(), plan, root, filesystem)
	if err == nil || !strings.Contains(err.Error(), "active recovery journal") {
		t.Fatalf("stale RetireActiveJournal error = %v, want active-journal refusal", err)
	}
	if len(filesystem.operations) != 0 {
		t.Fatalf("stale replay operations = %v, want none", filesystem.operations)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateClean)
}

func TestPreparedRetirementCapacityMatchesExecutableReadPasses(t *testing.T) {
	t.Run("active journal", func(t *testing.T) {
		recoveryRoot := filepath.Join(t.TempDir(), "recovery")
		_, _ = captureInventoryJournal(t, recoveryRoot, "retire-read-capacity")
		filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
		plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
		authority, err := activeJournalAuthorityForTest(t.Context(), plan, filesystem)
		if err != nil {
			t.Fatal(err)
		}
		prepared, err := PrepareActiveJournalRetirement(
			t.Context(),
			plan,
			authority,
			recoveryRoot,
			recovery.MaximumPhysicalPathDepth,
			retirementTestBudget(t),
			filesystem,
			testStateCodec(),
		)
		if err != nil {
			t.Fatalf("PrepareActiveJournalRetirement: %v", err)
		}
		defer prepared.Close()

		currentRecord, err := retirement.Encode(prepared.execution.record)
		if err != nil {
			t.Fatal(err)
		}
		finalizing, err := prepared.execution.record.Finalizing()
		if err != nil {
			t.Fatal(err)
		}
		finalRecord, err := retirement.Encode(finalizing)
		if err != nil {
			t.Fatal(err)
		}
		wantBytes := int64(7)*prepared.evidence.activeEnvelopeLimits.MaximumBytes() +
			maximumRecoveryJournalBytes +
			2*prepared.evidence.controlCurrentWork.Bytes() +
			4*prepared.evidence.controlFinalWork.Bytes() +
			int64(len(currentRecord)+len(finalRecord))
		if got := prepared.executionBudget.RemainingBytes(); got != wantBytes {
			t.Fatalf("reserved retirement bytes = %d, want %d", got, wantBytes)
		}

		filesystem.resetReadObservations()
		if err := prepared.ExecuteActive(t.Context(), plan); err != nil {
			t.Fatalf("ExecuteActive: %v", err)
		}
		journalPath := filepath.Join(plan.OperationDir(), recoveryJournalFileName)
		if got := filesystem.regularFileReadLimits[journalPath]; !slices.Equal(got, []int64{maximumRecoveryJournalBytes}) {
			t.Fatalf("active journal read limits = %v, want [%d]", got, maximumRecoveryJournalBytes)
		}
	})

	t.Run("prepared control", func(t *testing.T) {
		recoveryRoot := filepath.Join(t.TempDir(), "recovery")
		identity, result := captureInventoryJournal(t, recoveryRoot, "cleanup-read-capacity")
		writeInventoryControl(t, recoveryRoot, identity, retirement.PhasePrepared)
		renameInventoryJournalToResidue(t, result, identity)
		filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
		plan := loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
		prepared, err := PrepareJournalCleanup(
			t.Context(),
			plan,
			recoveryRoot,
			recovery.MaximumPhysicalPathDepth,
			retirementTestBudget(t),
			filesystem,
		)
		if err != nil {
			t.Fatalf("PrepareJournalCleanup: %v", err)
		}
		defer prepared.Close()

		currentRecord, err := retirement.Encode(prepared.execution.record)
		if err != nil {
			t.Fatal(err)
		}
		finalizing, err := prepared.execution.record.Finalizing()
		if err != nil {
			t.Fatal(err)
		}
		finalRecord, err := retirement.Encode(finalizing)
		if err != nil {
			t.Fatal(err)
		}
		wantBytes := 2*prepared.evidence.controlCurrentWork.Bytes() +
			4*prepared.evidence.controlFinalWork.Bytes() +
			int64(len(currentRecord)+len(finalRecord)) +
			6*prepared.evidence.residue.work.Bytes()
		if got := prepared.executionBudget.RemainingBytes(); got != wantBytes {
			t.Fatalf("reserved cleanup bytes = %d, want %d", got, wantBytes)
		}

		filesystem.resetReadObservations()
		if err := prepared.ExecuteCleanup(t.Context(), plan); err != nil {
			t.Fatalf("ExecuteCleanup: %v", err)
		}
		if got := filesystem.controlSnapshots[retirement.PhasePrepared]; got != 2 {
			t.Fatalf("prepared control snapshots = %d, want 2", got)
		}
	})
}

func TestRetireActiveJournalRejectsReplacedPlannedArtifactBeforeEffects(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, result := captureInventoryJournal(t, recoveryRoot, "retire-replaced")
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
	activeAuthority, err := activeJournalAuthorityForTest(
		t.Context(),
		plan,
		filesystem,
	)
	if err != nil {
		t.Fatal(err)
	}
	root := captureRetirementTestRoot(t, recoveryRoot)

	content, err := os.ReadFile(result.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(filepath.Dir(recoveryRoot), "original-active")
	if err := os.Rename(plan.OperationDir(), original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(plan.OperationDir(), retirement.DirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.JournalPath, content, recoveryJournalMode); err != nil {
		t.Fatal(err)
	}

	err = retireActiveJournalWithAuthorityForTest(
		t.Context(),
		plan,
		activeAuthority,
		root,
		recovery.MaximumPhysicalPathDepth,
		retirementTestBudget(t),
		filesystem,
		testStateCodec(),
	)
	if err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("RetireActiveJournal error = %v, want identity-change rejection", err)
	}
	if len(filesystem.operations) != 0 {
		t.Fatalf("operations = %v, want none", filesystem.operations)
	}
	if _, err := os.Lstat(result.JournalPath); err != nil {
		t.Fatalf("replacement was changed: %v", err)
	}
}

func TestPreparedRetirementRejectsNonRecordTreeGrowthBeforeEffects(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, _ = captureInventoryJournal(t, recoveryRoot, "retire-tree-growth")
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
	activeAuthority, err := activeJournalAuthorityForTest(t.Context(), plan, filesystem)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareActiveJournalRetirement(
		t.Context(),
		plan,
		activeAuthority,
		recoveryRoot,
		recovery.MaximumPhysicalPathDepth,
		retirementTestBudget(t),
		filesystem,
		testStateCodec(),
	)
	if err != nil {
		t.Fatalf("PrepareActiveJournalRetirement: %v", err)
	}
	t.Cleanup(func() {
		if err := prepared.Close(); err != nil {
			t.Errorf("close prepared retirement: %v", err)
		}
	})
	if err := os.WriteFile(
		filepath.Join(plan.OperationDir(), "late-backup"),
		[]byte("late\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	err = prepared.ExecuteActive(t.Context(), plan)
	if err == nil {
		t.Fatal("ExecuteActive accepted non-record tree growth")
	}
	if len(filesystem.operations) != 0 {
		t.Fatalf("operations = %v, want none", filesystem.operations)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateActive)
}

func TestRetireActiveJournalRevalidatesFingerprintAfterPreparedControl(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, result := captureInventoryJournal(t, recoveryRoot, "retire-content-drift")
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
	activeAuthority, err := activeJournalAuthorityForTest(
		t.Context(),
		plan,
		filesystem,
	)
	if err != nil {
		t.Fatal(err)
	}
	filesystem.afterOperation = func(operation string) {
		if operation != "publish_control" {
			return
		}
		content, readErr := os.ReadFile(result.JournalPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		changed := bytes.Replace(
			content,
			[]byte("2026-07-30T12:00:00Z"),
			[]byte("2026-07-30T12:00:01Z"),
			1,
		)
		if bytes.Equal(content, changed) {
			t.Fatal("journal fixture did not contain expected created_at")
		}
		if writeErr := os.WriteFile(result.JournalPath, changed, recoveryJournalMode); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	root := captureRetirementTestRoot(t, recoveryRoot)

	err = retireActiveJournalWithAuthorityForTest(
		t.Context(),
		plan,
		activeAuthority,
		root,
		recovery.MaximumPhysicalPathDepth,
		retirementTestBudget(t),
		filesystem,
		testStateCodec(),
	)
	if err == nil || !strings.Contains(err.Error(), "changed before visibility transition") {
		t.Fatalf("RetireActiveJournal error = %v, want fingerprint-change rejection", err)
	}
	if !slices.Equal(filesystem.operations, []string{"publish_control"}) {
		t.Fatalf("operations = %v, want prepared control only", filesystem.operations)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateBlocked)
}

func TestRetireActiveJournalUsesCapturedPhysicalRootForSelectedAlias(t *testing.T) {
	parent := t.TempDir()
	physicalRoot := filepath.Join(parent, "physical-recovery")
	if err := os.Mkdir(physicalRoot, 0o700); err != nil {
		t.Fatalf("create physical recovery root: %v", err)
	}
	selectedRoot := filepath.Join(parent, "selected-recovery")
	if err := os.Symlink(physicalRoot, selectedRoot); err != nil {
		t.Fatalf("create recovery-root alias: %v", err)
	}
	_, _ = captureInventoryJournal(t, physicalRoot, "retire-through-alias")
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan := loadCleanActiveRetirementPlan(t, physicalRoot, filesystem)
	root := captureRetirementTestRoot(t, selectedRoot)

	if err := retireActiveJournalForTest(t.Context(), plan, root, filesystem); err != nil {
		t.Fatalf("RetireActiveJournal through alias: %v", err)
	}
	assertRetirementState(t, physicalRoot, retirement.StateClean)
}

func TestRetireActiveJournalRejectsMismatchedCapturedRootBeforeEffects(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, _ = captureInventoryJournal(t, recoveryRoot, "retire-root-mismatch")
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
	otherRoot := filepath.Join(t.TempDir(), "other-recovery")
	if err := os.Mkdir(otherRoot, 0o700); err != nil {
		t.Fatalf("create other recovery root: %v", err)
	}
	root := captureRetirementTestRoot(t, otherRoot)

	err := retireActiveJournalForTest(t.Context(), plan, root, filesystem)
	if err == nil || !strings.Contains(err.Error(), "does not match its operation id") {
		t.Fatalf("RetireActiveJournal error = %v, want root mismatch", err)
	}
	if len(filesystem.operations) != 0 {
		t.Fatalf("mismatched-root operations = %v, want none", filesystem.operations)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateActive)
}

func TestRetireActiveJournalResumesPreparedControl(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	identity, _ := captureInventoryJournal(t, recoveryRoot, "retire-prepared")
	writeInventoryControl(t, recoveryRoot, identity, retirement.PhasePrepared)
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
	root := captureRetirementTestRoot(t, recoveryRoot)

	if err := retireActiveJournalForTest(t.Context(), plan, root, filesystem); err != nil {
		t.Fatalf("RetireActiveJournal: %v", err)
	}
	wantOperations := []string{
		"rename_active",
		"advance_record",
		"cleanup_residue",
		"rename_control",
		"cleanup_gc",
	}
	if !slices.Equal(filesystem.operations, wantOperations) {
		t.Fatalf("operations = %v, want %v", filesystem.operations, wantOperations)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateClean)
}

func TestRetirementRevalidatesExactControlBeforeEffects(t *testing.T) {
	t.Run("active prepared control", func(t *testing.T) {
		recoveryRoot := filepath.Join(t.TempDir(), "recovery")
		identity, _ := captureInventoryJournal(t, recoveryRoot, "retire-foreign-control")
		writeInventoryControl(t, recoveryRoot, identity, retirement.PhasePrepared)
		filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
		plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
		foreignPath := filepath.Join(recoveryRoot, identity.ControlName(), "foreign")
		if err := os.WriteFile(foreignPath, []byte("foreign\n"), retirement.RecordMode); err != nil {
			t.Fatal(err)
		}
		root := captureRetirementTestRoot(t, recoveryRoot)

		err := retireActiveJournalForTest(t.Context(), plan, root, filesystem)
		if err == nil || !strings.Contains(err.Error(), "unexpected child") {
			t.Fatalf("RetireActiveJournal error = %v, want foreign-child rejection", err)
		}
		if len(filesystem.operations) != 0 {
			t.Fatalf("operations = %v, want none", filesystem.operations)
		}
		if _, err := os.Lstat(filepath.Join(recoveryRoot, plan.OperationID())); err != nil {
			t.Fatalf("active journal changed after rejection: %v", err)
		}
		if _, err := os.Lstat(foreignPath); err != nil {
			t.Fatalf("foreign control child changed after rejection: %v", err)
		}
	})

	t.Run("cleanup finalizing control", func(t *testing.T) {
		recoveryRoot := filepath.Join(t.TempDir(), "recovery")
		identity, result := captureInventoryJournal(t, recoveryRoot, "cleanup-foreign-control")
		writeInventoryControl(t, recoveryRoot, identity, retirement.PhaseFinalizing)
		renameInventoryJournalToResidue(t, result, identity)
		filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
		plan := loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
		foreignPath := filepath.Join(recoveryRoot, identity.ControlName(), "foreign")
		if err := os.WriteFile(foreignPath, []byte("foreign\n"), retirement.RecordMode); err != nil {
			t.Fatal(err)
		}
		root := captureRetirementTestRoot(t, recoveryRoot)

		err := finalizeJournalCleanupForTest(
			t.Context(),
			plan,
			root,
			recovery.MaximumPhysicalPathDepth,
			retirementTestBudget(t),
			filesystem,
		)
		if err == nil || !strings.Contains(err.Error(), "unexpected child") {
			t.Fatalf("FinalizeJournalCleanup error = %v, want foreign-child rejection", err)
		}
		if len(filesystem.operations) != 0 {
			t.Fatalf("operations = %v, want none", filesystem.operations)
		}
		if _, err := os.Lstat(filepath.Join(recoveryRoot, identity.ResidueName())); err != nil {
			t.Fatalf("residue changed after rejection: %v", err)
		}
		if _, err := os.Lstat(foreignPath); err != nil {
			t.Fatalf("foreign control child changed after rejection: %v", err)
		}
	})
}

func TestFinalizeJournalCleanupAdmitsInterruptedRecordTemporary(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	identity, result := captureInventoryJournal(t, recoveryRoot, "cleanup-record-temporary")
	writeInventoryControl(t, recoveryRoot, identity, retirement.PhaseFinalizing)
	renameInventoryJournalToResidue(t, result, identity)
	temporaryPath := filepath.Join(
		recoveryRoot,
		identity.ControlName(),
		".daem-tmp-interrupted",
	)
	if err := os.WriteFile(temporaryPath, []byte("partial\n"), retirement.RecordMode); err != nil {
		t.Fatal(err)
	}
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan := loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
	root := captureRetirementTestRoot(t, recoveryRoot)

	if err := finalizeJournalCleanupForTest(
		t.Context(),
		plan,
		root,
		recovery.MaximumPhysicalPathDepth,
		retirementTestBudget(t),
		filesystem,
	); err != nil {
		t.Fatalf("FinalizeJournalCleanup: %v", err)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateClean)
}

func TestFinalizeJournalCleanupRejectsSpecialResidueBeforePhaseAdvance(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	identity, result := captureInventoryJournal(t, recoveryRoot, "cleanup-special-residue")
	writeInventoryControl(t, recoveryRoot, identity, retirement.PhasePrepared)
	renameInventoryJournalToResidue(t, result, identity)
	residue := filepath.Join(recoveryRoot, identity.ResidueName())
	preserved := filepath.Join(residue, "a-preserved")
	if err := os.WriteFile(preserved, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	special := filepath.Join(residue, "z-special")
	if err := unix.Mkfifo(special, 0o600); err != nil {
		t.Fatal(err)
	}

	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan := loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
	root := captureRetirementTestRoot(t, recoveryRoot)
	err := finalizeJournalCleanupForTest(
		t.Context(),
		plan,
		root,
		recovery.MaximumPhysicalPathDepth,
		retirementTestBudget(t),
		filesystem,
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported entry") {
		t.Fatalf("FinalizeJournalCleanup error = %v, want special-entry rejection", err)
	}
	if len(filesystem.operations) != 0 {
		t.Fatalf("operations = %v, want no phase advance or cleanup", filesystem.operations)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateRetained)
	content, readErr := os.ReadFile(preserved)
	if readErr != nil || string(content) != "keep" {
		t.Fatalf("preserved child content = %q err=%v, want keep", content, readErr)
	}
	if info, statErr := os.Lstat(special); statErr != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("special child changed: info=%v err=%v", info, statErr)
	}
}

func TestFinalizeJournalCleanupRejectsResidueCreatedAfterPlanning(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	identity, result := captureInventoryJournal(t, recoveryRoot, "cleanup-late-residue")
	writeInventoryControl(t, recoveryRoot, identity, retirement.PhaseFinalizing)
	if err := os.RemoveAll(result.Directory); err != nil {
		t.Fatal(err)
	}
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan := loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
	residue := filepath.Join(recoveryRoot, identity.ResidueName())
	if err := os.Mkdir(residue, retirement.DirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(residue, "late"),
		[]byte("late\n"),
		retirement.RecordMode,
	); err != nil {
		t.Fatal(err)
	}
	root := captureRetirementTestRoot(t, recoveryRoot)

	err := finalizeJournalCleanupForTest(
		t.Context(),
		plan,
		root,
		recovery.MaximumPhysicalPathDepth,
		retirementTestBudget(t),
		filesystem,
	)
	if err == nil || !strings.Contains(err.Error(), "appeared after cleanup planning") {
		t.Fatalf("FinalizeJournalCleanup error = %v, want late-residue rejection", err)
	}
	if len(filesystem.operations) != 0 {
		t.Fatalf("operations = %v, want none", filesystem.operations)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateFinalizing)
}

func TestFinalizeJournalCleanupResumesEveryCleanupPhase(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(*testing.T, string)
		wantOperations []string
	}{
		{
			name: "prepared residue",
			setup: func(t *testing.T, recoveryRoot string) {
				identity, result := captureInventoryJournal(t, recoveryRoot, "cleanup-prepared")
				writeInventoryControl(t, recoveryRoot, identity, retirement.PhasePrepared)
				renameInventoryJournalToResidue(t, result, identity)
			},
			wantOperations: []string{
				"advance_record",
				"cleanup_residue",
				"rename_control",
				"cleanup_gc",
			},
		},
		{
			name: "finalizing residue",
			setup: func(t *testing.T, recoveryRoot string) {
				identity, result := captureInventoryJournal(t, recoveryRoot, "cleanup-finalizing")
				writeInventoryControl(t, recoveryRoot, identity, retirement.PhaseFinalizing)
				renameInventoryJournalToResidue(t, result, identity)
			},
			wantOperations: []string{
				"cleanup_residue",
				"rename_control",
				"cleanup_gc",
			},
		},
		{
			name: "finalizing absent residue",
			setup: func(t *testing.T, recoveryRoot string) {
				identity, result := captureInventoryJournal(t, recoveryRoot, "cleanup-absent")
				writeInventoryControl(t, recoveryRoot, identity, retirement.PhaseFinalizing)
				if err := os.RemoveAll(result.Directory); err != nil {
					t.Fatalf("remove active journal: %v", err)
				}
			},
			wantOperations: []string{"rename_control", "cleanup_gc"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			test.setup(t, recoveryRoot)
			filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
			plan := loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
			root := captureRetirementTestRoot(t, recoveryRoot)

			if err := finalizeJournalCleanupForTest(
				t.Context(),
				plan,
				root,
				recovery.MaximumPhysicalPathDepth,
				retirementTestBudget(t),
				filesystem,
			); err != nil {
				t.Fatalf("FinalizeJournalCleanup: %v", err)
			}
			if !slices.Equal(filesystem.operations, test.wantOperations) {
				t.Fatalf(
					"operations = %v, want %v",
					filesystem.operations,
					test.wantOperations,
				)
			}
			assertRetirementState(t, recoveryRoot, retirement.StateClean)
		})
	}
}

func TestRetirementDoesNotCleanGCAfterControlRenameReportsFailure(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, _ = captureInventoryJournal(t, recoveryRoot, "retire-gc-failure")
	filesystem := &retirementRecordingFilesystem{
		Store:     journalTestFilesystem(),
		failAfter: "rename_control",
	}
	plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
	root := captureRetirementTestRoot(t, recoveryRoot)

	err := retireActiveJournalForTest(t.Context(), plan, root, filesystem)
	const want = "journal retirement committed; hidden GC cleanup did not complete successfully; no recovery action remains"
	if err == nil ||
		!IsRetirementFinalizedWithGCResidue(err) ||
		err.Error() != want {
		t.Fatalf("RetireActiveJournal error = %v, want finalized GC residue", err)
	}
	if slices.Contains(filesystem.operations, "cleanup_gc") {
		t.Fatalf("operations = %v, must not clean GC after rename failure", filesystem.operations)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateFinalized)
}

func TestRetirementPostVisibilityFailuresRemainClassifiableAndResumable(t *testing.T) {
	tests := []struct {
		name          string
		operation     string
		wantState     retirement.State
		resume        bool
		wantFinalized bool
	}{
		{
			name:      "prepared control publication",
			operation: "publish_control",
			wantState: retirement.StatePrepared,
			resume:    true,
		},
		{
			name:      "active to residue rename",
			operation: "rename_active",
			wantState: retirement.StateRetained,
			resume:    true,
		},
		{
			name:      "finalizing record replacement",
			operation: "advance_record",
			wantState: retirement.StateFinalizing,
			resume:    true,
		},
		{
			name:      "residue cleanup",
			operation: "cleanup_residue",
			wantState: retirement.StateFinalizing,
			resume:    true,
		},
		{
			name:          "control to GC rename",
			operation:     "rename_control",
			wantState:     retirement.StateFinalized,
			wantFinalized: true,
		},
		{
			name:          "physical GC cleanup",
			operation:     "cleanup_gc",
			wantState:     retirement.StateFinalized,
			wantFinalized: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			_, _ = captureInventoryJournal(t, recoveryRoot, "retire-"+test.operation)
			filesystem := &retirementRecordingFilesystem{
				Store:     journalTestFilesystem(),
				failAfter: test.operation,
			}
			if test.operation == "cleanup_gc" {
				filesystem.failAfter = ""
				filesystem.failBefore = "cleanup_gc"
			}
			plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
			root := captureRetirementTestRoot(t, recoveryRoot)

			err := retireActiveJournalForTest(t.Context(), plan, root, filesystem)
			if err == nil {
				t.Fatal("RetireActiveJournal returned nil, want failure")
			}
			if test.wantFinalized {
				const want = "journal retirement committed; hidden GC cleanup did not complete successfully; no recovery action remains"
				if !IsRetirementFinalizedWithGCResidue(err) || err.Error() != want {
					t.Fatalf("RetireActiveJournal error = %v, want finalized GC residue", err)
				}
			} else if !strings.Contains(err.Error(), "injected") {
				t.Fatalf("RetireActiveJournal error = %v, want injected failure", err)
			}
			assertRetirementState(t, recoveryRoot, test.wantState)

			if !test.resume {
				return
			}
			filesystem.failAfter = ""
			if test.wantState == retirement.StatePrepared {
				if err := retireActiveJournalForTest(t.Context(), plan, root, filesystem); err != nil {
					t.Fatalf("resume active retirement: %v", err)
				}
			} else {
				cleanup := loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
				if err := finalizeJournalCleanupForTest(
					t.Context(),
					cleanup,
					root,
					recovery.MaximumPhysicalPathDepth,
					retirementTestBudget(t),
					filesystem,
				); err != nil {
					t.Fatalf("resume cleanup retirement: %v", err)
				}
			}
			assertRetirementState(t, recoveryRoot, retirement.StateClean)
		})
	}
}

func TestRetirementCancellationAfterEachPhaseRemainsClassifiableAndResumable(t *testing.T) {
	tests := []struct {
		operation string
		wantState retirement.State
	}{
		{operation: "publish_control", wantState: retirement.StatePrepared},
		{operation: "rename_active", wantState: retirement.StateRetained},
		{operation: "advance_record", wantState: retirement.StateFinalizing},
		{operation: "cleanup_residue", wantState: retirement.StateFinalizing},
		{operation: "rename_control", wantState: retirement.StateFinalized},
		{operation: "cleanup_gc", wantState: retirement.StateClean},
	}

	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			_, _ = captureInventoryJournal(t, recoveryRoot, "cancel-"+test.operation)
			ctx, cancel := context.WithCancel(t.Context())
			filesystem := &retirementRecordingFilesystem{
				Store:       journalTestFilesystem(),
				cancelAfter: test.operation,
				cancel:      cancel,
			}
			plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
			root := captureRetirementTestRoot(t, recoveryRoot)

			err := retireActiveJournalForTest(ctx, plan, root, filesystem)
			if test.operation == "cleanup_gc" {
				if err != nil {
					t.Fatalf("RetireActiveJournal after final cleanup: %v", err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("RetireActiveJournal error = %v, want context cancellation", err)
			}
			assertRetirementState(t, recoveryRoot, test.wantState)

			switch test.wantState {
			case retirement.StatePrepared:
				fresh := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
				if err := retireActiveJournalForTest(t.Context(), fresh, root, filesystem); err != nil {
					t.Fatalf("resume active retirement: %v", err)
				}
			case retirement.StateRetained, retirement.StateFinalizing:
				cleanup := loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
				if err := finalizeJournalCleanupForTest(
					t.Context(),
					cleanup,
					root,
					recovery.MaximumPhysicalPathDepth,
					retirementTestBudget(t),
					filesystem,
				); err != nil {
					t.Fatalf("resume cleanup retirement: %v", err)
				}
			default:
				return
			}
			assertRetirementState(t, recoveryRoot, retirement.StateClean)
		})
	}
}

func loadCleanActiveRetirementPlan(
	t *testing.T,
	recoveryRoot string,
	filesystem mutationfs.Store,
) recovery.Plan {
	t.Helper()
	plan, err := LoadActivePlanForStateWithOptions(
		t.Context(),
		Paths{RecoveryDir: recoveryRoot, ManifestRoot: filepath.Dir(recoveryRoot)},
		beforeStatefile(),
		PlanLoadOptions{
			Filesystem: filesystem,
			Resolver: func(output.Destination) (string, error) {
				return "", nil
			},
			StateCodec: testStateCodec(),
		},
	)
	if err != nil {
		t.Fatalf("LoadActivePlanForStateWithOptions: %v", err)
	}
	if plan.Classification() != recovery.ClassificationCleanBefore {
		t.Fatalf("classification = %q, want clean_before", plan.Classification())
	}
	return plan
}

func retireActiveJournalForTest(
	ctx context.Context,
	plan recovery.Plan,
	root *rootedpath.CapturedRoot,
	filesystem mutationfs.Store,
) error {
	authority, err := activeJournalAuthorityForTest(ctx, plan, filesystem)
	if err != nil {
		return err
	}
	budget, err := recovery.NewPhysicalWorkBudget(0)
	if err != nil {
		return err
	}
	return retireActiveJournalWithAuthorityForTest(
		ctx,
		plan,
		authority,
		root,
		recovery.MaximumPhysicalPathDepth,
		budget,
		filesystem,
		testStateCodec(),
	)
}

func retireActiveJournalWithAuthorityForTest(
	ctx context.Context,
	plan recovery.Plan,
	authority ActiveJournalAuthority,
	root *rootedpath.CapturedRoot,
	maximumPhysicalDepth int,
	physicalWorkBudget rootedpath.PhysicalTraversalBudget,
	filesystem mutationfs.RootedStore,
	stateCodec durable.SnapshotCodec,
) error {
	budget, ok := physicalWorkBudget.(*recovery.PhysicalWorkBudget)
	if !ok {
		return fmt.Errorf("test retirement requires the canonical physical work budget")
	}
	rootAuthority, err := root.AuthorityBounded(budget)
	if err != nil {
		return err
	}
	prepared, err := PrepareActiveJournalRetirement(
		ctx,
		plan,
		authority,
		rootAuthority.PhysicalRoot(),
		maximumPhysicalDepth,
		budget,
		filesystem,
		stateCodec,
	)
	if err != nil {
		return err
	}
	defer prepared.Close()
	return prepared.ExecuteActive(ctx, plan)
}

func finalizeJournalCleanupForTest(
	ctx context.Context,
	plan retirement.CleanupPlan,
	root *rootedpath.CapturedRoot,
	maximumPhysicalDepth int,
	physicalWorkBudget rootedpath.PhysicalTraversalBudget,
	filesystem mutationfs.RootedStore,
) error {
	budget, ok := physicalWorkBudget.(*recovery.PhysicalWorkBudget)
	if !ok {
		return fmt.Errorf("test cleanup requires the canonical physical work budget")
	}
	rootAuthority, err := root.AuthorityBounded(budget)
	if err != nil {
		return err
	}
	prepared, err := PrepareJournalCleanup(
		ctx,
		plan,
		rootAuthority.PhysicalRoot(),
		maximumPhysicalDepth,
		budget,
		filesystem,
	)
	if err != nil {
		return err
	}
	defer prepared.Close()
	return prepared.ExecuteCleanup(ctx, plan)
}

func retirementTestBudget(t *testing.T) *recovery.PhysicalWorkBudget {
	t.Helper()
	budget, err := recovery.NewPhysicalWorkBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func activeJournalAuthorityForTest(
	ctx context.Context,
	plan recovery.Plan,
	filesystem mutationfs.Store,
) (ActiveJournalAuthority, error) {
	identity, err := filesystem.CaptureEntryIdentity(ctx, plan.OperationDir())
	if err != nil {
		return ActiveJournalAuthority{}, fmt.Errorf(
			"capture active recovery journal identity: %w",
			err,
		)
	}
	authority, err := newActiveJournalAuthority(identity)
	if err != nil {
		return ActiveJournalAuthority{}, err
	}
	return authority, nil
}

func loadCleanupRetirementPlan(
	t *testing.T,
	recoveryRoot string,
	filesystem mutationfs.Store,
) retirement.CleanupPlan {
	t.Helper()
	recoverable, err := LoadRecoverablePlanWithOptions(
		t.Context(),
		Paths{RecoveryDir: recoveryRoot, ManifestRoot: filepath.Dir(recoveryRoot)},
		PlanLoadOptions{
			Filesystem: filesystem,
			StateCodec: testStateCodec(),
		},
	)
	if err != nil {
		t.Fatalf("LoadRecoverablePlanWithOptions: %v", err)
	}
	plan, ok := JournalCleanupPlan(recoverable)
	if !ok {
		t.Fatalf(
			"authority kind = %q, want journal cleanup",
			recoverable.AuthorityKind(),
		)
	}
	return plan
}

func captureRetirementTestRoot(t *testing.T, recoveryRoot string) *rootedpath.CapturedRoot {
	t.Helper()
	root, err := rootedpath.CaptureRoot(recoveryRoot)
	if err != nil {
		t.Fatalf("CaptureRoot: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close retirement root: %v", err)
		}
	})
	return root
}

func assertRetirementState(t *testing.T, recoveryRoot string, want retirement.State) {
	t.Helper()
	inventory, err := loadRecoveryRootInventory(
		t.Context(),
		recoveryRoot,
		inventoryOptions{
			Filesystem: journalTestFilesystem(),
			StateCodec: testStateCodec(),
		},
	)
	if err != nil {
		t.Fatalf("loadRecoveryRootInventory: %v", err)
	}
	if inventory.decision.State() != want {
		t.Fatalf(
			"retirement state = %q detail=%q, want %q",
			inventory.decision.State(),
			inventory.decision.Detail(),
			want,
		)
	}
}

type retirementRecordingFilesystem struct {
	mutationfs.Store
	operations            []string
	regularFileReadLimits map[string][]int64
	controlSnapshots      map[retirement.Phase]int
	failBefore            string
	failAfter             string
	cancelAfter           string
	cancel                context.CancelFunc
	afterOperation        func(string)
}

func (filesystem *retirementRecordingFilesystem) resetReadObservations() {
	filesystem.regularFileReadLimits = make(map[string][]int64)
	filesystem.controlSnapshots = make(map[retirement.Phase]int)
}

func (filesystem *retirementRecordingFilesystem) ReadRootedRegularFileUpTo(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	maximumBytes int64,
) ([]byte, os.FileMode, mutationfs.EntryIdentity, error) {
	if filesystem.regularFileReadLimits != nil {
		path, err := capability.Destination().LexicalPath()
		if err != nil {
			_ = capability.Close()
			return nil, 0, nil, err
		}
		filesystem.regularFileReadLimits[path] = append(
			filesystem.regularFileReadLimits[path],
			maximumBytes,
		)
	}
	return filesystem.Store.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
}

func (filesystem *retirementRecordingFilesystem) SnapshotRootedDirectory(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	limits mutationfs.TreeTraversalLimits,
	sink mutationfs.RootedTreeSnapshotSink,
) (mutationfs.EntryIdentity, error) {
	if control, ok := sink.(*retirementControlSnapshotSink); ok && filesystem.controlSnapshots != nil {
		filesystem.controlSnapshots[control.expected.Phase()]++
	}
	return filesystem.Store.SnapshotRootedDirectory(ctx, capability, limits, sink)
}

func (filesystem *retirementRecordingFilesystem) PrepareRootedTree(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	populate func(mutationfs.RootedTreeWriter) error,
) (mutationfs.PreparedRootedTree, error) {
	prepared, err := filesystem.Store.PrepareRootedTree(ctx, capability, populate)
	if err != nil {
		return nil, err
	}
	return retirementRecordingPreparedTree{
		PreparedRootedTree: prepared,
		operation:          "publish_control",
		record: func() {
			filesystem.operations = append(filesystem.operations, "publish_control")
		},
		failAfter: filesystem.failAfter == "publish_control",
		after: func() {
			filesystem.cancelAfterOperation("publish_control")
		},
	}, nil
}

func (filesystem *retirementRecordingFilesystem) RenameRootedEntry(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	destinationName string,
	expected mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, error) {
	operation := "rename_active"
	switch {
	case strings.HasPrefix(destinationName, ".daem-journal-gc-"):
		operation = "rename_control"
	}
	filesystem.operations = append(filesystem.operations, operation)
	outcome, err := filesystem.Store.RenameRootedEntry(
		ctx,
		capability,
		destinationName,
		expected,
	)
	if err == nil {
		filesystem.cancelAfterOperation(operation)
	}
	if err == nil && filesystem.failAfter == operation {
		if operation == "rename_control" {
			return outcome, fmt.Errorf("injected %s failure", operation)
		}
		return injectedRetirementFailure(operation, destinationName)
	}
	return outcome, err
}

func (filesystem *retirementRecordingFilesystem) ReplaceRootedFileWithOutcome(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode os.FileMode,
	expected mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, error) {
	const operation = "advance_record"
	filesystem.operations = append(filesystem.operations, operation)
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		_ = capability.Close()
		return mutationfs.CommitOutcome{}, err
	}
	outcome, err := filesystem.Store.ReplaceRootedFileWithOutcome(
		ctx,
		capability,
		content,
		mode,
		expected,
	)
	if err == nil {
		filesystem.cancelAfterOperation(operation)
	}
	if err == nil && filesystem.failAfter == operation {
		return injectedRetirementFailure(operation, path)
	}
	return outcome, err
}

func (filesystem *retirementRecordingFilesystem) CleanupRootedEntry(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
	limits mutationfs.TreeTraversalLimits,
) (mutationfs.CommitOutcome, error) {
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		_ = capability.Close()
		return mutationfs.CommitOutcome{}, err
	}
	operation := "cleanup_residue"
	if strings.HasPrefix(filepath.Base(path), ".daem-journal-gc-") {
		operation = "cleanup_gc"
	}
	filesystem.operations = append(filesystem.operations, operation)
	if filesystem.failBefore == operation {
		return mutationfs.CommitOutcome{}, errors.Join(
			fmt.Errorf("injected %s failure", operation),
			capability.Close(),
		)
	}
	outcome, err := filesystem.Store.CleanupRootedEntry(ctx, capability, expected, limits)
	if err == nil {
		filesystem.cancelAfterOperation(operation)
	}
	if err == nil && filesystem.failAfter == operation {
		return injectedRetirementFailure(operation, path)
	}
	return outcome, err
}

type retirementRecordingPreparedTree struct {
	mutationfs.PreparedRootedTree
	operation string
	record    func()
	failAfter bool
	after     func()
}

func (prepared retirementRecordingPreparedTree) CommitWithOutcome(
	ctx context.Context,
) (mutationfs.CommitOutcome, error) {
	prepared.record()
	outcome, err := prepared.PreparedRootedTree.CommitWithOutcome(ctx)
	if err == nil && prepared.after != nil {
		prepared.after()
	}
	if err == nil && prepared.failAfter {
		return injectedRetirementFailure(prepared.operation, prepared.operation)
	}
	return outcome, err
}

func (filesystem *retirementRecordingFilesystem) cancelAfterOperation(operation string) {
	if filesystem.afterOperation != nil {
		filesystem.afterOperation(operation)
	}
	if filesystem.cancelAfter != operation || filesystem.cancel == nil {
		return
	}
	filesystem.cancelAfter = ""
	filesystem.cancel()
}

func injectedRetirementFailure(
	operation string,
	path string,
) (mutationfs.CommitOutcome, error) {
	retainedName := filepath.Base(path)
	outcome, err := mutationfs.NewCommitOutcome(
		mutationfs.CommitOutcomeIndeterminate,
		[]string{retainedName},
	)
	if err != nil {
		panic(err)
	}
	return outcome, fmt.Errorf("injected %s failure", operation)
}
