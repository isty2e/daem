//go:build darwin || linux

package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectWorkingDirectoryBindingRejectsSelectedAliasRetarget(t *testing.T) {
	parent := t.TempDir()
	first := filepath.Join(parent, "first")
	second := filepath.Join(parent, "second")
	selected := filepath.Join(parent, "selected")
	for _, directory := range []string{first, second} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}
	if err := os.Symlink(first, selected); err != nil {
		t.Fatalf("create selected-root alias: %v", err)
	}

	binding, err := projectWorkingDirectoryBinder(selected)()
	if err != nil {
		t.Fatalf("acquire selected-root binding: %v", err)
	}
	defer binding.Close()
	if err := binding.Validate(); err != nil {
		t.Fatalf("validate unchanged selected-root binding: %v", err)
	}

	if err := os.Remove(selected); err != nil {
		t.Fatalf("remove selected-root alias: %v", err)
	}
	if err := os.Symlink(second, selected); err != nil {
		t.Fatalf("retarget selected-root alias: %v", err)
	}
	if err := binding.Validate(); err == nil {
		t.Fatal("Validate accepted a selected-root alias retarget")
	}
}
