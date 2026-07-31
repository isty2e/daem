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

	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"golang.org/x/sys/unix"
)

func TestFinalizeJournalCleanupMigratesLegacyTombstoneAndPreparedControl(t *testing.T) {
	tests := []struct {
		name           string
		prepareControl bool
		wantOperations []string
	}{
		{
			name: "legacy only",
			wantOperations: []string{
				"publish_control",
				"rename_legacy",
				"advance_record",
				"cleanup_residue",
				"rename_control",
				"cleanup_gc",
			},
		},
		{
			name:           "prepared control and legacy",
			prepareControl: true,
			wantOperations: []string{
				"rename_legacy",
				"advance_record",
				"cleanup_residue",
				"rename_control",
				"cleanup_gc",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			identity, result := captureInventoryJournal(
				t,
				recoveryRoot,
				"migrate-"+strings.ReplaceAll(test.name, " ", "-"),
			)
			if test.prepareControl {
				writeInventoryControl(t, recoveryRoot, identity, retirement.PhasePrepared)
			}
			legacyPath := renameInventoryJournalToLegacy(t, result, "a")
			filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
			plan, authority := loadLegacyCleanupRetirementPlan(
				t,
				recoveryRoot,
				filesystem,
			)
			root := captureRetirementTestRoot(t, recoveryRoot)

			if err := FinalizeJournalCleanup(
				t.Context(),
				plan,
				authority,
				root,
				filesystem,
				testStateCodec(),
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
			if _, err := os.Lstat(legacyPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("legacy tombstone still exists: %v", err)
			}
			assertRetirementState(t, recoveryRoot, retirement.StateClean)
		})
	}
}

func TestFinalizeJournalCleanupRejectsReplacedLegacyTombstoneBeforeEffects(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, result := captureInventoryJournal(t, recoveryRoot, "migrate-replaced")
	legacyPath := renameInventoryJournalToLegacy(t, result, "b")
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan, authority := loadLegacyCleanupRetirementPlan(t, recoveryRoot, filesystem)
	root := captureRetirementTestRoot(t, recoveryRoot)

	journalPath := filepath.Join(legacyPath, recoveryJournalFileName)
	content, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(filepath.Dir(recoveryRoot), "original-legacy")
	if err := os.Rename(legacyPath, original); err != nil {
		t.Fatal(err)
	}
	mkdirPrivate(t, legacyPath)
	writePrivateFile(t, journalPath, content)

	err = FinalizeJournalCleanup(
		t.Context(),
		plan,
		authority,
		root,
		filesystem,
		testStateCodec(),
	)
	if err == nil || !strings.Contains(err.Error(), "identity changed before migration") {
		t.Fatalf("FinalizeJournalCleanup error = %v, want identity-change rejection", err)
	}
	if len(filesystem.operations) != 0 {
		t.Fatalf("operations = %v, want none", filesystem.operations)
	}
	if _, err := os.Lstat(journalPath); err != nil {
		t.Fatalf("replacement legacy journal changed: %v", err)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateLegacyMigration)
}

func TestFinalizeJournalCleanupRevalidatesLegacyFingerprintAfterPreparedControl(
	t *testing.T,
) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, result := captureInventoryJournal(t, recoveryRoot, "migrate-content-drift")
	legacyPath := renameInventoryJournalToLegacy(t, result, "c")
	journalPath := filepath.Join(legacyPath, recoveryJournalFileName)
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan, authority := loadLegacyCleanupRetirementPlan(t, recoveryRoot, filesystem)
	filesystem.afterOperation = func(operation string) {
		if operation != "publish_control" {
			return
		}
		content, readErr := os.ReadFile(journalPath)
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
		writePrivateFile(t, journalPath, changed)
	}
	root := captureRetirementTestRoot(t, recoveryRoot)

	err := FinalizeJournalCleanup(
		t.Context(),
		plan,
		authority,
		root,
		filesystem,
		testStateCodec(),
	)
	if err == nil || !strings.Contains(err.Error(), "changed before visibility transition") {
		t.Fatalf("FinalizeJournalCleanup error = %v, want fingerprint-change rejection", err)
	}
	if !slices.Equal(filesystem.operations, []string{"publish_control"}) {
		t.Fatalf("operations = %v, want prepared control only", filesystem.operations)
	}
	if _, err := os.Lstat(legacyPath); err != nil {
		t.Fatalf("legacy tombstone changed after rejection: %v", err)
	}
	assertRetirementState(t, recoveryRoot, retirement.StateBlocked)
}

func TestFinalizeJournalCleanupRejectsSpecialLegacyChildBeforeControl(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, result := captureInventoryJournal(t, recoveryRoot, "migrate-special-child")
	legacyPath := renameInventoryJournalToLegacy(t, result, "d")
	mutableDirectory := filepath.Join(legacyPath, "mutable")
	if err := os.Mkdir(mutableDirectory, retirement.DirectoryMode); err != nil {
		t.Fatal(err)
	}
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan, authority := loadLegacyCleanupRetirementPlan(t, recoveryRoot, filesystem)
	root := captureRetirementTestRoot(t, recoveryRoot)

	specialPath := filepath.Join(mutableDirectory, "foreign-fifo")
	if err := unix.Mkfifo(specialPath, 0o600); err != nil {
		t.Fatal(err)
	}

	err := FinalizeJournalCleanup(
		t.Context(),
		plan,
		authority,
		root,
		filesystem,
		testStateCodec(),
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported entry") {
		t.Fatalf("FinalizeJournalCleanup error = %v, want special-entry rejection", err)
	}
	if len(filesystem.operations) != 0 {
		t.Fatalf("operations = %v, want none", filesystem.operations)
	}
	if info, statErr := os.Lstat(specialPath); statErr != nil ||
		info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("special child changed: info=%v err=%v", info, statErr)
	}
}

