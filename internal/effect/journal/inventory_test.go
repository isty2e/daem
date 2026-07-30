package journal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/output"
)

func TestRecoveryRootInventoryClassifiesClosedPhysicalStates(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*testing.T, string) retirement.Identity
		want        retirement.State
		wantCleanup bool
		ready       bool
	}{
		{
			name:  "clean absent root",
			setup: func(*testing.T, string) retirement.Identity { return retirement.Identity{} },
			want:  retirement.StateClean,
			ready: true,
		},
		{
			name: "clean unrelated hidden",
			setup: func(t *testing.T, root string) retirement.Identity {
				t.Helper()
				mkdirPrivate(t, root)
				if err := os.Symlink("missing", filepath.Join(root, ".unrelated")); err != nil {
					t.Fatalf("create unrelated hidden symlink: %v", err)
				}
				return retirement.Identity{}
			},
			want:  retirement.StateClean,
			ready: true,
		},
		{
			name: "active",
			setup: func(t *testing.T, root string) retirement.Identity {
				t.Helper()
				identity, _ := captureInventoryJournal(t, root, "inventory-active")
				return identity
			},
			want: retirement.StateActive,
		},
		{
			name: "prepared active",
			setup: func(t *testing.T, root string) retirement.Identity {
				t.Helper()
				identity, _ := captureInventoryJournal(t, root, "inventory-prepared")
				writeInventoryControl(t, root, identity, retirement.PhasePrepared)
				return identity
			},
			want: retirement.StatePrepared,
		},
		{
			name: "retained",
			setup: func(t *testing.T, root string) retirement.Identity {
				t.Helper()
				identity, result := captureInventoryJournal(t, root, "inventory-retained")
				writeInventoryControl(t, root, identity, retirement.PhasePrepared)
				renameInventoryJournalToResidue(t, result, identity)
				return identity
			},
			want:        retirement.StateRetained,
			wantCleanup: true,
		},
		{
			name: "finalizing partial residue",
			setup: func(t *testing.T, root string) retirement.Identity {
				t.Helper()
				identity, result := captureInventoryJournal(t, root, "inventory-finalizing")
				writeInventoryControl(t, root, identity, retirement.PhaseFinalizing)
				residue := renameInventoryJournalToResidue(t, result, identity)
				if err := os.Remove(filepath.Join(residue, recoveryJournalFileName)); err != nil {
					t.Fatalf("remove residue journal payload: %v", err)
				}
				return identity
			},
			want:        retirement.StateFinalizing,
			wantCleanup: true,
		},
		{
			name: "finalizing absent residue",
			setup: func(t *testing.T, root string) retirement.Identity {
				t.Helper()
				identity, result := captureInventoryJournal(t, root, "inventory-finalizing-absent")
				writeInventoryControl(t, root, identity, retirement.PhaseFinalizing)
				if err := os.RemoveAll(result.Directory); err != nil {
					t.Fatalf("remove active journal: %v", err)
				}
				return identity
			},
			want:        retirement.StateFinalizing,
			wantCleanup: true,
		},
		{
			name: "finalized GC",
			setup: func(t *testing.T, root string) retirement.Identity {
				t.Helper()
				identity, result := captureInventoryJournal(t, root, "inventory-finalized")
				if err := os.RemoveAll(result.Directory); err != nil {
					t.Fatalf("remove active journal: %v", err)
				}
				mkdirPrivate(t, filepath.Join(root, identity.GCName()))
				return identity
			},
			want:  retirement.StateFinalized,
			ready: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			identity := test.setup(t, recoveryRoot)
			options := inventoryOptions{
				Filesystem: journalTestFilesystem(),
				StateCodec: testStateCodec(),
			}
			inventory, err := loadRecoveryRootInventory(t.Context(), recoveryRoot, options)
			if err != nil {
				t.Fatalf("loadRecoveryRootInventory: %v", err)
			}
			if inventory.decision.Blocked() || inventory.decision.State() != test.want {
				t.Fatalf(
					"decision = state %q blocked=%t detail=%q, want %q",
					inventory.decision.State(),
					inventory.decision.Blocked(),
					inventory.decision.Detail(),
					test.want,
				)
			}
			_, hasCleanup := inventory.decision.CleanupPlan()
			if hasCleanup != test.wantCleanup {
				t.Fatalf("cleanup plan present = %t, want %t", hasCleanup, test.wantCleanup)
			}
			if test.want == retirement.StateActive || test.want == retirement.StatePrepared {
				if inventory.active == nil ||
					!inventory.active.identity.Equal(identity) {
					t.Fatalf("active evidence = %#v, want identity %#v", inventory.active, identity)
				}
			} else if inventory.active != nil {
				t.Fatalf("state %q retained active authority %#v", test.want, inventory.active)
			}

			readinessErr := ensureNoActive(t.Context(), recoveryRoot, options)
			if (readinessErr == nil) != test.ready {
				t.Fatalf("EnsureNoActive error = %v, ready want %t", readinessErr, test.ready)
			}
			if !test.ready && !strings.Contains(readinessErr.Error(), "daem recover --dry-run") {
				t.Fatalf("EnsureNoActive error = %v, want recovery remediation", readinessErr)
			}

			if !test.wantCleanup {
				return
			}
			cleanup, err := LoadCleanupPlanWithOptions(t.Context(), Paths{
				RecoveryDir: recoveryRoot,
			}, PlanLoadOptions{
				Filesystem: journalTestFilesystem(),
				StateCodec: testStateCodec(),
			})
			if err != nil {
				t.Fatalf("LoadCleanupPlanWithOptions: %v", err)
			}
			if cleanup.Authority().OperationID() != identity.OperationID() ||
				cleanup.Authority().JournalAuthorityFingerprint() !=
					identity.JournalAuthorityFingerprint() {
				t.Fatalf("cleanup authority = %#v, want %#v", cleanup.Authority(), identity)
			}
		})
	}
}

