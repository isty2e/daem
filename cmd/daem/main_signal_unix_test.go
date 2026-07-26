//go:build darwin || linux

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

const signalLifecycleHelperMode = "DAEM_SIGNAL_LIFECYCLE_HELPER"

func TestSignalLifecyclePreservesFirstSignalExitCode(t *testing.T) {
	tests := []struct {
		name     string
		signal   os.Signal
		wantExit int
	}{
		{name: "SIGINT", signal: os.Interrupt, wantExit: 130},
		{name: "SIGTERM", signal: syscall.SIGTERM, wantExit: 143},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd, lines := startSignalLifecycleHelper(t, "graceful")
			waitForSignalLifecycleLine(t, lines, "ready")
			if err := cmd.Process.Signal(test.signal); err != nil {
				t.Fatalf("Signal: %v", err)
			}
			waitForSignalLifecycleLine(t, lines, "canceled")
			assertSignalLifecycleExit(t, cmd, test.wantExit)
		})
	}
}

func TestSignalLifecycleSecondSignalForcesFirstSignalExitCode(t *testing.T) {
	tests := []struct {
		name     string
		first    os.Signal
		second   os.Signal
		wantExit int
	}{
		{name: "SIGINT then SIGTERM", first: os.Interrupt, second: syscall.SIGTERM, wantExit: 130},
		{name: "SIGTERM then SIGINT", first: syscall.SIGTERM, second: os.Interrupt, wantExit: 143},
		{name: "SIGINT then SIGINT", first: os.Interrupt, second: os.Interrupt, wantExit: 130},
		{name: "SIGTERM then SIGTERM", first: syscall.SIGTERM, second: syscall.SIGTERM, wantExit: 143},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cmd, lines := startSignalLifecycleHelper(t, "force")
			waitForSignalLifecycleLine(t, lines, "ready")
			if err := cmd.Process.Signal(test.first); err != nil {
				t.Fatalf("first Signal: %v", err)
			}
			waitForSignalLifecycleLine(t, lines, "canceled")
			if err := cmd.Process.Signal(test.second); err != nil {
				t.Fatalf("second Signal: %v", err)
			}
			assertSignalLifecycleExit(t, cmd, test.wantExit)
		})
	}
}

func TestSignalLifecycleHelperProcess(t *testing.T) {
	mode := os.Getenv(signalLifecycleHelperMode)
	if mode == "" {
		return
	}
	exitCode := runWithSignalLifecycle(func(ctx context.Context) int {
		fmt.Println("ready")
		if mode == "force-tree" || mode == "mcp-tree" {
			return runSignalLifecycleTreeMode(ctx, mode)
		}
		<-ctx.Done()
		fmt.Println("canceled")
		if mode == "force" {
			for {
				time.Sleep(time.Hour)
			}
		}
		return 1
	})
	os.Exit(exitCode)
}

func startSignalLifecycleHelper(t *testing.T, mode string) (*exec.Cmd, <-chan string) {
	return startSignalLifecycleHelperWithEnv(t, mode)
}

func startSignalLifecycleHelperWithEnv(t *testing.T, mode string, extraEnv ...string) (*exec.Cmd, <-chan string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=TestSignalLifecycleHelperProcess")
	cmd.Env = append(os.Environ(), signalLifecycleHelperMode+"="+mode)
	cmd.Env = append(cmd.Env, extraEnv...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	lines := make(chan string, 4)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	return cmd, lines
}

func waitForSignalLifecycleLine(t *testing.T, lines <-chan string, want string) {
	t.Helper()
	select {
	case line, ok := <-lines:
		if !ok || line != want {
			t.Fatalf("line = %q/%t, want %q", line, ok, want)
		}
	case <-time.After(subprocessTestTimeout):
		t.Fatalf("timed out waiting for %q", want)
	}
}

func assertSignalLifecycleExit(t *testing.T, cmd *exec.Cmd, want int) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != want {
			t.Fatalf("Wait error = %v, want exit %d", err, want)
		}
	case <-time.After(subprocessTestTimeout):
		_ = cmd.Process.Kill()
		t.Fatalf("timed out waiting for exit %d", want)
	}
}
