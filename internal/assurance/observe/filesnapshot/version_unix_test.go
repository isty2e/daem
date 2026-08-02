//go:build darwin || linux

package filesnapshot

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadRegularFileContextRejectsRewriteWithRestoredMtime(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = readRegularFileContext(context.Background(), path, 64, readHooks{
		afterOpen: func() {
			if writeErr := os.WriteFile(path, []byte("after!"), 0o600); writeErr != nil {
				t.Fatalf("rewrite observed file: %v", writeErr)
			}
			if timeErr := os.Chtimes(path, before.ModTime(), before.ModTime()); timeErr != nil {
				t.Fatalf("restore mtime: %v", timeErr)
			}
		},
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("rewrite error = %v, want ErrChanged", err)
	}
}

func TestReadRegularFileContextDoesNotBlockWhenFileBecomesFIFO(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		var transitionErr error
		_, _, err := readRegularFileContext(context.Background(), path, 64, readHooks{
			afterInspect: func() {
				if removeErr := os.Remove(path); removeErr != nil {
					transitionErr = removeErr
					return
				}
				if fifoErr := unix.Mkfifo(path, 0o600); fifoErr != nil {
					transitionErr = fifoErr
				}
			},
		})
		if transitionErr != nil {
			result <- transitionErr
			return
		}
		result <- err
	}()

	select {
	case err := <-result:
		if !errors.Is(err, ErrChanged) {
			t.Fatalf("FIFO replacement error = %v, want ErrChanged", err)
		}
	case <-time.After(time.Second):
		t.Fatal("regular-file snapshot blocked while opening a FIFO replacement")
	}
}

func TestReadRegularFileContextDoesNotFollowSymlinkReplacement(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("must-not-read"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := readRegularFileContext(context.Background(), path, 64, readHooks{
		afterInspect: func() {
			if removeErr := os.Remove(path); removeErr != nil {
				t.Fatalf("remove inspected file: %v", removeErr)
			}
			if symlinkErr := os.Symlink(target, path); symlinkErr != nil {
				t.Fatalf("replace inspected file with symlink: %v", symlinkErr)
			}
		},
	})
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("symlink replacement error = %v, want ErrChanged", err)
	}
}

func TestReadRegularFileContextClassifiesSocketReplacementAsChanged(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "daem-filesnapshot-replacement-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	path := filepath.Join(root, "source")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}

	var listener net.Listener
	_, _, err = readRegularFileContext(context.Background(), path, 64, readHooks{
		afterInspect: func() {
			if removeErr := os.Remove(path); removeErr != nil {
				t.Fatalf("remove inspected file: %v", removeErr)
			}
			listener, err = net.Listen("unix", path)
			if err != nil {
				t.Fatalf("replace inspected file with socket: %v", err)
			}
		},
	})
	if listener != nil {
		t.Cleanup(func() { _ = listener.Close() })
	}
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("socket replacement error = %v, want ErrChanged", err)
	}
}
