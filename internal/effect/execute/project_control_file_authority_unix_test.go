//go:build darwin || linux

package execute

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func TestRootedControlFileCommitSupportsExternalStateAuthority(t *testing.T) {
	base := t.TempDir()
	selectedRoot := filepath.Join(base, "config")
	stateDir := filepath.Join(base, "state")
	statePath := filepath.Join(stateDir, "state.json")
	if err := os.Mkdir(selectedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	selected, err := rootedpath.CaptureRoot(selectedRoot)
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() { _ = selected.Close() })
	authority, err := rootedpath.BindSelectedEntryAuthority(
		selected,
		selectedRoot,
		statePath,
	)
	if err != nil {
		t.Fatalf("BindSelectedEntryAuthority returned error: %v", err)
	}
	t.Cleanup(func() { _ = authority.Close() })

	if err := commitRootedControlFile(
		t.Context(),
		testFilesystem(),
		authority,
		[]byte("state\n"),
		0o600,
	); err != nil {
		t.Fatalf("commitRootedControlFile returned error: %v", err)
	}
	assertHostFileContent(t, statePath, "state\n")
}

func TestRootedControlFileCommitCreatesMissingExternalStateParents(t *testing.T) {
	base := t.TempDir()
	selectedRoot := filepath.Join(base, "config")
	statePath := filepath.Join(base, "state", "daem", "state.json")
	if err := os.Mkdir(selectedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	selected, err := rootedpath.CaptureRoot(selectedRoot)
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() { _ = selected.Close() })
	authority, err := rootedpath.BindSelectedEntryAuthority(
		selected,
		selectedRoot,
		statePath,
	)
	if err != nil {
		t.Fatalf("BindSelectedEntryAuthority returned error: %v", err)
	}
	t.Cleanup(func() { _ = authority.Close() })

	if err := commitRootedControlFile(
		t.Context(),
		testFilesystem(),
		authority,
		[]byte("state\n"),
		0o600,
	); err != nil {
		t.Fatalf("commitRootedControlFile returned error: %v", err)
	}
	assertHostFileContent(t, statePath, "state\n")
}

func TestRootedControlFileCommitRejectsExternalParentSymlinkSubstitution(t *testing.T) {
	base := t.TempDir()
	selectedRoot := filepath.Join(base, "config")
	externalRoot := filepath.Join(base, "external")
	stateParent := filepath.Join(externalRoot, "state")
	statePath := filepath.Join(stateParent, "state.json")
	redirectedRoot := filepath.Join(base, "redirected")
	for _, path := range []string{selectedRoot, externalRoot, redirectedRoot} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	selected, err := rootedpath.CaptureRoot(selectedRoot)
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() { _ = selected.Close() })
	authority, err := rootedpath.BindSelectedEntryAuthority(
		selected,
		selectedRoot,
		statePath,
	)
	if err != nil {
		t.Fatalf("BindSelectedEntryAuthority returned error: %v", err)
	}
	t.Cleanup(func() { _ = authority.Close() })
	if err := os.Symlink(redirectedRoot, stateParent); err != nil {
		t.Fatalf("replace missing state parent with symlink: %v", err)
	}

	if err := commitRootedControlFile(
		t.Context(),
		testFilesystem(),
		authority,
		[]byte("state\n"),
		0o600,
	); err == nil {
		t.Fatal("commitRootedControlFile error = nil, want symlink substitution refusal")
	}
	assertHostMissing(t, filepath.Join(redirectedRoot, "state.json"))
}

func TestRootedControlFileCommitRejectsExternalStateRootReplacement(t *testing.T) {
	base := t.TempDir()
	selectedRoot := filepath.Join(base, "config")
	stateDir := filepath.Join(base, "state")
	statePath := filepath.Join(stateDir, "state.json")
	movedStateDir := stateDir + "-moved"
	if err := os.Mkdir(selectedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	selected, err := rootedpath.CaptureRoot(selectedRoot)
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() { _ = selected.Close() })
	authority, err := rootedpath.BindSelectedEntryAuthority(
		selected,
		selectedRoot,
		statePath,
	)
	if err != nil {
		t.Fatalf("BindSelectedEntryAuthority returned error: %v", err)
	}
	t.Cleanup(func() { _ = authority.Close() })
	if err := os.Rename(stateDir, movedStateDir); err != nil {
		t.Fatalf("move selected state root: %v", err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("create replacement state root: %v", err)
	}

	err = commitRootedControlFile(
		t.Context(),
		testFilesystem(),
		authority,
		[]byte("state\n"),
		0o600,
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("commitRootedControlFile error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	assertHostMissing(t, statePath)
	assertHostMissing(t, filepath.Join(movedStateDir, "state.json"))
}
