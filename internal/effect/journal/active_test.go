package journal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/output"
)

func TestLoadActivePlanRetainsCorruptJournalEvidence(t *testing.T) {
	root := t.TempDir()
	recoveryRoot := filepath.Join(root, "recovery")
	operationID := "20260711T120000.000000000Z-apply"
	operationDir := filepath.Join(recoveryRoot, operationID)
	if err := os.MkdirAll(operationDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	journalPath := filepath.Join(operationDir, recoveryJournalFileName)
	if err := os.WriteFile(journalPath, []byte(`{"version":7,"unexpected":true}`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err := LoadActivePlanWithOptions(context.Background(), Paths{
		RecoveryDir:   recoveryRoot,
		StatefilePath: filepath.Join(root, "state.json"),
		ManifestRoot:  root,
	}, PlanLoadOptions{Filesystem: journalTestFilesystem()})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("LoadActivePlan error = %v, want unknown-field rejection", err)
	}
	if _, statErr := os.Stat(operationDir); statErr != nil {
		t.Fatalf("corrupt recovery evidence was not retained: %v", statErr)
	}
	if err := ensureNoActive(t.Context(), recoveryRoot, inventoryOptions{
		Filesystem: journalTestFilesystem(),
		StateCodec: testStateCodec(),
	}); err == nil {
		t.Fatal("EnsureNoActive ignored corrupt active evidence")
	}
}

func TestRecoveryInventoryIgnoresUnrelatedHiddenEntries(t *testing.T) {
	recoveryRoot := t.TempDir()
	for _, name := range []string{".private-build-residue", ".foreign-private-file"} {
		if err := os.Mkdir(filepath.Join(recoveryRoot, name), 0o700); err != nil {
			t.Fatalf("Mkdir(%q) returned error: %v", name, err)
		}
	}
	operationID := "20260711T120000.000000000Z-apply"
	if _, err := CaptureJournalWithOptions(
		t.Context(),
		Paths{RecoveryDir: recoveryRoot},
		operationID,
		time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC),
		beforeStatefile(),
		afterStatefile(),
		CaptureOptions{
			Filesystem: journalTestFilesystem(),
			Resolver: func(output.Destination) (string, error) {
				return "", nil
			},
			StateCodec: testStateCodec(),
		},
	); err != nil {
		t.Fatalf("CaptureJournalWithOptions: %v", err)
	}

	inventory, err := loadRecoveryRootInventory(t.Context(), recoveryRoot, inventoryOptions{
		Filesystem: journalTestFilesystem(),
		StateCodec: testStateCodec(),
	})
	if err != nil {
		t.Fatalf("loadRecoveryRootInventory returned error: %v", err)
	}
	if inventory.decision.State() != retirement.StateActive ||
		inventory.active == nil ||
		inventory.active.identity.OperationID() != operationID {
		t.Fatalf("inventory = %#v, want active %q", inventory, operationID)
	}
}

func TestSafeRecoveryOperationIDRejectsRetirementNamespaces(t *testing.T) {
	for _, operationID := range []string{
		"retirement-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"retirement-future",
		".daem-journal-residue-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		".daem-journal-gc-v1-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		".daem-tombstone-legacy",
	} {
		if isSafeRecoveryOperationID(operationID) {
			t.Fatalf("isSafeRecoveryOperationID(%q) = true", operationID)
		}
	}
	if !isSafeRecoveryOperationID("20260730T120000.000000000Z-apply") {
		t.Fatal("ordinary generated operation id was rejected")
	}
}

func TestRecoveryInventoryFailsClosedOnMalformedRetirementControl(t *testing.T) {
	recoveryRoot := t.TempDir()
	controlName := "retirement-v1-" + strings.Repeat("a", 64)
	if err := os.Mkdir(filepath.Join(recoveryRoot, controlName), 0o700); err != nil {
		t.Fatalf("Mkdir retirement control returned error: %v", err)
	}

	inventory, err := loadRecoveryRootInventory(t.Context(), recoveryRoot, inventoryOptions{
		Filesystem: journalTestFilesystem(),
		StateCodec: testStateCodec(),
	})
	if err != nil {
		t.Fatalf("loadRecoveryRootInventory returned error: %v", err)
	}
	if !inventory.decision.Blocked() ||
		!strings.Contains(inventory.decision.Detail(), controlName) {
		t.Fatalf("inventory = %#v, want fail-closed control diagnostic", inventory)
	}
}
