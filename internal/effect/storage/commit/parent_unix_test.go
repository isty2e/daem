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
	"golang.org/x/sys/unix"
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
			cleanup := newTestAncestorCleanup(t)
			err := prepareCommitParentWithFaults(
				context.Background(),
				target,
				faultPlan{failures: map[phase]error{failedPhase: errors.New("injected")}},
				cleanup,
			)
			assertFailure(t, err, failureUncommitted, failedPhase)
			if _, statErr := os.Lstat(createdRoot); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("created ancestor remains after %s fault: %v", failedPhase, statErr)
			}
		})
	}
}

func TestPrepareCommitParentRetainsCleanupAuthorityWhenCleanupFails(t *testing.T) {
	root := canonicalTempDir(t)
	createdRoot := filepath.Join(root, "one")
	target := filepath.Join(createdRoot, "two", "state.json")
	cleanup := newTestAncestorCleanup(t)
	err := prepareCommitParentWithFaults(context.Background(), target, faultPlan{
		failures: map[phase]error{
			phaseSyncAncestors:    errors.New("sync injected"),
			phaseCleanupAncestors: errors.New("cleanup injected"),
		},
	}, cleanup)
	failure := assertFailure(t, err, failureUncommitted, phaseSyncAncestors)
	directories := testCreatedDirectories(cleanup)
	if len(directories) != 2 {
		t.Fatalf("created cleanup authority count = %d, want 2", len(directories))
	}
	if len(failure.retainedResidue()) != 2 {
		t.Fatalf("retained residue = %v, want both created ancestors", failure.retainedResidue())
	}
	for _, directory := range directories {
		if info, statErr := os.Stat(directory.path); statErr != nil || !info.IsDir() {
			t.Fatalf("created residue %q was not retained: info=%v err=%v", directory.path, info, statErr)
		}
	}
}

func TestAncestorCleanupReportsOnlyCurrentInvocationCreations(t *testing.T) {
	root := canonicalTempDir(t)
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(existing, "one", "two", "state.json")

	cleanup := newTestAncestorCleanup(t)
	if err := cleanup.PrepareParent(context.Background(), target); err != nil {
		t.Fatalf("PrepareParent returned error: %v", err)
	}
	want := []string{filepath.Join(existing, "one"), filepath.Join(existing, "one", "two")}
	directories := testCreatedDirectories(cleanup)
	if len(directories) != len(want) {
		t.Fatalf("created directory count = %d, want %d", len(directories), len(want))
	}
	for index := range want {
		if got := directories[index].path; got != want[index] {
			t.Fatalf("created[%d].path = %q, want %q", index, got, want[index])
		}
		if !directories[index].valid() {
			t.Fatalf("created[%d] is not valid cleanup authority", index)
		}
	}

	repeated := newTestAncestorCleanup(t)
	if err := repeated.PrepareParent(context.Background(), target); err != nil {
		t.Fatalf("repeated PrepareParent returned error: %v", err)
	}
	if directories := testCreatedDirectories(repeated); len(directories) != 0 {
		t.Fatalf("repeated preparation reported %d creations, want none", len(directories))
	}
}

