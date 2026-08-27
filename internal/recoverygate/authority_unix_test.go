//go:build darwin || linux

package recoverygate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/mutation"
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

func TestEffectAuthorityRequiresExplicitAcceptanceOfFirstStateDirIncarnation(t *testing.T) {
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
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := authority.Validate(t.Context()); !errors.Is(err, transaction.ErrStateDirAppeared) {
		t.Fatalf("Validate appeared StateDir error = %v, want appearance transition", err)
	} else {
		var stale mutation.StaleSnapshotError
		if !errors.As(err, &stale) {
			t.Fatalf("Validate appeared StateDir error = %v, want stale snapshot", err)
		}
	}
	if err := authority.AcceptStateDirCreation(t.Context()); err != nil {
		t.Fatalf("AcceptStateDirCreation: %v", err)
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
