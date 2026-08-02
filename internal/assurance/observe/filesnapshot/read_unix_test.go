//go:build darwin || linux

package filesnapshot_test

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/filesnapshot"
	"golang.org/x/sys/unix"
)

func TestReadRegularFileRejectsUnixSpecialFiles(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "daem-filesnapshot-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	fifo := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}

	socket := filepath.Join(root, "socket")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	for _, path := range []string{fifo, socket, "/dev/null"} {
		if _, _, err := filesnapshot.ReadRegularFile(path, 64); !errors.Is(err, filesnapshot.ErrNotRegular) {
			t.Fatalf("ReadRegularFile(%q) error = %v, want ErrNotRegular", path, err)
		}
	}
}