func TestPrepareCommitParentDoesNotClaimConcurrentExternalCreation(t *testing.T) {
	root := canonicalTempDir(t)
	external := filepath.Join(root, "external")
	target := filepath.Join(external, "state.json")
	cleanup := newTestAncestorCleanup(t)

	err := prepareCommitParentWithFaults(context.Background(), target, faultPlan{
		actions: map[phase]func(){
			phaseCreateAncestors: func() {
				if mkdirErr := os.Mkdir(external, 0o755); mkdirErr != nil {
					t.Fatalf("create external parent: %v", mkdirErr)
				}
			},
		},
	}, cleanup)
	if err != nil {
		t.Fatalf("prepareCommitParentWithFaults returned error: %v", err)
	}
	if directories := testCreatedDirectories(cleanup); len(directories) != 0 {
		t.Fatalf("preparation claimed %d externally created directories", len(directories))
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
	cleanup := newTestAncestorCleanup(t)

	err := prepareCommitParentWithFaults(context.Background(), target, faultPlan{
		actions: map[phase]func(){
			phaseCommitEntry: func() {
				if mkdirErr := os.Mkdir(external, 0o755); mkdirErr != nil {
					t.Fatalf("create concurrent external parent: %v", mkdirErr)
				}
			},
		},
	}, cleanup)
	if err != nil {
		t.Fatalf("prepareCommitParentWithFaults returned error: %v", err)
	}
	if directories := testCreatedDirectories(cleanup); len(directories) != 0 {
		t.Fatalf("preparation claimed %d concurrently published directories", len(directories))
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

func TestPrepareCommitParentDoesNotBindCleanupAuthorityToPublishedReplacement(t *testing.T) {
	root := canonicalTempDir(t)
	createdPath := filepath.Join(root, "created")
	displacedPath := filepath.Join(root, "displaced")
	target := filepath.Join(createdPath, "state.json")
	cleanup := newTestAncestorCleanup(t)

	err := prepareCommitParentWithFaults(context.Background(), target, faultPlan{
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
	}, cleanup)
	assertFailure(t, err, failureUncommitted, phaseCreateAncestors)
	if directories := testCreatedDirectories(cleanup); len(directories) != 1 {
		t.Fatalf("created cleanup authority count = %d, want displaced staged object only", len(directories))
	}
	if cleanupErr := cleanup.RemoveEmpty(context.Background()); cleanupErr == nil {
		t.Fatal("cleanup accepted replacement at the publication path")
	}
	for _, path := range []string{createdPath, displacedPath} {
		if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
			t.Fatalf("directory %q was not preserved: info=%v err=%v", path, info, statErr)
		}
	}
}

func TestAncestorCleanupRemovesExactEmptyDirectories(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "one", "two", "state.json")
	cleanup := newTestAncestorCleanup(t)
	if err := cleanup.PrepareParent(context.Background(), target); err != nil {
		t.Fatal(err)
	}

	if err := cleanup.RemoveEmpty(context.Background()); err != nil {
		t.Fatalf("RemoveEmpty: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "one")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created ancestor remains: %v", err)
	}
	if err := cleanup.RemoveEmpty(context.Background()); err != nil {
		t.Fatalf("repeated RemoveEmpty: %v", err)
	}
}

func TestAncestorCleanupPreservesPopulatedDirectory(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "created", "state.json")
	cleanup := newTestAncestorCleanup(t)
	if err := cleanup.PrepareParent(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(testCreatedDirectories(cleanup)[0].path, "external.txt")
	if err := os.WriteFile(external, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := cleanup.RemoveEmpty(context.Background()); err == nil {
		t.Fatal("RemoveEmpty removed or accepted a populated directory")
	}
	if content, readErr := os.ReadFile(external); readErr != nil || string(content) != "keep" {
		t.Fatalf("external content was not preserved: content=%q err=%v", content, readErr)
	}
}

func TestAncestorCleanupRejectsReplacementAndSymlink(t *testing.T) {
	for _, replacementKind := range []string{"directory", "symlink"} {
		t.Run(replacementKind, func(t *testing.T) {
			root := canonicalTempDir(t)
			target := filepath.Join(root, "created", "state.json")
			cleanup := newTestAncestorCleanup(t)
			if err := cleanup.PrepareParent(context.Background(), target); err != nil {
				t.Fatal(err)
			}
			original := testCreatedDirectories(cleanup)[0].path
			displaced := filepath.Join(root, "displaced")
			if err := os.Rename(original, displaced); err != nil {
				t.Fatal(err)
			}
			var err error
			switch replacementKind {
			case "directory":
				err = os.Mkdir(original, 0o700)
			case "symlink":
				err = os.Symlink(displaced, original)
			}
			if err != nil {
				t.Fatal(err)
			}

			if err := cleanup.RemoveEmpty(context.Background()); err == nil {
				t.Fatal("RemoveEmpty accepted replacement")
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

func TestAncestorCleanupRetainsCreatedObjectHandleUntilClose(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "created", "state.json")
	cleanup := newTestAncestorCleanup(t)
	if err := cleanup.PrepareParent(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	directory := testCreatedDirectories(cleanup)[0]
	displaced := filepath.Join(root, "displaced")
	if err := os.Rename(directory.path, displaced); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory.path, 0o700); err != nil {
		t.Fatal(err)
	}
	var retained unix.Stat_t
	if err := unix.Fstat(directory.fd, &retained); err != nil {
		t.Fatalf("created object handle was not retained: %v", err)
	}
	if err := cleanup.RemoveEmpty(context.Background()); err == nil {
		t.Fatal("cleanup accepted replacement while the created object handle was retained")
	}
	cleanup.Close()
	if err := unix.Fstat(directory.fd, &retained); !errors.Is(err, unix.EBADF) {
		t.Fatalf("created object handle remained open after Close: %v", err)
	}
	for _, path := range []string{directory.path, displaced} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("directory %q was not preserved: info=%v err=%v", path, info, err)
		}
	}
}

func TestAncestorCleanupCopiesShareOneHandleLifetime(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "created", "state.json")
	cleanup := newTestAncestorCleanup(t)
	if err := cleanup.PrepareParent(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	directory := testCreatedDirectories(cleanup)[0]
	copied := *cleanup
	copied.Close()

	var stat unix.Stat_t
	if err := unix.Fstat(directory.fd, &stat); !errors.Is(err, unix.EBADF) {
		t.Fatalf("copied cleanup did not close shared handle: %v", err)
	}
	if err := cleanup.RemoveEmpty(context.Background()); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("original cleanup remained usable after copied Close: %v", err)
	}
	cleanup.Close()
}

func TestClosedAncestorCleanupRefusesEffects(t *testing.T) {
	root := canonicalTempDir(t)
	parent := filepath.Join(root, "missing")
	target := filepath.Join(parent, "state.json")
	var cleanup AncestorCleanup
	cleanup.Close()
	if err := cleanup.PrepareParent(context.Background(), target); err == nil {
		t.Fatal("closed cleanup prepared a parent")
	}
	request, err := NewFileCreate(target, []byte("content"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup.CommitFile(context.Background(), request); err == nil {
		t.Fatal("closed cleanup committed a file")
	}
	if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("closed cleanup performed a filesystem effect: %v", err)
	}
}

func TestAncestorCleanupRevalidatesAfterCleanupRace(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "created", "state.json")
	cleanup := newTestAncestorCleanup(t)
	if err := cleanup.PrepareParent(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	directory := testCreatedDirectories(cleanup)[0]
	displaced := filepath.Join(root, "displaced")

	err := removeCreatedDirectoryIfEmptyWithFaults(context.Background(), directory, faultPlan{
		actions: map[phase]func(){
			phaseCleanupEntry: func() {
				if renameErr := os.Rename(directory.path, displaced); renameErr != nil {
					t.Fatalf("displace created directory: %v", renameErr)
				}
				if mkdirErr := os.Mkdir(directory.path, 0o700); mkdirErr != nil {
					t.Fatalf("create replacement directory: %v", mkdirErr)
				}
			},
		},
	})
	assertFailure(t, err, failureUncommitted, phaseCleanupEntry)
	if info, statErr := os.Stat(directory.path); statErr != nil || !info.IsDir() {
		t.Fatalf("replacement directory was not preserved: info=%v err=%v", info, statErr)
	}
	if info, statErr := os.Stat(displaced); statErr != nil || !info.IsDir() {
		t.Fatalf("created directory was not preserved after displacement: info=%v err=%v", info, statErr)
	}
}

func TestAncestorCleanupHonorsCancellationAndFaults(t *testing.T) {
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
			cleanup := newTestAncestorCleanup(t)
			if err := cleanup.PrepareParent(context.Background(), target); err != nil {
				t.Fatal(err)
			}
			directory := testCreatedDirectories(cleanup)[0]
			err := removeCreatedDirectoryIfEmptyWithFaults(test.context(), directory, test.faults)
			assertFailure(t, err, test.wantKind, test.wantPhase)
			_, statErr := os.Lstat(directory.path)
			if test.wantGone && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("created directory remains after visible cleanup: %v", statErr)
			}
			if !test.wantGone && statErr != nil {
				t.Fatalf("created directory was removed before commit: %v", statErr)
			}
		})
	}
}

func newTestAncestorCleanup(t *testing.T) *AncestorCleanup {
	t.Helper()
	cleanup := &AncestorCleanup{}
	t.Cleanup(cleanup.Close)
	return cleanup
}

func testCreatedDirectories(cleanup *AncestorCleanup) []createdDirectory {
	if cleanup == nil || cleanup.state == nil {
		return nil
	}
	return cleanup.state.directories
}
