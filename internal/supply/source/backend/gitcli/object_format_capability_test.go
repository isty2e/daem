package gitcli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
)

func TestGitInitHelpAdmitted(t *testing.T) {
	t.Parallel()

	if gitInitHelpAdmitted(subprocess.CommandAttemptResult{}) {
		t.Fatal("zero result must not be admitted")
	}
	if gitInitHelpAdmitted(failedGitHelpResult(t, 127, "sleep: not found")) {
		t.Fatal("unrelated nonzero help exit was admitted")
	}
	if !gitInitHelpAdmitted(failedGitHelpResult(t, gitHelpUsageExitCode, "usage: git init")) {
		t.Fatal("git help usage exit was rejected")
	}
}

func TestExplicitObjectFormatSupportPreservesCallerDeadline(t *testing.T) {
	installStallingGitInitHelp(t)

	resolver, err := NewResolver(t.TempDir())
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	_, err = resolver.explicitObjectFormatSupported(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Fatalf("caller deadline was classified as executor timeout: %v", err)
	}
}

func TestExplicitObjectFormatSupportWaitersReceiveLeaderError(t *testing.T) {
	started, countPath, releasePath := installFailingGitInitHelpAfterStart(t)

	resolver, err := NewResolver(t.TempDir())
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	leaderCtx, leaderCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer leaderCancel()
	leaderDone := make(chan error, 1)
	go func() {
		_, leaderErr := resolver.explicitObjectFormatSupported(leaderCtx)
		leaderDone <- leaderErr
	}()
	waitForFile(t, started, 10*time.Second, leaderDone)

	const waiters = 4
	waiterDone := make(chan error, waiters)
	for range waiters {
		go func() {
			_, waiterErr := resolver.explicitObjectFormatSupported(context.Background())
			waiterDone <- waiterErr
		}()
	}
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(releasePath, []byte("release\n"), 0o600); err != nil {
		t.Fatalf("release capability probe: %v", err)
	}

	returned := make([]error, 0, waiters+1)
	select {
	case err = <-leaderDone:
		returned = append(returned, err)
	case <-time.After(5 * time.Second):
		t.Fatal("leader did not return")
	}
	for i := 0; i < waiters; i++ {
		select {
		case err = <-waiterDone:
			returned = append(returned, err)
		case <-time.After(5 * time.Second):
			t.Fatal("waiter did not return")
		}
	}
	for _, err := range returned {
		if err == nil {
			t.Fatal("expected capability probe error")
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiter or leader returned cancellation: %v", err)
		}
	}
	if probes := countGitHelpProbes(t, countPath); probes != 1 {
		t.Fatalf("git init -h probes = %d, want 1 for the current generation", probes)
	}

	_, retryErr := resolver.explicitObjectFormatSupported(context.Background())
	if retryErr == nil {
		t.Fatal("independent retry expected a probe error")
	}
	if probes := countGitHelpProbes(t, countPath); probes != 2 {
		t.Fatalf("git init -h probes = %d, want 2 after an independent retry", probes)
	}
}

func TestExplicitObjectFormatSupportObservesWaiterCancellation(t *testing.T) {
	started := installStallingGitInitHelp(t)

	resolver, err := NewResolver(t.TempDir())
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	leaderCtx, leaderCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer leaderCancel()
	done := make(chan error, 1)
	go func() {
		_, leaderErr := resolver.explicitObjectFormatSupported(leaderCtx)
		done <- leaderErr
	}()

	waitForFile(t, started, 10*time.Second, done)
	waiterCtx, waiterCancel := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, waiterErr := resolver.explicitObjectFormatSupported(waiterCtx)
		waiterDone <- waiterErr
	}()
	time.Sleep(50 * time.Millisecond)
	waiterCancel()
	select {
	case err = <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled waiter did not observe cancellation")
	}
	leaderCancel()
	<-done
}

func failedGitHelpResult(t *testing.T, exitCode int, stderr string) subprocess.CommandAttemptResult {
	t.Helper()
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Timeout: time.Second,
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			return subprocess.CommandResult{
				Started:     true,
				HasExitCode: true,
				ExitCode:    exitCode,
				Stderr:      stderr,
			}
		},
	})
	return executor.Execute(context.Background(), subprocess.CommandAttemptRequest{
		Command: gitExecutable,
		Args:    inspectGitInitHelpArgs(),
	})
}

func installStallingGitInitHelp(t *testing.T) string {
	t.Helper()
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("sleep helper is unavailable: %v", err)
	}
	if strings.ContainsAny(sleepPath, " \t\n'\"$") {
		t.Fatalf("sleep helper path is not shell-safe: %q", sleepPath)
	}

	started := filepath.Join(t.TempDir(), "started")
	binRoot := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	wrapper := "#!/bin/sh\n" +
		"if [ \"$1\" = \"init\" ] && [ \"$2\" = \"-h\" ]; then\n" +
		"  : > " + strconv.Quote(started) + "\n" +
		"  exec " + sleepPath + " 30\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(binRoot, gitExecutable), []byte(wrapper), 0o700); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("PATH", binRoot+string(os.PathListSeparator)+os.Getenv("PATH"))
	return started
}

func installFailingGitInitHelpAfterStart(t *testing.T) (string, string, string) {
	t.Helper()
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("sleep helper is unavailable: %v", err)
	}
	if strings.ContainsAny(sleepPath, " \t\n'\"$") {
		t.Fatalf("sleep helper path is not shell-safe: %q", sleepPath)
	}

	started := filepath.Join(t.TempDir(), "started")
	countPath := filepath.Join(t.TempDir(), "probes")
	releasePath := filepath.Join(t.TempDir(), "release")
	binRoot := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binRoot, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	wrapper := "#!/bin/sh\n" +
		"if [ \"$1\" = \"init\" ] && [ \"$2\" = \"-h\" ]; then\n" +
		"  printf '.\\n' >> " + strconv.Quote(countPath) + "\n" +
		"  : > " + strconv.Quote(started) + "\n" +
		"  while [ ! -e " + strconv.Quote(releasePath) + " ]; do " + sleepPath + " 0.01; done\n" +
		"  exit 1\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(binRoot, gitExecutable), []byte(wrapper), 0o700); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("PATH", binRoot+string(os.PathListSeparator)+os.Getenv("PATH"))
	return started, countPath, releasePath
}

func countGitHelpProbes(t *testing.T, path string) int {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read git help probe count: %v", err)
	}
	return strings.Count(string(content), "\n")
}

func waitForFile(t *testing.T, path string, timeout time.Duration, leader <-chan error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case err := <-leader:
			t.Fatalf("leader returned before creating %s: %v", path, err)
		case <-time.After(5 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting for %s", path)
}
