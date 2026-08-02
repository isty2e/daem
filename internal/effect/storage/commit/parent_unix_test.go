//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestPrepareCommitParentPersistsPrivateMissingAncestors(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "one", "two", "state.json")
	if _, err := PrepareCommitParent(context.Background(), target); err != nil {
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
	_, err := PrepareCommitParent(context.Background(), filepath.Join(linkedParent, "state.json"))
	assertFailure(t, err, failureUncommitted, phaseCreateAncestors)
}

func TestPrepareCommitParentFaultsCleanCreatedAncestors(t *testing.T) {
	for _, failedPhase := range []phase{phaseCreateAncestors, phaseSyncAncestors} {
		t.Run(string(failedPhase), func(t *testing.T) {
			root := canonicalTempDir(t)
			createdRoot := filepath.Join(root, "one")
			target := filepath.Join(createdRoot, "two", "state.json")
			_, err := prepareCommitParentWithFaults(
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

func TestPrepareCommitParentReturnsCreatedProvenanceWhenCleanupFails(t *testing.T) {
	root := canonicalTempDir(t)
	createdRoot := filepath.Join(root, "one")
	target := filepath.Join(createdRoot, "two", "state.json")
	created, err := prepareCommitParentWithFaults(context.Background(), target, faultPlan{
		failures: map[phase]error{
			phaseSyncAncestors:    errors.New("sync injected"),
			phaseCleanupAncestors: errors.New("cleanup injected"),
		},
	})
	failure := assertFailure(t, err, failureUncommitted, phaseSyncAncestors)
	if len(created) != 2 {
		t.Fatalf("created provenance count = %d, want 2", len(created))
	}
	if len(failure.retainedResidue()) != 2 {
		t.Fatalf("retained residue = %v, want both created ancestors", failure.retainedResidue())
	}
	for _, directory := range created {
		if info, statErr := os.Stat(directory.Path()); statErr != nil || !info.IsDir() {
			t.Fatalf("created residue %q was not retained: info=%v err=%v", directory.Path(), info, statErr)
		}
	}
}

func TestPrepareCommitParentReportsOnlyCurrentInvocationCreations(t *testing.T) {
	root := canonicalTempDir(t)
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(existing, "one", "two", "state.json")

	created, err := PrepareCommitParent(context.Background(), target)
	if err != nil {
		t.Fatalf("PrepareCommitParent returned error: %v", err)
	}
	want := []string{filepath.Join(existing, "one"), filepath.Join(existing, "one", "two")}
	if len(created) != len(want) {
		t.Fatalf("created directory count = %d, want %d", len(created), len(want))
	}
	for index := range want {
		if got := created[index].Path(); got != want[index] {
			t.Fatalf("created[%d].Path() = %q, want %q", index, got, want[index])
		}
		if !created[index].valid() {
			t.Fatalf("created[%d] is not valid provenance", index)
		}
	}

	repeated, err := PrepareCommitParent(context.Background(), target)
	if err != nil {
		t.Fatalf("repeated PrepareCommitParent returned error: %v", err)
	}
	if len(repeated) != 0 {
		t.Fatalf("repeated preparation reported %d creations, want none", len(repeated))
	}
}

func TestPrepareCommitParentDoesNotClaimConcurrentExternalCreation(t *testing.T) {
	root := canonicalTempDir(t)
	external := filepath.Join(root, "external")
	target := filepath.Join(external, "state.json")

	created, err := prepareCommitParentWithFaults(context.Background(), target, faultPlan{
		actions: map[phase]func(){
			phaseCreateAncestors: func() {
				if mkdirErr := os.Mkdir(external, 0o755); mkdirErr != nil {
					t.Fatalf("create external parent: %v", mkdirErr)
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("prepareCommitParentWithFaults returned error: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("preparation claimed %d externally created directories", len(created))
	}
	info, err := os.Stat(external)
	if err != nil || !info.IsDir() {
		t.Fatalf("external parent was not preserved: info=%v err=%v", info, err)
	}
}

func TestPrepareCommitParentNoReplacePublicationDoesNotClaimConcurrentCreation(t *testing.T) {
	root := canonicalTempDir(t)
	external := filepath.Join(root, "external")
	target := filepath.Join(external, "state.json")

	created, err := prepareCommitParentWithFaults(context.Background(), target, faultPlan{
		actions: map[phase]func(){
			phaseCommitEntry: func() {
				if mkdirErr := os.Mkdir(external, 0o755); mkdirErr != nil {
					t.Fatalf("create concurrent external parent: %v", mkdirErr)
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("prepareCommitParentWithFaults returned error: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("preparation claimed %d concurrently published directories", len(created))
	}
	info, err := os.Stat(external)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o755 {
		t.Fatalf("concurrent external parent was not preserved: info=%v err=%v", info, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read parent after no-replace collision: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), temporaryPrefix) {
			t.Fatalf("staged ancestor residue remains after no-replace collision: %q", entry.Name())
		}
	}
}

func TestPrepareCommitParentDoesNotBindCreationEvidenceToPublishedReplacement(t *testing.T) {
	root := canonicalTempDir(t)
	createdPath := filepath.Join(root, "created")
	displacedPath := filepath.Join(root, "displaced")
	target := filepath.Join(createdPath, "state.json")

	created, err := prepareCommitParentWithFaults(context.Background(), target, faultPlan{
		actions: map[phase]func(){
			phasePublishAncestor: func() {
				if renameErr := os.Rename(createdPath, displacedPath); renameErr != nil {
					t.Fatalf("displace published ancestor: %v", renameErr)
				}
				if mkdirErr := os.Mkdir(createdPath, 0o700); mkdirErr != nil {
					t.Fatalf("create published replacement: %v", mkdirErr)
				}
			},
		},
	})
	assertFailure(t, err, failureUncommitted, phaseCreateAncestors)
	if len(created) != 1 {
		t.Fatalf("created evidence count = %d, want displaced staged object only", len(created))
	}
	if cleanupErr := RemoveCreatedDirectoryIfEmpty(context.Background(), created[0]); cleanupErr == nil {
		t.Fatal("cleanup accepted replacement at the publication path")
	}
	for _, path := range []string{createdPath, displacedPath} {
		if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
			t.Fatalf("directory %q was not preserved: info=%v err=%v", path, info, statErr)
		}
	}
}

func TestRemoveCreatedDirectoryIfEmptyRemovesExactEmptyDirectory(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "one", "two", "state.json")
	created, err := PrepareCommitParent(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}

	for index := len(created) - 1; index >= 0; index-- {
		if err := RemoveCreatedDirectoryIfEmpty(context.Background(), created[index]); err != nil {
			t.Fatalf("RemoveCreatedDirectoryIfEmpty(%q): %v", created[index].Path(), err)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "one")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created ancestor remains: %v", err)
	}
	for index := len(created) - 1; index >= 0; index-- {
		if err := RemoveCreatedDirectoryIfEmpty(context.Background(), created[index]); err != nil {
			t.Fatalf("repeated RemoveCreatedDirectoryIfEmpty(%q): %v", created[index].Path(), err)
		}
	}
}

func TestRemoveCreatedDirectoryIfEmptyPreservesPopulatedDirectory(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "created", "state.json")
	created, err := PrepareCommitParent(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(created[0].Path(), "external.txt")
	if err := os.WriteFile(external, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = RemoveCreatedDirectoryIfEmpty(context.Background(), created[0])
	if err == nil {
		t.Fatal("RemoveCreatedDirectoryIfEmpty removed or accepted a populated directory")
	}
	if content, readErr := os.ReadFile(external); readErr != nil || string(content) != "keep" {
		t.Fatalf("external content was not preserved: content=%q err=%v", content, readErr)
	}
}

func TestRemoveCreatedDirectoryIfEmptyRejectsReplacementAndSymlink(t *testing.T) {
	for _, replacementKind := range []string{"directory", "symlink"} {
		t.Run(replacementKind, func(t *testing.T) {
			root := canonicalTempDir(t)
			target := filepath.Join(root, "created", "state.json")
			created, err := PrepareCommitParent(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			original := created[0].Path()
			displaced := filepath.Join(root, "displaced")
			if err := os.Rename(original, displaced); err != nil {
				t.Fatal(err)
			}
			switch replacementKind {
			case "directory":
				err = os.Mkdir(original, 0o700)
			case "symlink":
				err = os.Symlink(displaced, original)
			}
			if err != nil {
				t.Fatal(err)
			}

			err = RemoveCreatedDirectoryIfEmpty(context.Background(), created[0])
			if err == nil {
				t.Fatal("RemoveCreatedDirectoryIfEmpty accepted replacement")
			}
			if _, statErr := os.Lstat(original); statErr != nil {
				t.Fatalf("replacement was not preserved: %v", statErr)
			}
			if info, statErr := os.Stat(displaced); statErr != nil || !info.IsDir() {
				t.Fatalf("original directory was not preserved: info=%v err=%v", info, statErr)
			}
		})
	}
}

func TestRemoveCreatedDirectoryIfEmptyRevalidatesAfterCleanupRace(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "created", "state.json")
	created, err := PrepareCommitParent(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	original := created[0].Path()
	displaced := filepath.Join(root, "displaced")

	err = removeCreatedDirectoryIfEmptyWithFaults(context.Background(), created[0], faultPlan{
		actions: map[phase]func(){
			phaseCleanupEntry: func() {
				if renameErr := os.Rename(original, displaced); renameErr != nil {
					t.Fatalf("displace created directory: %v", renameErr)
				}
				if mkdirErr := os.Mkdir(original, 0o700); mkdirErr != nil {
					t.Fatalf("create replacement directory: %v", mkdirErr)
				}
			},
		},
	})
	assertFailure(t, err, failureUncommitted, phaseCleanupEntry)
	if info, statErr := os.Stat(original); statErr != nil || !info.IsDir() {
		t.Fatalf("replacement directory was not preserved: info=%v err=%v", info, statErr)
	}
	if info, statErr := os.Stat(displaced); statErr != nil || !info.IsDir() {
		t.Fatalf("created directory was not preserved after displacement: info=%v err=%v", info, statErr)
	}
}

func TestRemoveCreatedDirectoryIfEmptyHonorsCancellationAndFaults(t *testing.T) {
	tests := []struct {
		name      string
		context   func() context.Context
		faults    faultPlan
		wantKind  mutationfs.FailureKind
		wantPhase phase
		wantGone  bool
	}{
		{
			name: "canceled",
			context: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantKind:  failureUncommitted,
			wantPhase: phaseValidate,
		},
		{
			name:      "cleanup",
			context:   context.Background,
			faults:    faultPlan{failures: map[phase]error{phaseCleanupEntry: errors.New("injected")}},
			wantKind:  failureUncommitted,
			wantPhase: phaseCleanupEntry,
		},
		{
			name:      "sync",
			context:   context.Background,
			faults:    faultPlan{failures: map[phase]error{phaseSyncCleanupParent: errors.New("injected")}},
			wantKind:  failureIndeterminateCommit,
			wantPhase: phaseSyncCleanupParent,
			wantGone:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			target := filepath.Join(root, "created", "state.json")
			created, err := PrepareCommitParent(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			err = removeCreatedDirectoryIfEmptyWithFaults(test.context(), created[0], test.faults)
			assertFailure(t, err, test.wantKind, test.wantPhase)
			_, statErr := os.Lstat(created[0].Path())
			if test.wantGone && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("created directory remains after visible cleanup: %v", statErr)
			}
			if !test.wantGone && statErr != nil {
				t.Fatalf("created directory was removed before commit: %v", statErr)
			}
		})
	}
}