func TestFinalizeJournalCleanupRejectsOverdeepLegacyTreeBeforeControl(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, result := captureInventoryJournal(t, recoveryRoot, "migrate-overdeep-tree")
	legacyPath := renameInventoryJournalToLegacy(t, result, "1")
	mutableDirectory := filepath.Join(legacyPath, "mutable")
	if err := os.Mkdir(mutableDirectory, retirement.DirectoryMode); err != nil {
		t.Fatal(err)
	}
	filesystem := &retirementRecordingFilesystem{Store: journalTestFilesystem()}
	plan, authority := loadLegacyCleanupRetirementPlan(t, recoveryRoot, filesystem)
	root := captureRetirementTestRoot(t, recoveryRoot)

	deepest := mutableDirectory
	for index := 0; index <= maximumRecoveryTreeDepth; index++ {
		deepest = filepath.Join(deepest, fmt.Sprintf("depth-%02d", index))
		if err := os.Mkdir(deepest, retirement.DirectoryMode); err != nil {
			t.Fatalf("create depth %d: %v", index, err)
		}
	}

	err := FinalizeJournalCleanup(
		t.Context(),
		plan,
		authority,
		root,
		filesystem,
		testStateCodec(),
	)
	if err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("FinalizeJournalCleanup error = %v, want depth-bound rejection", err)
	}
	if len(filesystem.operations) != 0 {
		t.Fatalf("operations = %v, want none", filesystem.operations)
	}
	if _, err := os.Lstat(deepest); err != nil {
		t.Fatalf("overdeep legacy tree changed: %v", err)
	}
}

func TestLegacyMigrationFailuresAtUniqueCommitPointsResume(t *testing.T) {
	tests := []struct {
		operation string
		wantState retirement.State
	}{
		{operation: "publish_control", wantState: retirement.StateLegacyPrepared},
		{operation: "rename_legacy", wantState: retirement.StateRetained},
	}

	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			_, result := captureInventoryJournal(
				t,
				recoveryRoot,
				"migrate-fail-"+test.operation,
			)
			renameInventoryJournalToLegacy(t, result, "e")
			filesystem := &retirementRecordingFilesystem{
				Store:     journalTestFilesystem(),
				failAfter: test.operation,
			}
			plan, authority := loadLegacyCleanupRetirementPlan(
				t,
				recoveryRoot,
				filesystem,
			)
			root := captureRetirementTestRoot(t, recoveryRoot)

			err := FinalizeJournalCleanup(
				t.Context(),
				plan,
				authority,
				root,
				filesystem,
				testStateCodec(),
			)
			if err == nil || !strings.Contains(err.Error(), "injected") {
				t.Fatalf("FinalizeJournalCleanup error = %v, want injected failure", err)
			}
			assertRetirementState(t, recoveryRoot, test.wantState)

			filesystem.failAfter = ""
			if test.wantState == retirement.StateLegacyPrepared {
				plan, authority = loadLegacyCleanupRetirementPlan(
					t,
					recoveryRoot,
					filesystem,
				)
			} else {
				plan = loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
				authority = LegacyJournalAuthority{}
			}
			if err := FinalizeJournalCleanup(
				t.Context(),
				plan,
				authority,
				root,
				filesystem,
				testStateCodec(),
			); err != nil {
				t.Fatalf("resume legacy migration: %v", err)
			}
			assertRetirementState(t, recoveryRoot, retirement.StateClean)
		})
	}
}

func TestLegacyMigrationCancellationAtUniqueCommitPointsResumes(t *testing.T) {
	tests := []struct {
		operation string
		wantState retirement.State
	}{
		{operation: "publish_control", wantState: retirement.StateLegacyPrepared},
		{operation: "rename_legacy", wantState: retirement.StateRetained},
	}

	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			_, result := captureInventoryJournal(
				t,
				recoveryRoot,
				"migrate-cancel-"+test.operation,
			)
			renameInventoryJournalToLegacy(t, result, "f")
			ctx, cancel := context.WithCancel(t.Context())
			filesystem := &retirementRecordingFilesystem{
				Store:       journalTestFilesystem(),
				cancelAfter: test.operation,
				cancel:      cancel,
			}
			plan, authority := loadLegacyCleanupRetirementPlan(
				t,
				recoveryRoot,
				filesystem,
			)
			root := captureRetirementTestRoot(t, recoveryRoot)

			err := FinalizeJournalCleanup(
				ctx,
				plan,
				authority,
				root,
				filesystem,
				testStateCodec(),
			)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("FinalizeJournalCleanup error = %v, want cancellation", err)
			}
			assertRetirementState(t, recoveryRoot, test.wantState)

			if test.wantState == retirement.StateLegacyPrepared {
				plan, authority = loadLegacyCleanupRetirementPlan(
					t,
					recoveryRoot,
					filesystem,
				)
			} else {
				plan = loadCleanupRetirementPlan(t, recoveryRoot, filesystem)
				authority = LegacyJournalAuthority{}
			}
			if err := FinalizeJournalCleanup(
				t.Context(),
				plan,
				authority,
				root,
				filesystem,
				testStateCodec(),
			); err != nil {
				t.Fatalf("resume legacy migration: %v", err)
			}
			assertRetirementState(t, recoveryRoot, retirement.StateClean)
		})
	}
}
