package journal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestRecoveryRootInventoryBlocksMultiplePhysicalAuthorities(t *testing.T) {
	t.Run("active journals", func(t *testing.T) {
		recoveryRoot := filepath.Join(t.TempDir(), "recovery")
		_, first := captureInventoryJournal(t, recoveryRoot, "multiple-active-first")
		holdingPath := filepath.Join(t.TempDir(), filepath.Base(first.Directory))
		if err := os.Rename(first.Directory, holdingPath); err != nil {
			t.Fatalf("hold first active journal: %v", err)
		}
		captureInventoryJournal(t, recoveryRoot, "multiple-active-second")
		if err := os.Rename(holdingPath, first.Directory); err != nil {
			t.Fatalf("restore first active journal: %v", err)
		}

		assertRecoveryInventoryBlocked(t, recoveryRoot, "multiple active recovery journals")
	})

	t.Run("controls", func(t *testing.T) {
		recoveryRoot := filepath.Join(t.TempDir(), "recovery")
		first := inventoryTestIdentity(t, "multiple-control-first", "a")
		second := inventoryTestIdentity(t, "multiple-control-second", "b")
		writeInventoryControl(t, recoveryRoot, first, retirement.PhasePrepared)
		writeInventoryControl(t, recoveryRoot, second, retirement.PhaseFinalizing)

		assertRecoveryInventoryBlocked(t, recoveryRoot, "multiple journal retirement controls")
	})

	t.Run("residues", func(t *testing.T) {
		recoveryRoot := filepath.Join(t.TempDir(), "recovery")
		first := inventoryTestIdentity(t, "multiple-residue-first", "c")
		second := inventoryTestIdentity(t, "multiple-residue-second", "d")
		mkdirPrivate(t, filepath.Join(recoveryRoot, first.ResidueName()))
		mkdirPrivate(t, filepath.Join(recoveryRoot, second.ResidueName()))

		assertRecoveryInventoryBlocked(t, recoveryRoot, "multiple journal retirement residues")
	})
}

func TestRecoveryRootInventoryBlocksUnsupportedPhysicalCrossProducts(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
		want  string
	}{
		{
			name: "active with finalizing control",
			setup: func(t *testing.T, root string) {
				identity, _ := captureInventoryJournal(t, root, "active-finalizing")
				writeInventoryControl(t, root, identity, retirement.PhaseFinalizing)
			},
			want: "active journal control must remain in prepared phase",
		},
		{
			name: "active with cross-paired control",
			setup: func(t *testing.T, root string) {
				captureInventoryJournal(t, root, "active-cross-paired")
				other := inventoryTestIdentity(t, "other-cross-paired", "e")
				writeInventoryControl(t, root, other, retirement.PhasePrepared)
			},
			want: "active journal and retirement control identities do not match",
		},
		{
			name: "prepared control without residue",
			setup: func(t *testing.T, root string) {
				identity := inventoryTestIdentity(t, "prepared-without-residue", "f")
				writeInventoryControl(t, root, identity, retirement.PhasePrepared)
			},
			want: "prepared retirement control requires its journal residue",
		},
		{
			name: "residue without control",
			setup: func(t *testing.T, root string) {
				identity := inventoryTestIdentity(t, "residue-without-control", "1")
				mkdirPrivate(t, filepath.Join(root, identity.ResidueName()))
			},
			want: "unsupported state",
		},
		{
			name: "active with matching GC",
			setup: func(t *testing.T, root string) {
				identity, _ := captureInventoryJournal(t, root, "active-with-gc")
				mkdirPrivate(t, filepath.Join(root, identity.GCName()))
			},
			want: "also has finalized GC",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			test.setup(t, recoveryRoot)
			assertRecoveryInventoryBlocked(t, recoveryRoot, test.want)
		})
	}
}

func TestRecoveryRootInventoryBlocksReservedSymlinks(t *testing.T) {
	identity := inventoryTestIdentity(t, "reserved-symlink", "2")
	tests := []struct {
		name string
		path func(retirement.Identity) string
		want string
	}{
		{name: "control", path: retirement.Identity.ControlName, want: "must be a no-follow directory"},
		{name: "residue", path: retirement.Identity.ResidueName, want: "must be a no-follow directory"},
		{name: "GC", path: retirement.Identity.GCName, want: "must be a no-follow directory"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			mkdirPrivate(t, recoveryRoot)
			if err := os.Symlink("missing", filepath.Join(recoveryRoot, test.path(identity))); err != nil {
				t.Fatalf("create reserved symlink: %v", err)
			}
			assertRecoveryInventoryBlocked(t, recoveryRoot, test.want)
		})
	}
}

