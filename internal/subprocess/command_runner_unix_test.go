//go:build darwin || linux

package subprocess

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDefaultRunnerTimeoutTerminatesGrandchild(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	executor := NewCommandExecutor(CommandOptions{Timeout: time.Second, OutputLimit: 1024})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args: []string{
			"-test.run=TestCommandExecProcessTreeHelper",
			"--",
			"parent",
			readyPath,
		},
	})

	if result.Reason() != CommandReasonTimeout || !result.Started() || !result.TimedOut() {
		t.Fatalf("result = %#v, want started timeout", result)
	}
	assertCommandExecProcessesGone(t, readCommandExecPIDs(t, readyPath))
}

func TestDefaultRunnerCancellationTerminatesGrandchild(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	executor := NewCommandExecutor(CommandOptions{Timeout: 2 * DefaultCommandTimeout, OutputLimit: 1024})
	ctx, cancel := context.WithCancel(context.Background())
	resultDone := make(chan CommandAttemptResult, 1)
	go func() {
		resultDone <- executor.executeWithoutWorkingDirectory(ctx, CommandAttemptRequest{
			Command: executable,
			Args: []string{
				"-test.run=TestCommandExecProcessTreeHelper",
				"--",
				"parent",
				readyPath,
			},
		})
	}()
	if err := waitForCommandExecFile(readyPath, DefaultCommandTimeout); err != nil {
		cancel()
		t.Fatal(err)
	}
	pids := readCommandExecPIDs(t, readyPath)
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	cancel()
	var result CommandAttemptResult
	select {
	case result = <-resultDone:
	case <-time.After(DefaultCommandTimeout):
		t.Fatal("timed out waiting for canceled command tree")
	}

	if result.Reason() != CommandReasonCanceled || !result.Started() || !result.Canceled() {
		t.Fatalf("result = %#v, want started cancellation", result)
	}
	assertCommandExecProcessesGone(t, pids)
}

func TestDefaultRunnerRejectsSuccessfulLeaderWithResidualGrandchild(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	executor := NewCommandExecutor(CommandOptions{Timeout: DefaultCommandTimeout, OutputLimit: 1024})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args: []string{
			"-test.run=TestCommandExecProcessTreeHelper",
			"--",
			"parent-exit",
			readyPath,
		},
	})

	if result.Reason() != CommandReasonRunnerError || !result.Started() {
		t.Fatalf("result = %#v, want residual-process runner failure", result)
	}
	if exitCode, ok := result.ExitCode(); !ok || exitCode != 0 {
		t.Fatalf("exit code = %d/%t, want observed direct-child exit 0", exitCode, ok)
	}
	if !strings.Contains(result.ErrorDetail(), "descendant processes remained") {
		t.Fatalf("error detail = %q", result.ErrorDetail())
	}
	pids := readCommandExecPIDs(t, readyPath)
	assertCommandExecProcessesGone(t, pids[1:])
}

