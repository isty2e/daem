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
	limit int
	used  int
}

func (budget *stateDirRecordingBudget) AdmitPathComponents(count int) error {
	if budget.used+count > budget.limit {
		return fmt.Errorf("injected StateDir path budget exhausted")
	}
	budget.used += count
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
