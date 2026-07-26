//go:build unix

package repair

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestVerifiedRepairStagingRestoresModesAcrossUmask(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "source")
	writeTestFile(t, sourceRoot, "SKILL.md", "content")
	writeTestFile(t, sourceRoot, "nested/tool.sh", "#!/bin/sh\n")
	chmodTestPath(t, sourceRoot, 0o750)
	chmodTestPath(t, filepath.Join(sourceRoot, "nested"), 0o711)
	chmodTestPath(t, filepath.Join(sourceRoot, "SKILL.md"), 0o640)
	chmodTestPath(t, filepath.Join(sourceRoot, "nested", "tool.sh"), 0o751)
	identity, view := testArtifact(t, sourceRoot)

	previousUmask := syscall.Umask(0o077)
	defer syscall.Umask(previousUmask)
	staging, err := newVerifiedRepairStaging(context.Background(), identity, view)
	if err != nil {
		t.Fatalf("newVerifiedRepairStaging() error: %v", err)
	}
	t.Cleanup(func() {
		if err := staging.release(); err != nil {
			t.Fatalf("release() error: %v", err)
		}
	})
	if err := staging.finalizeDirectoryModes(); err != nil {
		t.Fatalf("finalizeDirectoryModes() error: %v", err)
	}

	assertTestPathMode(t, staging.artifactRoot, 0o750)
	assertTestPathMode(t, filepath.Join(staging.artifactRoot, "nested"), 0o711)
	assertTestPathMode(t, filepath.Join(staging.artifactRoot, "SKILL.md"), 0o640)
	assertTestPathMode(t, filepath.Join(staging.artifactRoot, "nested", "tool.sh"), 0o751)
}

func assertTestPathMode(t *testing.T, path string, expected os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%q) error: %v", path, err)
	}
	if info.Mode().Perm() != expected.Perm() {
		t.Fatalf("mode(%q) = %#o, want %#o", path, info.Mode().Perm(), expected.Perm())
	}
}
