//go:build darwin || linux

package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const cacheLockHelperEnvironment = "DAEM_CACHE_LOCK_HELPER"

func TestAdvisoryLockReleasesAfterHolderProcessDeath(t *testing.T) {
	root := t.TempDir()
	readyPath := filepath.Join(t.TempDir(), "ready")
	command := exec.Command(
		os.Args[0],
		"-test.run=^TestCacheLockHelperProcess$",
		"--",
		root,
		readyPath,
	)
	command.Env = append(os.Environ(), cacheLockHelperEnvironment+"=1")
	if err := command.Start(); err != nil {
		t.Fatalf("start cache lock helper: %v", err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	waitForCacheLockHelper(t, readyPath)

	key := mustKey(t, "cache-lock", "process-death")
	blockedContext, cancelBlocked := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, err := NewLocker(root).Acquire(blockedContext, key)
	cancelBlocked()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire while helper owns lock = %v, want deadline", err)
	}

	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill cache lock helper: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("killed cache lock helper exited successfully")
	}

	retryContext, cancelRetry := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelRetry()
	lock, err := NewLocker(root).Acquire(retryContext, key)
	if err != nil {
		t.Fatalf("Acquire after helper death: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release lock acquired after helper death: %v", err)
	}
}

func TestCacheLockHelperProcess(t *testing.T) {
	if os.Getenv(cacheLockHelperEnvironment) != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || len(os.Args) != separator+3 {
		t.Fatalf("cache lock helper arguments = %q", os.Args)
	}
	root := os.Args[separator+1]
	readyPath := os.Args[separator+2]
	key, err := NewKey("cache-lock", "process-death")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := NewLocker(root).Acquire(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if err := os.WriteFile(readyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(24 * time.Hour)
}

func waitForCacheLockHelper(t *testing.T, readyPath string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(readyPath); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect cache lock helper readiness: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("cache lock helper did not create %q", readyPath))
}
