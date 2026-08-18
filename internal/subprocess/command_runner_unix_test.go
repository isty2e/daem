//go:build darwin || linux

package subprocess

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	if !strings.Contains(result.ErrorDetail(), "process-group members remained") {
		t.Fatalf("error detail = %q", result.ErrorDetail())
	}
	pids := readCommandExecPIDs(t, readyPath)
	assertCommandExecProcessesGone(t, pids[1:])
}

func TestDefaultRunnerAllowsSetsidChildToOutliveLeader(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	registerCommandExecEscapedChildCleanup(t, readyPath)
	executor := NewCommandExecutor(CommandOptions{Timeout: DefaultCommandTimeout, OutputLimit: 1024})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args: []string{
			"-test.run=TestCommandExecProcessTreeHelper",
			"--",
			"parent-exit-setsid",
			readyPath,
		},
	})

	if result.Reason() != CommandReasonNone || !result.Succeeded() {
		t.Fatalf("result = %#v, want successful leader with out-of-group setsid child", result)
	}
	if strings.Contains(result.ErrorDetail(), "descendant") || strings.Contains(result.ErrorDetail(), "process tree") {
		t.Fatalf("error detail = %q, want no spawn-tree residual claim", result.ErrorDetail())
	}
	pids := readCommandExecPIDs(t, readyPath)
	if len(pids) != 2 {
		t.Fatalf("pids = %v, want leader and setsid child", pids)
	}
	t.Cleanup(func() { _ = unix.Kill(pids[1], unix.SIGKILL) })
	assertCommandExecProcessesGone(t, pids[:1])
	assertCommandExecProcessAlive(t, pids[1])
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

func TestDefaultRunnerPreservesNonZeroExitWhenSetsidChildHoldsOutputDescriptors(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	registerCommandExecEscapedChildCleanup(t, readyPath)
	executor := NewCommandExecutor(CommandOptions{Timeout: 5 * time.Second, OutputLimit: 1024})

	startedAt := time.Now()
	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args: []string{
			"-test.run=TestCommandExecProcessTreeHelper",
			"--",
			"parent-fail-setsid-inherited-output",
			readyPath,
		},
	})

	if result.Reason() != CommandReasonNonZeroExit || result.TimedOut() {
		t.Fatalf("result = %#v, want prompt nonzero result with escaped inherited writers", result)
	}
	if exitCode, ok := result.ExitCode(); !ok || exitCode != 17 {
		t.Fatalf("exit code = %d/%t, want 17", exitCode, ok)
	}
	if !result.StdoutTruncated() || !result.StderrTruncated() {
		t.Fatalf(
			"truncation flags = %t/%t, want incomplete capture while a setsid child held pipes",
			result.StdoutTruncated(),
			result.StderrTruncated(),
		)
	}
	if elapsed := time.Since(startedAt); elapsed >= 2*time.Second {
		t.Fatalf("escaped output descriptor cleanup took %s, want less than 2s", elapsed)
	}
	pids := readCommandExecPIDs(t, readyPath)
	t.Cleanup(func() {
		for _, pid := range pids[1:] {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	assertCommandExecProcessesGone(t, pids[:1])
	assertCommandExecProcessAlive(t, pids[1])
}

func TestDefaultRunnerPreservesExitWhenCleanupCrossesDeadline(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	registerCommandExecEscapedChildCleanup(t, readyPath)
	entered, release := holdProcessGroupWaitDone(t)
	ctx := newTriggeredDeadlineContext()
	executor := NewCommandExecutor(CommandOptions{
		Timeout:     DefaultCommandTimeout,
		OutputLimit: 1024,
	})
	resultDone := make(chan CommandAttemptResult, 1)
	go func() {
		resultDone <- executor.executeWithoutWorkingDirectory(ctx, CommandAttemptRequest{
			Command: executable,
			Args: []string{
				"-test.run=TestCommandExecProcessTreeHelper",
				"--",
				"parent-fail-setsid-inherited-output",
				readyPath,
			},
		})
	}()

	pids := readCommandExecPIDs(t, readyPath)
	assertCommandExecProcessesGone(t, pids[:1])
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for leader wait before cleanup deadline")
	}
	ctx.expire()
	release()

	var result CommandAttemptResult
	select {
	case result = <-resultDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runner after leader exit and cleanup deadline")
	}

	if result.Reason() != CommandReasonNonZeroExit || result.TimedOut() {
		t.Fatalf("result = %#v, want nonzero exit preserved when output cleanup crossed the attempt deadline", result)
	}
	if exitCode, ok := result.ExitCode(); !ok || exitCode != 17 {
		t.Fatalf("exit code = %d/%t, want 17", exitCode, ok)
	}
	assertCommandExecProcessAlive(t, pids[1])
}

func TestDefaultRunnerResidualProcessWithoutInheritedOutputDoesNotTruncateCompleteStreams(t *testing.T) {
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
			"parent-exit-discard-output",
			readyPath,
		},
	})

	if result.Reason() != CommandReasonRunnerError || result.TimedOut() {
		t.Fatalf("result = %#v, want residual-process runner failure", result)
	}
	if exitCode, ok := result.ExitCode(); !ok || exitCode != 0 {
		t.Fatalf("exit code = %d/%t, want observed direct-child exit 0", exitCode, ok)
	}
	if result.StdoutTruncated() || result.StderrTruncated() {
		t.Fatalf(
			"truncation flags = %t/%t, want complete streams when residual members held no output",
			result.StdoutTruncated(),
			result.StderrTruncated(),
		)
	}
	if !strings.Contains(result.ErrorDetail(), "process-group members remained") {
		t.Fatalf("error detail = %q, want residual process-group error", result.ErrorDetail())
	}
	pids := readCommandExecPIDs(t, readyPath)
	assertCommandExecProcessesGone(t, pids[1:])
}