func TestRecoveryRootInventoryBlocksJournalPayloadViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, CaptureResult)
		want   string
	}{
		{
			name: "truncated",
			mutate: func(t *testing.T, result CaptureResult) {
				writePrivateFile(t, result.JournalPath, []byte("{"))
			},
			want: "EOF",
		},
		{
			name: "oversized",
			mutate: func(t *testing.T, result CaptureResult) {
				if err := os.Truncate(result.JournalPath, maximumRecoveryJournalBytes+1); err != nil {
					t.Fatalf("enlarge recovery journal: %v", err)
				}
			},
			want: "invalid file metadata",
		},
		{
			name: "wrong mode",
			mutate: func(t *testing.T, result CaptureResult) {
				if err := os.Chmod(result.JournalPath, 0o640); err != nil {
					t.Fatalf("change recovery journal mode: %v", err)
				}
			},
			want: "invalid file metadata",
		},
		{
			name: "operation id drift",
			mutate: func(t *testing.T, result CaptureResult) {
				content, err := os.ReadFile(result.JournalPath)
				if err != nil {
					t.Fatalf("read recovery journal: %v", err)
				}
				operationID := filepath.Base(result.Directory)
				replacement := strings.Repeat("x", len(operationID))
				changed := strings.Replace(string(content), operationID, replacement, 1)
				if changed == string(content) {
					t.Fatal("recovery journal did not contain its operation id")
				}
				writePrivateFile(t, result.JournalPath, []byte(changed))
			},
			want: "does not match directory",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			operationID := "journal-payload-" + strings.ReplaceAll(test.name, " ", "-")
			_, result := captureInventoryJournal(t, recoveryRoot, operationID)
			test.mutate(t, result)
			assertRecoveryInventoryBlocked(t, recoveryRoot, test.want)
		})
	}
}

func TestRecoveryRootInventoryBlocksControlRecordViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		want   string
	}{
		{
			name: "truncated record",
			mutate: func(t *testing.T, control string) {
				writePrivateFile(t, filepath.Join(control, retirement.RecordFileName), []byte("{"))
			},
			want: "EOF",
		},
		{
			name: "oversized record",
			mutate: func(t *testing.T, control string) {
				recordPath := filepath.Join(control, retirement.RecordFileName)
				if err := os.Truncate(recordPath, retirement.MaximumRecordBytes+1); err != nil {
					t.Fatalf("enlarge retirement record: %v", err)
				}
			},
			want: "exceeds",
		},
		{
			name: "wrong record mode",
			mutate: func(t *testing.T, control string) {
				if err := os.Chmod(filepath.Join(control, retirement.RecordFileName), 0o640); err != nil {
					t.Fatalf("change retirement record mode: %v", err)
				}
			},
			want: "permissions",
		},
		{
			name: "record symlink",
			mutate: func(t *testing.T, control string) {
				recordPath := filepath.Join(control, retirement.RecordFileName)
				if err := os.Remove(recordPath); err != nil {
					t.Fatalf("remove retirement record: %v", err)
				}
				if err := os.Symlink("missing", recordPath); err != nil {
					t.Fatalf("replace retirement record with symlink: %v", err)
				}
			},
			want: "must be a no-follow regular file",
		},
		{
			name: "temporary symlink",
			mutate: func(t *testing.T, control string) {
				if err := os.Symlink("missing", filepath.Join(control, ".daem-tmp-link")); err != nil {
					t.Fatalf("create retirement temporary symlink: %v", err)
				}
			},
			want: "must be a no-follow regular file",
		},
		{
			name: "temporary directory",
			mutate: func(t *testing.T, control string) {
				mkdirPrivate(t, filepath.Join(control, ".daem-tmp-directory"))
			},
			want: "must be a no-follow regular file",
		},
		{
			name: "oversized temporary",
			mutate: func(t *testing.T, control string) {
				temporary := filepath.Join(control, ".daem-tmp-oversized")
				writePrivateFile(t, temporary, []byte("temporary"))
				if err := os.Truncate(temporary, retirement.MaximumRecordBytes+1); err != nil {
					t.Fatalf("enlarge retirement temporary: %v", err)
				}
			},
			want: "exceeds",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			operationID := "control-record-" + strings.ReplaceAll(test.name, " ", "-")
			identity := inventoryTestIdentity(t, operationID, "3")
			control := writeInventoryControl(t, recoveryRoot, identity, retirement.PhaseFinalizing)
			test.mutate(t, control)
			assertRecoveryInventoryBlocked(t, recoveryRoot, test.want)
		})
	}
}

func TestRecoveryRootInventoryBlocksUnownedReservedEntryEvidence(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	identity := inventoryTestIdentity(t, "unowned-gc", "4")
	mkdirPrivate(t, filepath.Join(recoveryRoot, identity.GCName()))
	filesystem := &unownedEntryFilesystem{
		Store: journalTestFilesystem(),
		name:  identity.GCName(),
	}

	inventory, err := loadRecoveryRootInventory(t.Context(), recoveryRoot, inventoryOptions{
		Filesystem: filesystem,
		StateCodec: testStateCodec(),
	})
	if err != nil {
		t.Fatalf("loadRecoveryRootInventory: %v", err)
	}
	if !inventory.decision.Blocked() ||
		!strings.Contains(inventory.decision.Detail(), "not owned by the invoking user") {
		t.Fatalf("decision = %#v, want unowned-entry blocker", inventory.decision)
	}
}

