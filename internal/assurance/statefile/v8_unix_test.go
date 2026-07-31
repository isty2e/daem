//go:build darwin || linux

package statefile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"golang.org/x/sys/unix"
)

func TestLoadRequiresPrivateIdentityStableRegularFile(t *testing.T) {
	content, err := Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("private regular file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		writeStatefileFixture(t, path, content, statefileMode)

		snapshot, err := Load(t.Context(), path)
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}
		if !snapshot.Equal(durable.EmptySnapshot()) {
			t.Fatal("Load returned non-empty state")
		}
	})

	t.Run("permissive mode", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		writeStatefileFixture(t, path, content, 0o644)

		_, err := Load(t.Context(), path)
		if err == nil || !strings.Contains(err.Error(), "permissions are 0644, want 0600") {
			t.Fatalf("Load error = %v, want private-mode rejection", err)
		}
	})

	t.Run("noncanonical private mode", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		writeStatefileFixture(t, path, content, 0o400)

		_, err := Load(t.Context(), path)
		if err == nil || !strings.Contains(err.Error(), "permissions are 0400, want 0600") {
			t.Fatalf("Load error = %v, want exact private-mode rejection", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}

		if _, err := Load(t.Context(), path); err == nil {
			t.Fatal("Load accepted a directory")
		}
	})

	t.Run("final symlink", func(t *testing.T) {
		root := t.TempDir()
		referent := filepath.Join(root, "referent.json")
		writeStatefileFixture(t, referent, content, statefileMode)
		path := filepath.Join(root, "state.json")
		if err := os.Symlink(referent, path); err != nil {
			t.Fatal(err)
		}

		if _, err := Load(t.Context(), path); err == nil {
			t.Fatal("Load followed a final symlink")
		}
	})

	t.Run("canonical ancestor alias", func(t *testing.T) {
		root := t.TempDir()
		referentRoot := filepath.Join(root, "referent")
		if err := os.Mkdir(referentRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		writeStatefileFixture(t, filepath.Join(referentRoot, "state.json"), content, statefileMode)
		alias := filepath.Join(root, "alias")
		if err := os.Symlink(referentRoot, alias); err != nil {
			t.Fatal(err)
		}

		snapshot, err := Load(t.Context(), filepath.Join(alias, "state.json"))
		if err != nil {
			t.Fatalf("Load returned error for canonical ancestor alias: %v", err)
		}
		if !snapshot.Equal(durable.EmptySnapshot()) {
			t.Fatal("Load returned non-empty state through canonical ancestor alias")
		}
	})

	t.Run("fifo", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := unix.Mkfifo(path, uint32(statefileMode)); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() {
			_, loadErr := Load(t.Context(), path)
			result <- loadErr
		}()

		select {
		case err := <-result:
			if err == nil {
				t.Fatal("Load accepted a FIFO")
			}
		case <-time.After(5 * time.Second):
			if descriptor, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
				_ = unix.Close(descriptor)
			}
			t.Fatal("Load blocked while opening a FIFO")
		}
	})

	t.Run("caller cancellation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		writeStatefileFixture(t, path, content, statefileMode)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := Load(ctx, path)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Load error = %v, want context cancellation", err)
		}
	})

	t.Run("foreign owner", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("changing fixture ownership requires root")
		}
		path := filepath.Join(t.TempDir(), "state.json")
		writeStatefileFixture(t, path, content, statefileMode)
		if err := os.Chown(path, 1, -1); err != nil {
			t.Fatal(err)
		}

		_, err := Load(t.Context(), path)
		if err == nil || !strings.Contains(err.Error(), "not owned by the invoking user") {
			t.Fatalf("Load error = %v, want foreign-owner rejection", err)
		}
	})
}

func writeStatefileFixture(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, content, statefileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
