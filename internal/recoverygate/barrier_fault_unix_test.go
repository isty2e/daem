//go:build darwin || linux

package recoverygate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestFirstIncarnationFaultMatrix(t *testing.T) {
	t.Run("cancel before create leaves StateDir absent and retry succeeds", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), ".daem")
		authority, err := CaptureStateDir(t.Context(), stateDir)
		if err != nil {
			t.Fatal(err)
		}
		created, err := authority.ensureOwnedIncarnationWithFaults(
			t.Context(),
			authority.state.physicalWorkBudget,
			barrierFaultPlan{failures: map[barrierPhase]error{
				barrierPhasePreCreate: context.Canceled,
			}},
		)
		if created || !errors.Is(err, context.Canceled) {
			t.Fatalf("pre-create fault = (%t, %v), want (false, canceled)", created, err)
		}
		assertStateDirAbsent(t, stateDir)

		created, err = authority.EnsureOwnedIncarnation(t.Context())
		if err != nil || !created {
			t.Fatalf("retry EnsureOwnedIncarnation = (%t, %v), want created", created, err)
		}
		if err := authority.Validate(t.Context()); err != nil {
			t.Fatalf("Validate after retry: %v", err)
		}
	})

	t.Run("cancel after create rolls back and retry succeeds", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), ".daem")
		authority, err := CaptureStateDir(t.Context(), stateDir)
		if err != nil {
			t.Fatal(err)
		}
		created, err := authority.ensureOwnedIncarnationWithFaults(
			t.Context(),
			authority.state.physicalWorkBudget,
			barrierFaultPlan{failures: map[barrierPhase]error{
				barrierPhasePostCreate: context.Canceled,
			}},
		)
		if created || !errors.Is(err, context.Canceled) {
			t.Fatalf("post-create cancel = (%t, %v), want (false, canceled)", created, err)
		}
		assertStateDirAbsent(t, stateDir)

		created, err = authority.EnsureOwnedIncarnation(t.Context())
		if err != nil || !created {
			t.Fatalf("retry after rollback = (%t, %v), want created", created, err)
		}
	})

	t.Run("replace after create does not bind and retry sees appearance", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), ".daem")
		authority, err := CaptureStateDir(t.Context(), stateDir)
		if err != nil {
			t.Fatal(err)
		}
		created, err := authority.ensureOwnedIncarnationWithFaults(
			t.Context(),
			authority.state.physicalWorkBudget,
			barrierFaultPlan{actions: map[barrierPhase]func(){
				barrierPhasePostCreate: func() { replaceStateDir(t, stateDir) },
			}},
		)
		if created {
			t.Fatal("post-create replacement bound the replacement directory")
		}
		if err == nil {
			t.Fatal("post-create replacement returned nil error")
		}
		if _, statErr := os.Lstat(stateDir); statErr != nil {
			t.Fatalf("replacement StateDir missing: %v", statErr)
		}

		created, err = authority.EnsureOwnedIncarnation(t.Context())
		if created || !errors.Is(err, ErrStateDirAppeared) {
			t.Fatalf("retry after replacement = (%t, %v), want ErrStateDirAppeared", created, err)
		}
	})

	t.Run("cancel after accept keeps bound incarnation", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), ".daem")
		authority, err := CaptureStateDir(t.Context(), stateDir)
		if err != nil {
			t.Fatal(err)
		}
		created, err := authority.ensureOwnedIncarnationWithFaults(
			t.Context(),
			authority.state.physicalWorkBudget,
			barrierFaultPlan{failures: map[barrierPhase]error{
				barrierPhasePostAccept: context.Canceled,
			}},
		)
		if !created || !errors.Is(err, context.Canceled) {
			t.Fatalf("post-accept cancel = (%t, %v), want (true, canceled)", created, err)
		}
		if _, statErr := os.Lstat(stateDir); statErr != nil {
			t.Fatalf("owned StateDir missing after post-accept cancel: %v", statErr)
		}
		if err := authority.Validate(t.Context()); err != nil {
			t.Fatalf("Validate bound incarnation after cancel: %v", err)
		}
	})

	t.Run("replace after accept refuses identity", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), ".daem")
		authority, err := CaptureStateDir(t.Context(), stateDir)
		if err != nil {
			t.Fatal(err)
		}
		created, err := authority.ensureOwnedIncarnationWithFaults(
			t.Context(),
			authority.state.physicalWorkBudget,
			barrierFaultPlan{actions: map[barrierPhase]func(){
				barrierPhasePostAccept: func() { replaceStateDir(t, stateDir) },
			}},
		)
		if !created {
			t.Fatal("post-accept replacement un-created the bound incarnation")
		}
		if err != nil {
			t.Fatalf("post-accept replacement action error = %v, want bound then later Validate refusal", err)
		}
		if err := authority.Validate(t.Context()); !errors.Is(err, fileset.ErrFileSetAccessUnprovable) {
			t.Fatalf("Validate after post-accept replacement = %v, want identity refusal", err)
		}
	})
}

