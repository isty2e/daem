//go:build linux

package diagnose

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/isty2e/daem/internal/findings"
)

func TestDirectoryCheckDetectsUnsupportedInheritedMetadata(t *testing.T) {
	root := t.TempDir()
	// Linux UAPI posix_acl_xattr: version 2; default user/group/other entries.
	acl := []byte{
		2, 0, 0, 0,
		1, 0, 7, 0, 255, 255, 255, 255,
		4, 0, 5, 0, 255, 255, 255, 255,
		32, 0, 5, 0, 255, 255, 255, 255,
	}
	if err := unix.Setxattr(root, "system.posix_acl_default", acl, 0); err != nil {
		if errors.Is(err, unix.EOPNOTSUPP) {
			t.Skipf("default ACLs unavailable: %v", err)
		}
		t.Fatal(err)
	}
	file, err := os.CreateTemp(root, "ordinary-")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	check := directoryCheck("storage", root)
	if check.Status != findings.CheckError {
		t.Fatalf("writable but metadata-incompatible path = %#v, want error", check)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(file.Name()) {
		t.Fatalf("doctor changed existing entries or left scratch artifacts: %v", entries)
	}
	observed := make([]byte, len(acl))
	if _, err := unix.Getxattr(root, "system.posix_acl_default", observed); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(acl, observed) {
		t.Fatal("doctor changed the directory's default ACL")
	}
}

func TestDirectoryCheckCleansSuccessfulScratchProbe(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "untouched")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	check := directoryCheck("storage", root)
	if check.Status != findings.CheckOK {
		t.Fatalf("storage probe = %#v, want success", check)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "untouched" {
		t.Fatalf("doctor changed existing entries or left scratch artifacts: %v", entries)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "original" {
		t.Fatalf("existing file = %q, %v", content, err)
	}
}
