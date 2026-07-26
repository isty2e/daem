//go:build darwin || linux

package rootedpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBindSelectedEntryBindsAliasRelativeChildToCapturedRoot(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, "physical")
	alias := filepath.Join(base, "alias")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	root, err := CaptureRoot(alias)
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })

	destination, err := root.bindSelectedEntry(alias, filepath.Join(alias, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("BindSelectedEntry returned error: %v", err)
	}
	got, err := destination.LexicalPath()
	if err != nil {
		t.Fatalf("LexicalPath returned error: %v", err)
	}
	resolvedPhysical, err := filepath.EvalSymlinks(physical)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	want := filepath.Join(resolvedPhysical, ".daem", "state.json")
	if got != want {
		t.Fatalf("destination path = %q, want %q", got, want)
	}
}

func TestBindSelectedEntryRejectsEscapeAndReplacedSelection(t *testing.T) {
	base := t.TempDir()
	selected := filepath.Join(base, "project")
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := CaptureRoot(selected)
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })

	_, err = root.bindSelectedEntry(selected, filepath.Join(base, "outside"))
	if !hasFailureKind(err, FailureInvalidDestination) {
		t.Fatalf("escape error = %v, want %s", err, FailureInvalidDestination)
	}

	moved := selected + "-moved"
	if err := os.Rename(selected, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = root.bindSelectedEntry(selected, filepath.Join(selected, ".daem", "state.json"))
	if !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("replacement error = %v, want %s", err, FailureRootReplaced)
	}
}

func TestBindSelectedEntryAuthorityCapturesExternalEntryIndependently(t *testing.T) {
	base := t.TempDir()
	selected := filepath.Join(base, "config")
	stateDir := filepath.Join(base, "state")
	statePath := filepath.Join(stateDir, "state.json")
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := CaptureRoot(selected)
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })

	authority, err := BindSelectedEntryAuthority(root, selected, statePath)
	if err != nil {
		t.Fatalf("BindSelectedEntryAuthority returned error: %v", err)
	}
	capability, err := authority.Acquire()
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	got, err := capability.Destination().LexicalPath()
	if err != nil {
		t.Fatalf("LexicalPath returned error: %v", err)
	}
	physicalStateDir, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	want := filepath.Join(physicalStateDir, "state.json")
	if got != want {
		t.Fatalf("bound external path = %q, want %q", got, want)
	}
	if err := capability.Close(); err != nil {
		t.Fatalf("close capability: %v", err)
	}
	if err := authority.Close(); err != nil {
		t.Fatalf("close external authority: %v", err)
	}
	if _, err := authority.Acquire(); !hasFailureKind(err, FailureRootUnavailable) {
		t.Fatalf("closed authority error = %v, want %s", err, FailureRootUnavailable)
	}
	if err := root.ValidateSelection(selected); err != nil {
		t.Fatalf("external authority close invalidated selected root: %v", err)
	}
}

func TestBindSelectedEntryAuthorityCloseDoesNotCloseBorrowedRoot(t *testing.T) {
	selected := t.TempDir()
	root, err := CaptureRoot(selected)
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })

	authority, err := BindSelectedEntryAuthority(
		root,
		selected,
		filepath.Join(selected, ".daem", "state.json"),
	)
	if err != nil {
		t.Fatalf("BindSelectedEntryAuthority returned error: %v", err)
	}
	if err := authority.Close(); err != nil {
		t.Fatalf("close borrowed authority: %v", err)
	}
	if err := root.ValidateSelection(selected); err != nil {
		t.Fatalf("borrowed authority close invalidated selected root: %v", err)
	}
}

func TestBindSelectedEntryAuthorityRejectsRootItselfAndReplacedSelection(t *testing.T) {
	base := t.TempDir()
	selected := filepath.Join(base, "project")
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := CaptureRoot(selected)
	if err != nil {
		t.Fatalf("CaptureRoot returned error: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })

	_, err = BindSelectedEntryAuthority(root, selected, selected)
	if !hasFailureKind(err, FailureInvalidDestination) {
		t.Fatalf("same-root error = %v, want %s", err, FailureInvalidDestination)
	}

	moved := selected + "-moved"
	if err := os.Rename(selected, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = BindSelectedEntryAuthority(
		root,
		selected,
		filepath.Join(base, "external", "state.json"),
	)
	if !hasFailureKind(err, FailureRootReplaced) {
		t.Fatalf("replacement error = %v, want %s", err, FailureRootReplaced)
	}
}
