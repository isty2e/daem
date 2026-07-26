//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareCommitParentPersistsPrivateMissingAncestors(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "one", "two", "state.json")
	if err := PrepareCommitParent(context.Background(), target); err != nil {
		t.Fatalf("PrepareCommitParent returned error: %v", err)
	}
	for _, path := range []string{filepath.Join(root, "one"), filepath.Join(root, "one", "two")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%q) returned error: %v", path, err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("%q mode = %v, want private directory", path, info.Mode())
		}
	}
}

func TestPrepareCommitParentRejectsSymlinkAncestor(t *testing.T) {
	root := canonicalTempDir(t)
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	linkedParent := filepath.Join(root, "linked")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}
	err := PrepareCommitParent(context.Background(), filepath.Join(linkedParent, "state.json"))
	assertFailure(t, err, failureUncommitted, phaseCreateAncestors)
}

func TestPrepareCommitParentFaultsCleanCreatedAncestors(t *testing.T) {
	for _, failedPhase := range []phase{phaseCreateAncestors, phaseSyncAncestors} {
		t.Run(string(failedPhase), func(t *testing.T) {
			root := canonicalTempDir(t)
			createdRoot := filepath.Join(root, "one")
			target := filepath.Join(createdRoot, "two", "state.json")
			err := prepareCommitParentWithFaults(
				context.Background(),
				target,
				faultPlan{failures: map[phase]error{failedPhase: errors.New("injected")}},
			)
			assertFailure(t, err, failureUncommitted, failedPhase)
			if _, statErr := os.Lstat(createdRoot); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("created ancestor remains after %s fault: %v", failedPhase, statErr)
			}
		})
	}
}