func TestRecoveryRootInventoryBlocksMalformedAndCrossPairedEvidence(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*testing.T, string)
		want      string
		allowLoad bool
	}{
		{
			name: "legacy tombstone",
			setup: func(t *testing.T, root string) {
				mkdirPrivate(t, filepath.Join(root, ".daem-tombstone-legacy"))
			},
			want: "manual remediation",
		},
		{
			name: "foreign visible file",
			setup: func(t *testing.T, root string) {
				mkdirPrivate(t, root)
				writePrivateFile(t, filepath.Join(root, "foreign"), []byte("not a journal"))
			},
			want: "must be a no-follow directory",
		},
		{
			name: "control missing record",
			setup: func(t *testing.T, root string) {
				identity, _ := captureInventoryJournal(t, root, "missing-control-record")
				if err := os.RemoveAll(filepath.Join(root, identity.OperationID())); err != nil {
					t.Fatalf("remove active journal: %v", err)
				}
				mkdirPrivate(t, filepath.Join(root, identity.ControlName()))
			},
			want: "exactly one retirement.json",
		},
		{
			name: "control unknown child",
			setup: func(t *testing.T, root string) {
				identity, result := captureInventoryJournal(t, root, "unknown-control-child")
				if err := os.RemoveAll(result.Directory); err != nil {
					t.Fatalf("remove active journal: %v", err)
				}
				control := writeInventoryControl(t, root, identity, retirement.PhaseFinalizing)
				writePrivateFile(t, filepath.Join(control, "unknown"), []byte("x"))
			},
			want: "unexpected child",
		},
		{
			name: "prepared residue missing journal",
			setup: func(t *testing.T, root string) {
				identity, result := captureInventoryJournal(t, root, "missing-residue-journal")
				writeInventoryControl(t, root, identity, retirement.PhasePrepared)
				residue := renameInventoryJournalToResidue(t, result, identity)
				if err := os.Remove(filepath.Join(residue, recoveryJournalFileName)); err != nil {
					t.Fatalf("remove residue journal: %v", err)
				}
			},
			want: "has no journal.json",
		},
		{
			name: "prepared cross-paired residue",
			setup: func(t *testing.T, root string) {
				first, firstResult := captureInventoryJournal(t, root, "cross-pair-first")
				if err := os.RemoveAll(firstResult.Directory); err != nil {
					t.Fatalf("remove first active journal: %v", err)
				}
				second, secondResult := captureInventoryJournal(t, root, "cross-pair-second")
				writeInventoryControl(t, root, first, retirement.PhasePrepared)
				renameInventoryJournalToResidue(t, secondResult, second)
			},
			want: "does not match its control",
		},
		{
			name: "prepared residue journal drift",
			setup: func(t *testing.T, root string) {
				identity, result := captureInventoryJournal(t, root, "residue-drift")
				writeInventoryControl(t, root, identity, retirement.PhasePrepared)
				residue := renameInventoryJournalToResidue(t, result, identity)
				journalPath := filepath.Join(residue, recoveryJournalFileName)
				content, err := os.ReadFile(journalPath)
				if err != nil {
					t.Fatalf("read residue journal: %v", err)
				}
				changed := strings.Replace(
					string(content),
					"2026-07-30T12:00:00Z",
					"2026-07-30T12:00:01Z",
					1,
				)
				if changed == string(content) {
					t.Fatal("journal fixture did not contain expected created_at")
				}
				writePrivateFile(t, journalPath, []byte(changed))
			},
			want: "does not match its loaded journal identity",
		},
		{
			name: "matching GC and finalizing control",
			setup: func(t *testing.T, root string) {
				identity, result := captureInventoryJournal(t, root, "control-with-gc")
				if err := os.RemoveAll(result.Directory); err != nil {
					t.Fatalf("remove active journal: %v", err)
				}
				writeInventoryControl(t, root, identity, retirement.PhaseFinalizing)
				mkdirPrivate(t, filepath.Join(root, identity.GCName()))
			},
			want: "also has finalized GC",
		},
		{
			name: "valid interrupted record temporary",
			setup: func(t *testing.T, root string) {
				identity, result := captureInventoryJournal(t, root, "valid-record-temporary")
				if err := os.RemoveAll(result.Directory); err != nil {
					t.Fatalf("remove active journal: %v", err)
				}
				control := writeInventoryControl(t, root, identity, retirement.PhaseFinalizing)
				writePrivateFile(t, filepath.Join(control, ".daem-tmp-interrupted"), []byte("partial"))
			},
			allowLoad: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			test.setup(t, recoveryRoot)
			inventory, err := loadRecoveryRootInventory(t.Context(), recoveryRoot, inventoryOptions{
				Filesystem: journalTestFilesystem(),
				StateCodec: testStateCodec(),
			})
			if err != nil {
				t.Fatalf("loadRecoveryRootInventory: %v", err)
			}
			if test.allowLoad {
				if inventory.decision.Blocked() ||
					inventory.decision.State() != retirement.StateFinalizing {
					t.Fatalf("valid temporary decision = %#v", inventory.decision)
				}
				return
			}
			if !inventory.decision.Blocked() ||
				!strings.Contains(inventory.decision.Detail(), test.want) {
				t.Fatalf(
					"decision = blocked=%t detail=%q, want %q",
					inventory.decision.Blocked(),
					inventory.decision.Detail(),
					test.want,
				)
			}
			if _, err := LoadCleanupPlanWithOptions(
				t.Context(),
				Paths{RecoveryDir: recoveryRoot},
				PlanLoadOptions{
					Filesystem: journalTestFilesystem(),
					StateCodec: testStateCodec(),
				},
			); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadCleanupPlanWithOptions error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRecoveryRootInventoryRejectsJournalIdentityReplacement(t *testing.T) {
	recoveryRoot := filepath.Join(t.TempDir(), "recovery")
	_, result := captureInventoryJournal(t, recoveryRoot, "identity-replacement")
	var replacementErr error
	filesystem := replacingAfterReadFilesystem{
		Store: journalTestFilesystem(),
		replace: func() {
			temporary := result.JournalPath + ".replacement"
			replacementErr = os.WriteFile(temporary, []byte(`{"invalid":true}`), 0o600)
			if replacementErr == nil {
				replacementErr = os.Rename(temporary, result.JournalPath)
			}
		},
	}

	_, err := loadRecoveryRootInventory(t.Context(), recoveryRoot, inventoryOptions{
		Filesystem: &filesystem,
		StateCodec: testStateCodec(),
	})
	if replacementErr != nil {
		t.Fatalf("replace journal during inventory: %v", replacementErr)
	}
	if err == nil || !strings.Contains(err.Error(), "changed while inventorying") {
		t.Fatalf("loadRecoveryRootInventory error = %v, want concurrent-change rejection", err)
	}
}

type replacingAfterReadFilesystem struct {
	mutationfs.Store
	once    sync.Once
	replace func()
}

func (filesystem *replacingAfterReadFilesystem) ReadRegularFileSnapshotUpTo(
	ctx context.Context,
	path string,
	maximumBytes int64,
) (mutationfs.RegularFileSnapshot, error) {
	snapshot, err := filesystem.Store.ReadRegularFileSnapshotUpTo(ctx, path, maximumBytes)
	if err == nil {
		filesystem.once.Do(filesystem.replace)
	}
	return snapshot, err
}

func captureInventoryJournal(
	t *testing.T,
	recoveryRoot string,
	operationID string,
) (retirement.Identity, CaptureResult) {
	t.Helper()

	result, err := CaptureJournalWithOptions(
		t.Context(),
		Paths{RecoveryDir: recoveryRoot},
		operationID,
		time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC),
		beforeStatefile(),
		afterStatefile(),
		CaptureOptions{
			Filesystem: journalTestFilesystem(),
			Resolver: func(output.Destination) (string, error) {
				return "", nil
			},
			StateCodec: testStateCodec(),
		},
	)
	if err != nil {
		t.Fatalf("CaptureJournalWithOptions: %v", err)
	}
	loaded, err := loadRecoveryJournal(
		t.Context(),
		journalTestFilesystem(),
		result.JournalPath,
		testStateCodec(),
	)
	if err != nil {
		t.Fatalf("loadRecoveryJournal: %v", err)
	}
	fingerprint, err := recoveryJournalAuthorityFingerprint(loaded, testStateCodec())
	if err != nil {
		t.Fatalf("recoveryJournalAuthorityFingerprint: %v", err)
	}
	identity, err := retirement.NewIdentity(operationID, fingerprint)
	if err != nil {
		t.Fatalf("retirement.NewIdentity: %v", err)
	}
	return identity, result
}

func writeInventoryControl(
	t *testing.T,
	recoveryRoot string,
	identity retirement.Identity,
	phase retirement.Phase,
) string {
	t.Helper()

	record, err := retirement.NewRecord(
		identity.OperationID(),
		identity.JournalAuthorityFingerprint(),
		phase,
	)
	if err != nil {
		t.Fatalf("retirement.NewRecord: %v", err)
	}
	content, err := retirement.Encode(record)
	if err != nil {
		t.Fatalf("retirement.Encode: %v", err)
	}
	control := filepath.Join(recoveryRoot, identity.ControlName())
	mkdirPrivate(t, control)
	writePrivateFile(t, filepath.Join(control, retirement.RecordFileName), content)
	return control
}

func renameInventoryJournalToResidue(
	t *testing.T,
	result CaptureResult,
	identity retirement.Identity,
) string {
	t.Helper()

	residue := filepath.Join(filepath.Dir(result.Directory), identity.ResidueName())
	if err := os.Rename(result.Directory, residue); err != nil {
		t.Fatalf("rename active journal to residue: %v", err)
	}
	return residue
}

func mkdirPrivate(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, retirement.DirectoryMode); err != nil {
		t.Fatalf("mkdir private %q: %v", path, err)
	}
	if err := os.Chmod(path, retirement.DirectoryMode); err != nil {
		t.Fatalf("chmod private %q: %v", path, err)
	}
}

func writePrivateFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, retirement.RecordMode); err != nil {
		t.Fatalf("write private file %q: %v", path, err)
	}
	if err := os.Chmod(path, retirement.RecordMode); err != nil {
		t.Fatalf("chmod private file %q: %v", path, err)
	}
}