func TestDefaultRunnerMarksOnlyTheStreamHeldOpenByASetsidChild(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	registerCommandExecEscapedChildCleanup(t, readyPath)
	executor := NewCommandExecutor(CommandOptions{Timeout: 5 * time.Second, OutputLimit: 1024})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args: []string{
			"-test.run=TestCommandExecProcessTreeHelper",
			"--",
			"parent-fail-setsid-inherited-stderr",
			readyPath,
		},
	})

	if result.Reason() != CommandReasonNonZeroExit || result.TimedOut() {
		t.Fatalf("result = %#v, want nonzero exit with one held stderr stream", result)
	}
	if result.Stdout() != "leader stdout\n" || result.StdoutTruncated() {
		t.Fatalf("stdout = %q truncated=%t, want complete leader stdout", result.Stdout(), result.StdoutTruncated())
	}
	if !result.StderrTruncated() || result.Stderr() != "leader stderr\n" {
		t.Fatalf("stderr = %q truncated=%t, want incomplete held stderr", result.Stderr(), result.StderrTruncated())
	}
	pids := readCommandExecPIDs(t, readyPath)
	t.Cleanup(func() {
		for _, pid := range pids[1:] {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	assertCommandExecProcessesGone(t, pids[:1])
	assertCommandExecProcessAlive(t, pids[1])
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

func registerCommandExecEscapedChildCleanup(t *testing.T, readyPath string) {
	t.Helper()
	t.Cleanup(func() {
		content, err := os.ReadFile(readyPath + ".child")
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
		if err != nil || pid <= 0 {
			return
		}
		_ = unix.Kill(pid, unix.SIGKILL)
	})
}

func holdProcessGroupWaitDone(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	entered := make(chan struct{})
	releaseCh := make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	afterProcessGroupWaitDone = func() {
		enterOnce.Do(func() { close(entered) })
		<-releaseCh
	}
	release := func() {
		releaseOnce.Do(func() { close(releaseCh) })
	}
	t.Cleanup(func() {
		afterProcessGroupWaitDone = nil
		release()
	})
	return entered, release
}

type triggeredDeadlineContext struct {
	done chan struct{}
	once sync.Once
}

func newTriggeredDeadlineContext() *triggeredDeadlineContext {
	return &triggeredDeadlineContext{done: make(chan struct{})}
}

func (ctx *triggeredDeadlineContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *triggeredDeadlineContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *triggeredDeadlineContext) Err() error {
	select {
	case <-ctx.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (ctx *triggeredDeadlineContext) Value(any) any {
	return nil
}

func (ctx *triggeredDeadlineContext) expire() {
	ctx.once.Do(func() {
		close(ctx.done)
	})
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
		"parent-exit-discard-output",
		"parent-exit-inherited-output",
		"parent-exit-secret-prefix-inherited-output",
		"parent-fail-inherited-output",
		"parent-fail-setsid-inherited-output",
		"parent-fail-setsid-inherited-stderr",
		"parent-exit-short-inherited-output",
		"parent-exit-setsid":
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
		} else if strings.Contains(mode, "inherited-stderr") {
			child.Stderr = os.Stderr
			devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				os.Exit(88)
			}
			child.Stdout = devNull
		} else if mode == "parent-exit-discard-output" {
			devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				os.Exit(88)
			}
			child.Stdout = devNull
			child.Stderr = devNull
		}
		if strings.Contains(mode, "setsid") {
			child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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
		if (strings.Contains(mode, "inherited-output") || strings.Contains(mode, "inherited-stderr")) &&
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
		if mode == "parent-fail-inherited-output" ||
			mode == "parent-fail-setsid-inherited-output" ||
			mode == "parent-fail-setsid-inherited-stderr" {
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

func assertCommandExecProcessAlive(t *testing.T, pid int) {
	t.Helper()
	if err := unix.Kill(pid, 0); err != nil && err != unix.EPERM {
		t.Fatalf("pid %d not alive: %v", pid, err)
	}
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
