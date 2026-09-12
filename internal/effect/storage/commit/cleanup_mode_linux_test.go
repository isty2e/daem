//go:build linux

package commit

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPreparedTreeCleanupChmodRetainsExactEntry(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "entry")
			if directory {
				if err := os.Mkdir(path, 0); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("preserved"), 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, 0o700) })
			expected := captureIdentity(t, path)
			parent, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(parent)
			if err := chmodPreparedTreeEntryForCleanup(parent, "entry", expected, 0o700); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o700 {
				t.Fatalf("cleanup mode = %v, %v", info, err)
			}
			if !directory {
				assertFile(t, path, "preserved", 0o700)
			}
		})
	}
}

func TestPreparedTreeCleanupChmodRejectsReplacementAndSymlink(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		name := "replacement"
		if symlink {
			name = "symlink"
		}
		t.Run(name, func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "entry")
			writeTestFile(t, path, "planned", 0o600)
			expected := captureIdentity(t, path)
			if err := os.Rename(path, filepath.Join(root, "original")); err != nil {
				t.Fatal(err)
			}
			if symlink {
				if err := os.Symlink("original", path); err != nil {
					t.Fatal(err)
				}
			} else {
				writeTestFile(t, path, "replacement", 0o600)
			}
			parent, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(parent)
			if err := chmodPreparedTreeEntryForCleanup(parent, "entry", expected, 0o700); err == nil {
				t.Fatal("cleanup chmod accepted a replacement")
			}
			assertFile(t, filepath.Join(root, "original"), "planned", 0o600)
			if !symlink {
				assertFile(t, path, "replacement", 0o600)
			}
		})
	}
}
