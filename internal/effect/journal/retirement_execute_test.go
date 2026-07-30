package journal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
)

func TestRetireActiveJournalUsesCanonicalProtocolAndRejectsStaleReplay(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, _ = captureInventoryJournal(t, recoveryRoot, "retire-active")
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan := loadCleanActiveRetirementPlan(t, recoveryRoot, filesystem)
	root := captureRetirementTestRoot(t, recoveryRoot)

	if err := RetireActiveJournal(t.Context(), plan, root, filesystem); err != nil {
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
	err := RetireActiveJournal(t.Context(), plan, root, filesystem)
	if err == nil || !strings.Contains(err.Error(), "active recovery journal") {
		t.Fatalf("stale RetireActiveJournal error = %v, want active-journal refusal", err)
	}
	if len(filesystem.operations) != 0 {
		t.Fatalf("stale replay operations = %v, want none", filesystem.operations)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateClean)
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

	if err := RetireActiveJournal(t.Context(), plan, root, filesystem); err != nil {
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

	err := RetireActiveJournal(t.Context(), plan, root, filesystem)
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

	if err := RetireActiveJournal(t.Context(), plan, root, filesystem); err != nil {
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

		err := RetireActiveJournal(t.Context(), plan, root, filesystem)
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

		err := FinalizeJournalCleanup(t.Context(), plan, root, filesystem)
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

	if err := FinalizeJournalCleanup(t.Context(), plan, root, filesystem); err != nil {
		t.Fatalf("FinalizeJournalCleanup: %v", err)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateClean)
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

			if err := FinalizeJournalCleanup(t.Context(), plan, root, filesystem); err != nil {
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

	err := RetireActiveJournal(t.Context(), plan, root, filesystem)
	if err == nil || !strings.Contains(err.Error(), "injected rename_control failure") {
		t.Fatalf("RetireActiveJournal error = %v, want injected rename failure", err)
	}
	if slices.Contains(filesystem.operations, "cleanup_gc") {
		t.Fatalf("operations = %v, must not clean GC after rename failure", filesystem.operations)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateFinalized)
}

func TestRetirementPostVisibilityFailuresRemainClassifiableAndResumable(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		wantState retirement.State
		resume    bool
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
			name:      "control to GC rename",
			operation: "rename_control",
			wantState: retirement.StateFinalized,
		},
		{
			name:      "physical GC cleanup",
			operation: "cleanup_gc",
			wantState: retirement.StateFinalized,
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

			err := RetireActiveJournal(t.Context(), plan, root, filesystem)
			if err == nil || !strings.Contains(err.Error(), "injected") {
				t.Fatalf("RetireActiveJournal error = %v, want injected failure", err)
			}
			assertRetirementState(t, recoveryRoot, test.wantState)

			if !test.resume {
				return
			}
			filesystem.failAfter = ""
			if test.wantState == retirement.StatePrepared {
				if err := RetireActiveJournal(t.Context(), plan, root, filesystem); err != nil {
					t.Fatalf("resume active retirement: %v", err)
				}
			} else {
				cleanup := loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
				if err := FinalizeJournalCleanup(t.Context(), cleanup, root, filesystem); err != nil {
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

			err := RetireActiveJournal(ctx, plan, root, filesystem)
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
				if err := RetireActiveJournal(t.Context(), fresh, root, filesystem); err != nil {
					t.Fatalf("resume active retirement: %v", err)
				}
			case retirement.StateRetained, retirement.StateFinalizing:
				cleanup := loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
				if err := FinalizeJournalCleanup(t.Context(), cleanup, root, filesystem); err != nil {
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
		Paths{RecoveryDir: recoveryRoot},
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

func loadCleanupRetirementPlan(
	t *testing.T,
	recoveryRoot string,
	filesystem mutationfs.Store,
) retirement.CleanupPlan {
	t.Helper()
	recoverable, err := LoadRecoverablePlanWithOptions(
		t.Context(),
		Paths{RecoveryDir: recoveryRoot},
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
	operations  []string
	failBefore  string
	failAfter   string
	cancelAfter string
	cancel      context.CancelFunc
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
	if strings.HasPrefix(destinationName, ".daem-journal-gc-") {
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
	outcome, err := filesystem.Store.CleanupRootedEntry(ctx, capability, expected)
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
