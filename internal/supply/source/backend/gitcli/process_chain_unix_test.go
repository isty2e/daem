//go:build darwin || linux

package gitcli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestGitChainHelperWaitsForOwnSignal(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "pids")
	command := gitProcessHelperCommand(t, ctx, "chain-parent", pidFile)
	result := make(chan error, 1)
	go func() {
		_, err := runGitOutput(ctx, command)
		result <- err
	}()
	pids := waitForGitHelperPIDs(t, pidFile, 3)

	// Deliver the descendant's signal first: group-wide delivery need not
	// let the leader handle its own signal before it reaps a terminated child.
	for _, pid := range pids[1:] {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
waitForReap:
	for {
		select {
		case err := <-result:
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				t.Fatalf("chain exited before its own signal: %v (exit code %d)", err, exitErr.ExitCode())
			}
			t.Fatalf("chain exited before its own signal: %v", err)
		case <-ticker.C:
			data, err := os.ReadFile(pidFile + ".reaped")
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if slices.Contains(strings.Fields(string(data)), strconv.Itoa(pids[0])) {
				break waitForReap
			}
		case <-timeout.C:
			t.Fatal("chain leader did not acknowledge reaping its child")
		}
	}
	assertGitHelperProcessAlive(t, pids[0])

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("runGitOutput error = %v, want context.Canceled", err)
		}
		if err != nil && strings.Contains(err.Error(), "process-group members remained") {
			t.Errorf("runGitOutput error = %v, want no residual members", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("runGitOutput did not return after cancellation")
	}
	assertGitHelperProcessesGone(t, pids)
}
