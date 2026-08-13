//go:build darwin || linux

package declarationartifact_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/filesnapshot"
	"golang.org/x/sys/unix"
)

func TestReadRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fifo := filepath.Join(root, "config")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(root, "selected")
	if err := os.Symlink(fifo, selected); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"direct":        fifo,
		"final symlink": selected,
	} {
		t.Run(name, func(t *testing.T) {
			result := make(chan error, 1)
			go func() {
				_, err := declarationartifact.Read(context.Background(), path)
				result <- err
			}()
			select {
			case err := <-result:
				if !errors.Is(err, filesnapshot.ErrNotRegular) {
					t.Fatalf("Read error = %v, want ErrNotRegular", err)
				}
			case <-time.After(time.Second):
				t.Fatal("Read waited for a FIFO writer")
			}
		})
	}
}