func TestEnsureStateDirForEffectFaultMatrix(t *testing.T) {
	t.Run("peer cancel before ensure leaves StateDir absent", func(t *testing.T) {
		root := t.TempDir()
		stateDir := filepath.Join(root, ".daem")
		authority, err := NewEffectAuthority(t.Context(), daempaths.Paths{
			StateDir:    stateDir,
			RecoveryDir: filepath.Join(stateDir, "recovery"),
		})
		if err != nil {
			t.Fatal(err)
		}
		created, err := authority.EnsureStateDirForEffect(
			t.Context(),
			func(context.Context) error { return context.Canceled },
		)
		if created || !errors.Is(err, context.Canceled) {
			t.Fatalf("pre-ensure peer cancel = (%t, %v)", created, err)
		}
		assertStateDirAbsent(t, stateDir)
	})

	t.Run("peer replace after ensure refuses identity", func(t *testing.T) {
		root := t.TempDir()
		stateDir := filepath.Join(root, ".daem")
		authority, err := NewEffectAuthority(t.Context(), daempaths.Paths{
			StateDir:    stateDir,
			RecoveryDir: filepath.Join(stateDir, "recovery"),
		})
		if err != nil {
			t.Fatal(err)
		}
		validations := 0
		created, err := authority.EnsureStateDirForEffect(
			t.Context(),
			func(context.Context) error {
				validations++
				if validations == 2 {
					replaceStateDir(t, stateDir)
				}
				return nil
			},
		)
		if !created {
			t.Fatal("post-ensure replacement reported no creation")
		}
		if !errors.Is(err, fileset.ErrFileSetAccessUnprovable) {
			t.Fatalf("post-ensure replacement = %v, want identity refusal", err)
		}
	})
}

func TestAbandonedResidueFenceSurvivesRetryThenClears(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), ".daem")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	residue := filepath.Join(stateDir, ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}
	authority, err := CaptureStateDir(t.Context(), stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RequireClear(t.Context()); !errors.Is(err, fileset.ErrAbandonedFileSetResidue) {
		t.Fatalf("first RequireClear = %v, want abandoned residue", err)
	}
	if err := authority.RequireClear(t.Context()); !errors.Is(err, fileset.ErrAbandonedFileSetResidue) {
		t.Fatalf("retry RequireClear = %v, want abandoned residue", err)
	}
	if err := os.Remove(residue); err != nil {
		t.Fatal(err)
	}
	if err := authority.RequireClear(t.Context()); err != nil {
		t.Fatalf("RequireClear after residue removal = %v, want clear", err)
	}
}

func replaceStateDir(t *testing.T, stateDir string) {
	t.Helper()
	retained := stateDir + "-retained"
	if err := os.Rename(stateDir, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func assertStateDirAbsent(t *testing.T, stateDir string) {
	t.Helper()
	if _, err := os.Lstat(stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("StateDir stat = %v, want absent", err)
	}
}
