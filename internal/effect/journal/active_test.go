package journal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if err := EnsureNoActive(recoveryRoot); err == nil {
		t.Fatal("EnsureNoActive ignored corrupt active evidence")
	}
}

func TestActiveRecoveryOperationsIgnorePrivateResidue(t *testing.T) {
	recoveryRoot := t.TempDir()
	for _, name := range []string{".daem-tombstone-residue", ".private-build-residue"} {
		if err := os.Mkdir(filepath.Join(recoveryRoot, name), 0o700); err != nil {
			t.Fatalf("Mkdir(%q) returned error: %v", name, err)
		}
	}
	operationID := "20260711T120000.000000000Z-apply"
	if err := os.Mkdir(filepath.Join(recoveryRoot, operationID), 0o700); err != nil {
		t.Fatalf("Mkdir active operation returned error: %v", err)
	}

	operations, err := activeRecoveryOperations(recoveryRoot)
	if err != nil {
		t.Fatalf("activeRecoveryOperations returned error: %v", err)
	}
	if len(operations) != 1 || operations[0] != operationID {
		t.Fatalf("operations = %v, want only %q", operations, operationID)
	}
}
