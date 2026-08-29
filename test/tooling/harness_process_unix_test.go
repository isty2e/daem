//go:build unix

package tooling

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRepositoryGoTestPackageWrapperForwardsTerminationToProcessGroup(t *testing.T) {
	root := findRepoRoot(t)
	wrapper := filepath.Join(root, "tools", "test-exec.sh")
	testRoot := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "pids")
	command := exec.Command(
		wrapper,
		"sh",
		"-c",
		`trap '' TERM; printf '%s\n' "$$" > "$DAEM_TEST_PID_FILE"; sleep 60 & printf '%s\n' "$!" >> "$DAEM_TEST_PID_FILE"; wait`,
	)
	command.Env = append(
		withoutEnvironment(os.Environ(), "DAEM_TEST_ROOT", "DAEM_TEST_PID_FILE"),
		"DAEM_TEST_HARNESS=1",
		"DAEM_TEST_ROOT="+testRoot,
		"DAEM_TEST_PID_FILE="+pidFile,
	)
	if err := command.Start(); err != nil {
		t.Fatalf("start package wrapper: %v", err)
	}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	})

	pids := waitForPIDFile(t, pidFile, 2)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("terminate package wrapper: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("repeat package wrapper termination: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 143 {
			t.Fatalf("terminated package wrapper result = %v, want exit 143", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("package wrapper did not exit after SIGTERM")
	}

	for _, pid := range pids {
		waitForProcessExit(t, pid)
	}
	entries, err := os.ReadDir(testRoot)
	if err != nil {
		t.Fatalf("read wrapper test root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("package wrapper retained temporary roots after SIGTERM: %v", entries)
	}
}

func waitForPIDFile(t *testing.T, path string, count int) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(path)
		if err == nil {
			lines := strings.Fields(string(content))
			if len(lines) == count {
				pids := make([]int, 0, count)
				for _, line := range lines {
					pid, parseErr := strconv.Atoi(line)
					if parseErr != nil {
						t.Fatalf("parse child pid %q: %v", line, parseErr)
					}
					pids = append(pids, pid)
				}
				return pids
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid file %q did not contain %d entries", path, count)
	return nil
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil {
			t.Fatalf("inspect process %d: %v", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("process %d remained after wrapper termination", pid))
}
