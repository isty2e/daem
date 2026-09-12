//go:build darwin || linux

// Package fsclock provides filesystem-clock prerequisites for mutation tests.
package fsclock

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// WaitForTick waits until a separate probe observes a ctime different from path.
// It requires Linux or Darwin; unsupported platforms fail explicitly.
// Call before injecting a change whose rejection depends on timestamp evidence.
// The fixture and probe must share a filesystem. The probe does not write the
// referenced fixture, but allocating its temporary directory can change shared
// ancestors. A non-progressing clock skips the dependent test, not a failed
// production assertion.
func WaitForTick(t testing.TB, path string) {
	t.Helper()
	var reference unix.Stat_t
	if err := unix.Lstat(path, &reference); err != nil {
		t.Fatalf("inspect clock reference: %v", err)
	}
	probe := filepath.Join(t.TempDir(), "clock")
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := os.WriteFile(probe, []byte("tick"), 0o600); err != nil {
			t.Fatalf("write clock probe: %v", err)
		}
		var current unix.Stat_t
		if err := unix.Stat(probe, &current); err != nil {
			t.Fatalf("inspect clock probe: %v", err)
		}
		if current.Dev != reference.Dev {
			t.Fatal("clock probe and fixture must share a filesystem; select the fixture filesystem with TMPDIR")
		}
		if current.Ctim != reference.Ctim {
			return
		}
		if time.Now().After(deadline) {
			t.Skip("independent filesystem probe ctime did not advance within 3s; timestamp-change prerequisite unavailable")
		}
		time.Sleep(time.Millisecond)
	}
}
