//go:build darwin || linux

package recoverygate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/declaration/transaction"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestEffectAuthorityRejectsStateDirDirectoryReplacement(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := daempaths.Paths{
		StateDir:    stateDir,
		RecoveryDir: filepath.Join(stateDir, "recovery"),
	}
	authority, err := NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	retained := stateDir + "-retained"
	if err := os.Rename(stateDir, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := authority.Validate(t.Context()); !errors.Is(err, transaction.ErrFileSetAccessUnprovable) {
		t.Fatalf("Validate error = %v, want StateDir identity refusal", err)
	}
}

func TestEffectAuthorityCreatesStateDirBetweenPeerAuthorityValidations(t *testing.T) {
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
			_, statErr := os.Stat(stateDir)
			if validations == 1 && !os.IsNotExist(statErr) {
				t.Fatalf("StateDir existed during pre-effect validation: %v", statErr)
			}
			if validations == 2 && statErr != nil {
				t.Fatalf("StateDir missing during post-effect validation: %v", statErr)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("EnsureStateDirForEffect: %v", err)
	}
	if !created || validations != 2 {
		t.Fatalf("created, validations = %t, %d; want true, 2", created, validations)
	}
}

func TestEffectAuthorityDoesNotCreateStateDirAfterPeerValidationFailure(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	authority, err := NewEffectAuthority(t.Context(), daempaths.Paths{
		StateDir:    stateDir,
		RecoveryDir: filepath.Join(stateDir, "recovery"),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("peer authority changed")

	created, err := authority.EnsureStateDirForEffect(
		t.Context(),
		func(context.Context) error { return wantErr },
	)
	if !errors.Is(err, wantErr) || created {
		t.Fatalf("EnsureStateDirForEffect = %t, %v; want false, peer error", created, err)
	}
	if _, statErr := os.Stat(stateDir); !os.IsNotExist(statErr) {
		t.Fatalf("StateDir stat error = %v, want absent", statErr)
	}
}

func TestEffectAuthorityCreatesAndBindsFirstStateDirIncarnation(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".daem")
	paths := daempaths.Paths{
		StateDir:    stateDir,
		RecoveryDir: filepath.Join(stateDir, "recovery"),
	}
	authority, err := NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Validate(t.Context()); err != nil {
		t.Fatalf("Validate absent StateDir: %v", err)
	}
	created, err := authority.EnsureStateDirForEffect(
		t.Context(),
		func(context.Context) error { return nil },
	)
	if err != nil {
		t.Fatalf("EnsureStateDirForEffect: %v", err)
	}
	if !created {
		t.Fatal("EnsureStateDirForEffect did not report the created directory")
	}
	if err := authority.Validate(t.Context()); err != nil {
		t.Fatalf("Validate accepted StateDir: %v", err)
	}

	retained := stateDir + "-retained"
	if err := os.Rename(stateDir, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := authority.Validate(t.Context()); !errors.Is(err, transaction.ErrFileSetAccessUnprovable) {
		t.Fatalf("Validate replacement error = %v, want StateDir identity refusal", err)
	}
}