func TestDefaultRunnerDoesNotWaitForTimeoutWhenGrandchildHoldsOutputDescriptors(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	executor := NewCommandExecutor(CommandOptions{Timeout: 5 * time.Second, OutputLimit: 1024})

	startedAt := time.Now()
	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args: []string{
			"-test.run=TestCommandExecProcessTreeHelper",
			"--",
			"parent-exit-inherited-output",
			readyPath,
		},
	})
	elapsed := time.Since(startedAt)

	if result.Reason() != CommandReasonRunnerError || !result.Started() || result.TimedOut() {
		t.Fatalf("result = %#v, want prompt residual-process runner failure", result)
	}
	if exitCode, ok := result.ExitCode(); !ok || exitCode != 0 {
		t.Fatalf("exit code = %d/%t, want observed direct-child exit 0", exitCode, ok)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("residual output descriptor cleanup took %s, want less than 2s", elapsed)
	}
	if !strings.Contains(result.ErrorDetail(), "inherited output descriptors open") {
		t.Fatalf("error detail = %q", result.ErrorDetail())
	}
	if result.Stdout() != "leader stdout\n" || result.Stderr() != "leader stderr\n" {
		t.Fatalf("captured output = %q/%q", result.Stdout(), result.Stderr())
	}
	if !result.StdoutTruncated() || !result.StderrTruncated() {
		t.Fatalf(
			"truncation flags = %t/%t, want conservative incomplete capture",
			result.StdoutTruncated(),
			result.StderrTruncated(),
		)
	}
	pids := readCommandExecPIDs(t, readyPath)
	t.Cleanup(func() {
		for _, pid := range pids[1:] {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	assertCommandExecProcessesGone(t, pids[1:])
}

func TestDefaultRunnerRedactsSecretPrefixCutByInheritedOutputDescriptorCleanup(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	const secret = "command-exec-secret-value"
	executor := NewCommandExecutor(CommandOptions{
		Timeout:     5 * time.Second,
		OutputLimit: 1024,
		LookupEnv: func(name string) (string, bool) {
			if name == "SOURCE_SECRET" {
				return secret, true
			}
			return "", false
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args: []string{
			"-test.run=TestCommandExecProcessTreeHelper",
			"--",
			"parent-exit-secret-prefix-inherited-output",
			readyPath,
		},
		EnvRefs: []CommandEnvRef{{
			Name:       "DAEM_TEST_SECRET",
			SourceName: "SOURCE_SECRET",
		}},
	})

	if result.Reason() != CommandReasonRunnerError || !result.Redacted() || !result.StdoutTruncated() {
		t.Fatalf("result = %#v, want redacted incomplete residual-process result", result)
	}
	if result.Stdout() != "[REDACTED]" || strings.Contains(result.Stdout(), secret[:len(secret)-6]) {
		t.Fatalf("stdout = %q, want redacted secret prefix", result.Stdout())
	}
	pids := readCommandExecPIDs(t, readyPath)
	t.Cleanup(func() {
		for _, pid := range pids[1:] {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	assertCommandExecProcessesGone(t, pids[1:])
}

func TestDefaultRunnerPreservesNonZeroExitWhenGrandchildHoldsOutputDescriptors(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	executor := NewCommandExecutor(CommandOptions{Timeout: 5 * time.Second, OutputLimit: 1024})

	startedAt := time.Now()
	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args: []string{
			"-test.run=TestCommandExecProcessTreeHelper",
			"--",
			"parent-fail-inherited-output",
			readyPath,
		},
	})

	if result.Reason() != CommandReasonNonZeroExit || result.TimedOut() {
		t.Fatalf("result = %#v, want prompt nonzero result with residual cleanup", result)
	}
	if exitCode, ok := result.ExitCode(); !ok || exitCode != 17 {
		t.Fatalf("exit code = %d/%t, want 17", exitCode, ok)
	}
	if !result.StdoutTruncated() || !result.StderrTruncated() {
		t.Fatalf(
			"truncation flags = %t/%t, want incomplete forcibly terminated command tree",
			result.StdoutTruncated(),
			result.StderrTruncated(),
		)
	}
	if elapsed := time.Since(startedAt); elapsed >= 2*time.Second {
		t.Fatalf("residual output descriptor cleanup took %s, want less than 2s", elapsed)
	}
	pids := readCommandExecPIDs(t, readyPath)
	t.Cleanup(func() {
		for _, pid := range pids[1:] {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	assertCommandExecProcessesGone(t, pids[1:])
}

func TestDefaultRunnerAllowsInheritedOutputGrandchildToClosePromptly(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	executor := NewCommandExecutor(CommandOptions{Timeout: 5 * time.Second, OutputLimit: 1024})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args: []string{
			"-test.run=TestCommandExecProcessTreeHelper",
			"--",
			"parent-exit-short-inherited-output",
			readyPath,
		},
	})

	if result.Reason() != CommandReasonNone || !result.Succeeded() {
		t.Fatalf("result = %#v, want successful natural descendant quiescence", result)
	}
	if result.StdoutTruncated() || result.StderrTruncated() {
		t.Fatalf(
			"truncation flags = %t/%t, want complete natural output closure",
			result.StdoutTruncated(),
			result.StderrTruncated(),
		)
	}
	pids := readCommandExecPIDs(t, readyPath)
	assertCommandExecProcessesGone(t, pids[1:])
}

func TestCommandExecProcessTreeHelper(t *testing.T) {
	args := argsAfterDoubleDash(os.Args)
	if len(args) < 2 {
		return
	}
	mode := args[0]
	readyPath := args[1]

	switch mode {
	case "child", "child-short":
		if err := writeCommandExecHelperFile(readyPath+".child", []byte(strconv.Itoa(os.Getpid()))); err != nil {
			os.Exit(81)
		}
		if mode == "child-short" {
			time.Sleep(25 * time.Millisecond)
			os.Exit(0)
		}
		for {
			time.Sleep(time.Hour)
		}
	case "parent",
		"parent-exit",
		"parent-exit-inherited-output",
		"parent-exit-secret-prefix-inherited-output",
		"parent-fail-inherited-output",
		"parent-exit-short-inherited-output":
		executable, err := os.Executable()
		if err != nil {
			os.Exit(82)
		}
		childMode := "child"
		if mode == "parent-exit-short-inherited-output" {
			childMode = "child-short"
		}
		child := exec.Command(executable, "-test.run=TestCommandExecProcessTreeHelper", "--", childMode, readyPath)
		if strings.Contains(mode, "inherited-output") {
			child.Stdout = os.Stdout
			child.Stderr = os.Stderr
		}
		if err := child.Start(); err != nil {
			os.Exit(83)
		}
		if err := waitForCommandExecFile(readyPath+".child", 5*time.Second); err != nil {
			_ = child.Process.Kill()
			os.Exit(84)
		}
		content := fmt.Sprintf("%d,%d", os.Getpid(), child.Process.Pid)
		if err := writeCommandExecHelperFile(readyPath, []byte(content)); err != nil {
			_ = child.Process.Kill()
			os.Exit(85)
		}
		if strings.Contains(mode, "inherited-output") &&
			mode != "parent-exit-secret-prefix-inherited-output" {
			fmt.Fprintln(os.Stdout, "leader stdout")
			fmt.Fprintln(os.Stderr, "leader stderr")
		}
		if mode == "parent-exit-secret-prefix-inherited-output" {
			secret := os.Getenv("DAEM_TEST_SECRET")
			if len(secret) < 7 {
				os.Exit(87)
			}
			fmt.Fprint(os.Stdout, secret[:len(secret)-6])
		}
		if mode == "parent-fail-inherited-output" {
			os.Exit(17)
		}
		if mode != "parent" {
			os.Exit(0)
		}
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(86)
	}
}

func readCommandExecPIDs(t *testing.T, path string) []int {
	t.Helper()
	if err := waitForCommandExecFile(path, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSpace(string(content)), ",")
	if len(parts) != 2 {
		t.Fatalf("pid evidence = %q", content)
	}
	pids := make([]int, 0, len(parts))
	for _, part := range parts {
		pid, err := strconv.Atoi(part)
		if err != nil || pid <= 0 {
			t.Fatalf("pid evidence = %q", content)
		}
		pids = append(pids, pid)
	}
	return pids
}

func waitForCommandExecFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeCommandExecHelperFile(path string, content []byte) error {
	temporaryPath := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(temporaryPath, content, 0o600); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func assertCommandExecProcessesGone(t *testing.T, pids []int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		allGone := true
		for _, pid := range pids {
			if err := unix.Kill(pid, 0); err == nil || err == unix.EPERM {
				allGone = false
				break
			} else if err != unix.ESRCH {
				t.Fatalf("probe pid %d: %v", pid, err)
			}
		}
		if allGone {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("processes still alive: %v", pids)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
