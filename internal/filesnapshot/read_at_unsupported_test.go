//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !windows

package filesnapshot_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/filesnapshot"
)

func TestReadRegularFileAtCountedFailsClosedWithoutPathnameReopen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dirPath := filepath.Join(root, "plugin")
	if err := os.Mkdir(dirPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "plugin.json"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })

	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "plugin.json"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dirPath, dirPath+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(outside, dirPath); err != nil {
		t.Fatal(err)
	}

	counted, err := filesnapshot.ReadRegularFileAtCounted(t.Context(), dir, "plugin.json", 64)
	if !errors.Is(err, filesnapshot.ErrUnsupported) {
		t.Fatalf("ReadRegularFileAtCounted after path replacement = %+v, %v, want ErrUnsupported", counted, err)
	}
	if counted.Exists || counted.Attempted != 0 || len(counted.Content) != 0 {
		t.Fatalf("unsupported snapshot observation = %+v, want zero CountedContent", counted)
	}
	if string(counted.Content) == "outside" || string(counted.Content) == "inside" {
		t.Fatalf("unsupported snapshot leaked file bytes = %q", counted.Content)
	}
}
