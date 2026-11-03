package executioncontext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxIdentitySourcesRequireBoundedRegularFiles(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "identity")
	identity := strings.Repeat("a", 32) + "\n"
	if err := os.WriteFile(regular, []byte(identity), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readExecutionIdentityFile(regular, false)
	if err != nil || got != identity {
		t.Fatal("regular identity source failed", err)
	}
	symlink := filepath.Join(root, "symlink")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatal(err)
	}
	oversized := filepath.Join(root, "oversized")
	if err := os.WriteFile(oversized, []byte(strings.Repeat("a", 65)), 0o600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{root, symlink, oversized, fifo} {
		if content, err := readExecutionIdentityFile(input, false); err == nil || content != "" {
			t.Fatal("nonregular or oversized source produced identity data")
		}
	}
	if _, err := readExecutionIdentityFile(regular, true); err == nil {
		t.Fatal("ordinary file qualified as a kernel boot source")
	}
}
