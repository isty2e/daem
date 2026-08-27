//go:build darwin || linux

package transaction

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStateDirAuthorityDoesNotAcceptExternalFirstAppearance(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), ".daem")
	authority, err := CaptureStateDirAuthority(t.Context(), stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err = authority.EnsureOwnedIncarnation(t.Context())
	if !errors.Is(err, ErrStateDirAppeared) {
		t.Fatalf("EnsureOwnedIncarnation error = %v, want ErrStateDirAppeared", err)
	}
	if validateErr := authority.Validate(t.Context()); !errors.Is(validateErr, ErrStateDirAppeared) {
		t.Fatalf("Validate error = %v, want ErrStateDirAppeared", validateErr)
	}
}

type stateDirRecordingBudget struct {
	limit         int
	used          int
	physicalCalls int
	entries       int
	bytes         int64
}

func (budget *stateDirRecordingBudget) AdmitPathComponents(count int) error {
	if budget.used+count > budget.limit {
		return fmt.Errorf("injected StateDir path budget exhausted")
	}
	budget.used += count
	return nil
}

func (budget *stateDirRecordingBudget) AdmitPhysicalWork(
	pathComponents int,
	entries int,
	bytes int64,
) error {
	if err := budget.AdmitPathComponents(pathComponents); err != nil {
		return err
	}
	budget.physicalCalls++
	budget.entries += entries
	budget.bytes += bytes
	return nil
}

func TestStateDirAuthorityCreatesAndBindsOwnedIncarnation(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), ".daem")
	authority, err := CaptureStateDirAuthority(t.Context(), stateDir)
	if err != nil {
		t.Fatal(err)
	}
	created, err := authority.EnsureOwnedIncarnation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("EnsureOwnedIncarnation did not report the created directory")
	}
	if err := authority.Validate(t.Context()); err != nil {
		t.Fatalf("Validate created StateDir: %v", err)
	}
}

func TestStateDirRequireClearChargesCompleteResidueCensusWork(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), ".daem")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	residueName := fileSetTemporaryPrefix + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := os.Mkdir(filepath.Join(stateDir, residueName), 0o700); err != nil {
		t.Fatal(err)
	}
	budget := &stateDirRecordingBudget{limit: 1 << 20}
	authority, err := CaptureStateDirAuthorityBounded(t.Context(), stateDir, 256, budget)
	if err != nil {
		t.Fatal(err)
	}
	beforePathWork := budget.used
	err = authority.RequireClear(t.Context())
	if !errors.Is(err, ErrAbandonedFileSetResidue) {
		t.Fatalf("RequireClear error = %v, want abandoned residue", err)
	}
	if budget.physicalCalls == 0 || budget.used <= beforePathWork ||
		budget.entries < 4 || budget.bytes < int64(len("state.json")+len(residueName)) {
		t.Fatalf(
			"census work calls=%d paths=%d entries=%d bytes=%d, want charged path, entries, names, and observations",
			budget.physicalCalls,
			budget.used-beforePathWork,
			budget.entries,
			budget.bytes,
		)
	}
}

func TestCaptureStateDirAuthorityBoundedRejectsPathWorkBeforeObservation(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "one", "two", "three", ".daem")
	budget := &stateDirRecordingBudget{limit: 1}
	_, err := CaptureStateDirAuthorityBounded(t.Context(), stateDir, 256, budget)
	if err == nil || !errors.Is(err, ErrFileSetFenceUnprovable) {
		t.Fatalf("CaptureStateDirAuthorityBounded error = %v, want bounded access refusal", err)
	}
	if budget.used > budget.limit {
		t.Fatalf("path budget used = %d, limit = %d", budget.used, budget.limit)
	}
}