func TestRecoveryPlanLoadersSelectOneAuthorityKind(t *testing.T) {
	t.Run("active excludes cleanup", func(t *testing.T) {
		recoveryRoot := filepath.Join(t.TempDir(), "recovery")
		captureInventoryJournal(t, recoveryRoot, "active-plan-selection")

		_, err := LoadCleanupPlanWithOptions(
			t.Context(),
			Paths{RecoveryDir: recoveryRoot},
			PlanLoadOptions{
				Filesystem: journalTestFilesystem(),
				StateCodec: testStateCodec(),
			},
		)
		if err == nil || !strings.Contains(err.Error(), "no journal cleanup plan") {
			t.Fatalf("LoadCleanupPlanWithOptions error = %v, want no cleanup authority", err)
		}
	})

	t.Run("retained excludes active", func(t *testing.T) {
		recoveryRoot := filepath.Join(t.TempDir(), "recovery")
		identity, result := captureInventoryJournal(t, recoveryRoot, "cleanup-plan-selection")
		writeInventoryControl(t, recoveryRoot, identity, retirement.PhasePrepared)
		renameInventoryJournalToResidue(t, result, identity)

		_, err := LoadActivePlanWithOptions(
			t.Context(),
			Paths{RecoveryDir: recoveryRoot},
			PlanLoadOptions{
				Filesystem: journalTestFilesystem(),
				StateCodec: testStateCodec(),
			},
		)
		if err == nil || !strings.Contains(err.Error(), "no active recovery journal") {
			t.Fatalf("LoadActivePlanWithOptions error = %v, want no active authority", err)
		}
	})
}

func TestRecoveryRootInventoryPropagatesCapabilityAndCancellationErrors(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	captureInventoryJournal(t, recoveryRoot, "inventory-operational-errors")

	_, err := loadRecoveryRootInventory(t.Context(), recoveryRoot, inventoryOptions{
		Filesystem: journalTestFilesystem(),
	})
	if !errors.Is(err, errRecoveryJournalStateCodecRequired) {
		t.Fatalf("missing-codec error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = loadRecoveryRootInventory(ctx, recoveryRoot, inventoryOptions{
		Filesystem: journalTestFilesystem(),
		StateCodec: testStateCodec(),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled inventory error = %v", err)
	}
}

type unownedEntryFilesystem struct {
	mutationfs.Store
	name string
}

func (filesystem *unownedEntryFilesystem) SnapshotDirectory(
	ctx context.Context,
	path string,
) (mutationfs.DirectorySnapshot, error) {
	snapshot, err := filesystem.Store.SnapshotDirectory(ctx, path)
	if err != nil {
		return mutationfs.DirectorySnapshot{}, err
	}
	entries := snapshot.Entries()
	for index, entry := range entries {
		if entry.Name() != filesystem.name {
			continue
		}
		entries[index], err = mutationfs.NewDirectoryEntrySnapshot(
			entry.Name(),
			entry.Identity(),
			entry.Mode(),
			false,
			entry.Size(),
		)
		if err != nil {
			return mutationfs.DirectorySnapshot{}, err
		}
	}
	return mutationfs.NewDirectorySnapshot(
		snapshot.RootIdentity(),
		snapshot.RootMode(),
		snapshot.RootOwnedByInvoker(),
		entries,
	)
}

func inventoryTestIdentity(
	t *testing.T,
	operationID string,
	digestCharacter string,
) retirement.Identity {
	t.Helper()
	identity, err := retirement.NewIdentity(
		operationID,
		"sha256:"+strings.Repeat(digestCharacter, 64),
	)
	if err != nil {
		t.Fatalf("retirement.NewIdentity: %v", err)
	}
	return identity
}

func assertRecoveryInventoryBlocked(t *testing.T, recoveryRoot string, want string) {
	t.Helper()
	inventory, err := loadRecoveryRootInventory(t.Context(), recoveryRoot, inventoryOptions{
		Filesystem: journalTestFilesystem(),
		StateCodec: testStateCodec(),
	})
	if err != nil {
		t.Fatalf("loadRecoveryRootInventory: %v", err)
	}
	if !inventory.decision.Blocked() ||
		!strings.Contains(inventory.decision.Detail(), want) {
		t.Fatalf(
			"decision = state %q blocked=%t detail=%q, want %q",
			inventory.decision.State(),
			inventory.decision.Blocked(),
			inventory.decision.Detail(),
			want,
		)
	}
}
