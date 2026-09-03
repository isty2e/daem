//go:build !windows

package recover

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

func TestRecoverPlanBlocksObservedActiveJournalWhenStateDirAccessIsUnprovable(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	_, err := planRecoveryWithFilesystemAndFence(
		t.Context(),
		fixture.input,
		storagecommit.Adapter{},
		func(context.Context, recoverygate.StateDirAuthority) error {
			return fileset.ErrFileSetAccessUnprovable
		},
	)
	if !errors.Is(err, fileset.ErrFileSetAccessUnprovable) {
		t.Fatalf("Plan error = %v, want StateDir access failure", err)
	}
	if !errors.Is(err, journal.ErrInterruptedApply) {
		t.Fatalf("Plan error = %v, want observed active journal authority", err)
	}
	if _, statErr := os.Lstat(fixture.operationDir); statErr != nil {
		t.Fatalf("journal authority changed after refused planning: %v", statErr)
	}
}

func TestRecoverExecuteRejectsStateDirReplacementEvenWhenJournalMovesWithIt(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	retainedStateDir := paths.StateDir + "-retained"
	if err := os.Rename(paths.StateDir, retainedStateDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(
		filepath.Join(retainedStateDir, "recovery"),
		paths.RecoveryDir,
	); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Lstat(filepath.Join(retainedStateDir, "state.json")); statErr == nil {
		if err := os.Rename(
			filepath.Join(retainedStateDir, "state.json"),
			paths.StatefilePath,
		); err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal(statErr)
	}
	residue := filepath.Join(retainedStateDir, ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err = Execute(t.Context(), prepared, ExecuteOptions{})
	if !errors.Is(err, fileset.ErrFileSetAccessUnprovable) {
		t.Fatalf("Execute error = %v, want StateDir identity failure", err)
	}
	if _, statErr := os.Lstat(residue); statErr != nil {
		t.Fatalf("retained residue changed after refused execution: %v", statErr)
	}
	if content, readErr := os.ReadFile(fixture.hostPath); readErr != nil {
		t.Fatal(readErr)
	} else if string(content) != string(fixture.newContent) {
		t.Fatalf("host content = %q, want unchanged %q", content, fixture.newContent)
	}
}

func TestRecoverExecuteBlocksWhenStateDirIdentityIsLostAfterPlanning(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	retainedStateDir := paths.StateDir + "-retained"
	retainedOperation := filepath.Join(
		retainedStateDir,
		"recovery",
		filepath.Base(fixture.operationDir),
	)
	if err := os.Rename(paths.StateDir, retainedStateDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StateDir, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(paths.StateDir)
		_ = os.Rename(retainedStateDir, paths.StateDir)
	})

	_, err = Execute(t.Context(), prepared, ExecuteOptions{})
	if !errors.Is(err, fileset.ErrFileSetAccessUnprovable) {
		t.Fatalf("Execute error = %v, want StateDir access failure", err)
	}
	if _, statErr := os.Lstat(retainedOperation); statErr != nil {
		t.Fatalf("retained journal authority changed after refused execution: %v", statErr)
	}
	content, readErr := os.ReadFile(fixture.hostPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != string(fixture.newContent) {
		t.Fatalf("host content = %q, want unchanged %q", content, fixture.newContent)
	}
}
