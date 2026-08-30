//go:build darwin || linux

package recoverygate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/declaration/transaction"
)

type captureStateDirRecordingBudget struct {
	limit int
	used  int
}

func (budget *captureStateDirRecordingBudget) AdmitPathComponents(count int) error {
	if budget.used+count > budget.limit {
		return fmt.Errorf("injected StateDir path budget exhausted")
	}
	budget.used += count
	return nil
}

func (budget *captureStateDirRecordingBudget) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	return budget.AdmitPathComponents(pathComponents)
}

func TestCaptureStateDirBoundedRejectsPathWorkBeforeObservation(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), "one", "two", "three", ".daem")
	budget := &captureStateDirRecordingBudget{limit: 1}
	_, err := CaptureStateDirBounded(t.Context(), stateDir, 256, budget)
	if err == nil || !errors.Is(err, transaction.ErrFileSetFenceUnprovable) {
		t.Fatalf("CaptureStateDirBounded error = %v, want bounded access refusal", err)
	}
	if budget.used > budget.limit {
		t.Fatalf("path budget used = %d, limit = %d", budget.used, budget.limit)
	}
}

func TestCaptureStateDirDoesNotInspectRecoveryDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "recovery"), []byte("not-a-recovery-directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	authority, err := CaptureStateDir(t.Context(), stateDir)
	if err != nil {
		t.Fatalf("CaptureStateDir error = %v, want success without journal inspection", err)
	}
	if err := authority.RequireClear(t.Context()); err != nil {
		t.Fatalf("RequireClear error = %v, want clear file-set fence", err)
	}
}
