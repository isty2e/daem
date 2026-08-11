//go:build darwin || linux

package commit

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestValidateRootedDirectoryTreeEnforcesMetadataContract(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	destination := filepath.Join(root, ".agents", "tree")
	if err := os.MkdirAll(filepath.Join(destination, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(
		t,
		filepath.Join(destination, "directory", "file"),
		"payload",
		0o600,
	)
	if err := os.Symlink("directory/file", filepath.Join(destination, "link")); err != nil {
		t.Fatal(err)
	}
	captured := captureRootForCommitTest(t, root)

	validate := func(limitsEntries int, limitsDepth int, limitsBytes int64) error {
		capability := rootedCapabilityForCommitTest(t, captured, ".agents/tree")
		defer capability.Close()
		_, err := ValidateRootedDirectoryTree(
			t.Context(),
			capability,
			mustTreeTraversalLimits(t, limitsEntries, limitsDepth, limitsBytes),
		)
		return err
	}
	if err := validate(3, 1, 7); err != nil {
		t.Fatalf("exact metadata limits failed: %v", err)
	}
	if err := validate(2, 1, 7); err == nil {
		t.Fatal("entry cardinality limit was not enforced")
	}
	if err := validate(3, 0, 7); err == nil {
		t.Fatal("directory depth limit was not enforced")
	}
	if err := validate(3, 1, 6); err == nil {
		t.Fatal("regular-file byte limit was not enforced")
	}
}

func TestValidateRootedDirectoryTreeRejectsSpecialChild(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	destination := filepath.Join(root, ".agents", "tree")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	special := filepath.Join(destination, "special")
	if err := unix.Mkfifo(special, 0o600); err != nil {
		t.Fatal(err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/tree")
	defer capability.Close()

	if _, err := ValidateRootedDirectoryTree(
		t.Context(),
		capability,
		mustTreeTraversalLimits(t, 1, 0, 1),
	); err == nil {
		t.Fatal("special child was admitted")
	}
	if info, err := os.Lstat(special); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("special child changed: info=%v err=%v", info, err)
	}
}
