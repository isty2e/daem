package mutation

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMutationTestFile(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create test file parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

func mkdirMutationTest(path string) error {
	return os.MkdirAll(path, 0o700)
}

func symlinkForMutationTest(oldname string, newname string) error {
	return os.Symlink(oldname, newname)
}
