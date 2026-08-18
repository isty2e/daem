//go:build darwin || linux

package mcp

import (
	"bufio"
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

func TestDefaultCommandRunnerTimeoutTerminatesGrandchild(t *testing.T) {
	readyPath := t.TempDir() + "/ready"
	ctx := newTriggeredDeadlineContext()
	resultDone := make(chan commandResult, 1)
	go func() {
		resultDone <- defaultCommandRunner(ctx, mcpProcessTreeRequest(t, "parent-hang", readyPath))
	}()
	if err := waitForMCPProbeFile(readyPath, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	pids := readMCPProbePIDs(t, readyPath)
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	ctx.expire()

	var result commandResult
	select {
	case result = <-resultDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for expired MCP process group")
	}

	if !result.Started || !result.TimedOut || result.InitializeSucceeded {
		t.Fatalf("result = %#v, want started timeout", result)
	}
	assertMCPProbeProcessesGone(t, pids)
}

func TestDefaultCommandRunnerCancellationTerminatesGrandchild(t *testing.T) {
	readyPath := t.TempDir() + "/ready"
	ctx, cancel := context.WithCancel(context.Background())
	request := mcpProcessTreeRequest(t, "parent-hang", readyPath)
	resultDone := make(chan commandResult, 1)
	go func() {
		resultDone <- defaultCommandRunner(ctx, request)
	}()
	if err := waitForMCPProbeFile(readyPath, 5*time.Second); err != nil {
		cancel()
		t.Fatal(err)
	}
	pids := readMCPProbePIDs(t, readyPath)
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	cancel()
	var result commandResult
	select {
	case result = <-resultDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for canceled MCP process group")
	}

	if !result.Started || !result.Canceled || result.InitializeSucceeded {
		t.Fatalf("result = %#v, want started cancellation", result)
	}
	assertMCPProbeProcessesGone(t, pids)
}

func TestDefaultCommandRunnerSuccessCleansServerGrandchild(t *testing.T) {
	readyPath := t.TempDir() + "/ready"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := defaultCommandRunner(ctx, mcpProcessTreeRequest(t, "parent-initialize", readyPath))

	if !result.Started || !result.InitializeSucceeded || result.Err != nil {
		t.Fatalf("result = %#v, want successful initialize and cleanup", result)
	}
	if result.StderrTruncated {
		t.Fatalf("stderr truncated after killing an initialized server, want complete kill+EOF capture")
	}
	assertMCPProbeProcessesGone(t, readMCPProbePIDs(t, readyPath))
}

func TestDefaultCommandRunnerReturnsWhenSetsidChildHoldsPipes(t *testing.T) {
	readyPath := t.TempDir() + "/ready"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultDone := make(chan commandResult, 1)
	go func() {
		resultDone <- defaultCommandRunner(ctx, mcpProcessTreeRequest(t, "setsid-inherit-parent", readyPath))
	}()
	pids := readMCPProbePIDs(t, readyPath)
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	cancel()
	var result commandResult
	select {
	case result = <-resultDone:
	case <-time.After(6 * time.Second):
		t.Fatal("timed out waiting for canceled MCP process group while a setsid child held pipes")
	}
	if !result.Started || !result.Canceled || result.InitializeSucceeded {
		t.Fatalf("result = %#v, want started cancellation", result)
	}
	if !result.StderrTruncated {
		t.Fatalf("stderr truncated=%t stderr=%q, want forced incomplete capture", result.StderrTruncated, result.Stderr)
	}
	capture := sanitizeCapture(result, []string{"super-secret"}, defaultOutputLimit)
	if strings.Contains(capture.stderr, "super-") || strings.Contains(capture.stderr, "super-secret") {
		t.Fatalf("capture leaked secret prefix: %#v", capture)
	}
	if !capture.stderrTruncated || !capture.redacted {
		t.Fatalf("capture = %#v, want redacted truncated stderr", capture)
	}
	assertMCPProbeProcessAlive(t, pids[1])
}

func TestDefaultCommandRunnerNonzeroExitMarksSetsidStderrIncomplete(t *testing.T) {
	readyPath := t.TempDir() + "/ready"
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := defaultCommandRunner(ctx, mcpProcessTreeRequest(t, "setsid-inherit-stderr-exit", readyPath))
	pids := readMCPProbePIDs(t, readyPath)
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = unix.Kill(pid, unix.SIGKILL)
		}
	})
	if !result.Started || result.InitializeSucceeded || result.Canceled || result.TimedOut {
		t.Fatalf("result = %#v, want started nonzero exit before initialize", result)
	}
	if !result.StderrTruncated {
		t.Fatalf("stderr truncated=%t stderr=%q, want forced incomplete capture", result.StderrTruncated, result.Stderr)
	}
	capture := sanitizeCapture(result, []string{"super-secret"}, defaultOutputLimit)
	if strings.Contains(capture.stderr, "super-") || strings.Contains(capture.stderr, "super-secret") {
		t.Fatalf("capture leaked secret prefix: %#v", capture)
	}
	if !capture.stderrTruncated || !capture.redacted {
		t.Fatalf("capture = %#v, want redacted truncated stderr", capture)
	}
	assertMCPProbeProcessesGone(t, pids[:1])
	assertMCPProbeProcessAlive(t, pids[1])
}

func TestDefaultCommandRunnerKeepsCompletedInitializeWhenDeadlineExpiresDuringCleanup(t *testing.T) {
	markerPath := t.TempDir() + "/initialized"
	registerMCPMarkerPIDCleanup(t, markerPath)
	entered, release := holdMCPProtocolCleanup(t)
	ctx := newTriggeredDeadlineContext()
	resultDone := make(chan commandResult, 1)
	go func() {
		resultDone <- defaultCommandRunner(ctx, commandRequestWithNativeWorkDir(t, commandRequest{
			Command: os.Args[0],
			Args: []string{
				"-test.run=^TestMCPProbeHelperProcess$",
			},
			Env: append(
				os.Environ(),
				"DAEM_MCPPROBE_HELPER=success-hang",
				"DAEM_MCPPROBE_MARKER="+markerPath,
			),
			OutputLimit:     defaultOutputLimit,
			ProtocolVersion: defaultProtocolVersion,
		}))
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for MCP protocol outcome before cleanup")
	}
	ctx.expire()
	release()

	var result commandResult
	select {
	case result = <-resultDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for initialized MCP process cleanup")
	}
	if !result.Started || !result.InitializeSucceeded || result.TimedOut || result.Canceled || result.Err != nil {
		t.Fatalf("result = %#v, want completed initialize preserved through cleanup deadline", result)
	}
}

func TestDefaultCommandRunnerKeepsFailedInitializeWhenDeadlineExpiresDuringCleanup(t *testing.T) {
	markerPath := t.TempDir() + "/initialize-error"
	registerMCPMarkerPIDCleanup(t, markerPath)
	entered, release := holdMCPProtocolCleanup(t)
	ctx := newTriggeredDeadlineContext()
	resultDone := make(chan commandResult, 1)
	go func() {
		resultDone <- defaultCommandRunner(ctx, commandRequestWithNativeWorkDir(t, commandRequest{
			Command: os.Args[0],
			Args: []string{
				"-test.run=^TestMCPProbeHelperProcess$",
			},
			Env: append(
				os.Environ(),
				"DAEM_MCPPROBE_HELPER=initialize-error-hang",
				"DAEM_MCPPROBE_MARKER="+markerPath,
			),
			OutputLimit:     defaultOutputLimit,
			ProtocolVersion: defaultProtocolVersion,
		}))
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for MCP protocol outcome before cleanup")
	}
	ctx.expire()
	release()

	var result commandResult
	select {
	case result = <-resultDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for failed MCP initialize cleanup")
	}
	if !result.Started || result.InitializeSucceeded || result.TimedOut || result.Canceled {
		t.Fatalf("result = %#v, want initialize failure preserved through cleanup deadline", result)
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "initialize error") {
		t.Fatalf("error = %v, want initialize error without timeout", result.Err)
	}
}

func TestMCPProbeProcessTreeHelper(t *testing.T) {
	mode := os.Getenv("DAEM_MCPPROBE_TREE_MODE")
	if mode == "" {
		return
	}
	readyPath := os.Getenv("DAEM_MCPPROBE_TREE_READY")
	if mode == "child" {
		if err := writeMCPProbeHelperFile(readyPath+".child", []byte(strconv.Itoa(os.Getpid()))); err != nil {
			os.Exit(71)
		}
		for {
			time.Sleep(time.Hour)
		}
	}

	executable, err := os.Executable()
	if err != nil {
		os.Exit(72)
	}
	child := exec.Command(executable, "-test.run=TestMCPProbeProcessTreeHelper")
	child.Env = append(
		os.Environ(),
		"DAEM_MCPPROBE_TREE_MODE=child",
		"DAEM_MCPPROBE_TREE_READY="+readyPath,
	)
	if mode == "setsid-inherit-parent" {
		if _, err := os.Stderr.WriteString("super-"); err != nil {
			os.Exit(78)
		}
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
	}
	if mode == "setsid-inherit-stderr-exit" {
		if _, err := os.Stderr.WriteString("super-"); err != nil {
			os.Exit(78)
		}
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		child.Stderr = os.Stderr
	}
	if err := child.Start(); err != nil {
		os.Exit(73)
	}
	if err := waitForMCPProbeFile(readyPath+".child", 5*time.Second); err != nil {
		_ = child.Process.Kill()
		os.Exit(74)
	}
	content := fmt.Sprintf("%d,%d", os.Getpid(), child.Process.Pid)
	if err := writeMCPProbeHelperFile(readyPath, []byte(content)); err != nil {
		_ = child.Process.Kill()
		os.Exit(75)
	}

	switch mode {
	case "parent-hang", "setsid-inherit-parent":
		for {
			time.Sleep(time.Hour)
		}
	case "setsid-inherit-stderr-exit":
		os.Exit(17)
	case "parent-initialize":
		reader := bufio.NewReader(os.Stdin)
		readInitializeRequestFromReaderOrExit(reader)
		fmt.Println(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"tree-test","version":"1"}}}`)
		notification, err := reader.ReadString('\n')
		if err != nil || !strings.Contains(notification, "notifications/initialized") {
			os.Exit(76)
		}
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(77)
	}
}

func mcpProcessTreeRequest(t *testing.T, mode string, readyPath string) commandRequest {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return commandRequestWithNativeWorkDir(t, commandRequest{
		Command: executable,
		Args:    []string{"-test.run=TestMCPProbeProcessTreeHelper"},
		Env: append(
			os.Environ(),
			"DAEM_MCPPROBE_TREE_MODE="+mode,
			"DAEM_MCPPROBE_TREE_READY="+readyPath,
		),
		OutputLimit:     defaultOutputLimit,
		ProtocolVersion: defaultProtocolVersion,
	})
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

func readMCPProbePIDs(t *testing.T, path string) []int {
	t.Helper()
	if err := waitForMCPProbeFile(path, 5*time.Second); err != nil {
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

func waitForMCPProbeFile(path string, timeout time.Duration) error {
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

func writeMCPProbeHelperFile(path string, content []byte) error {
	temporaryPath := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(temporaryPath, content, 0o600); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func holdMCPProtocolCleanup(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	entered := make(chan struct{})
	releaseCh := make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	afterMCPProtocolOutcome = func() {
		enterOnce.Do(func() { close(entered) })
		<-releaseCh
	}
	release := func() {
		releaseOnce.Do(func() { close(releaseCh) })
	}
	t.Cleanup(func() {
		afterMCPProtocolOutcome = nil
		release()
	})
	return entered, release
}

func registerMCPMarkerPIDCleanup(t *testing.T, markerPath string) {
	t.Helper()
	t.Cleanup(func() {
		content, err := os.ReadFile(markerPath + ".pid")
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

func assertMCPProbeProcessAlive(t *testing.T, pid int) {
	t.Helper()
	err := unix.Kill(pid, 0)
	if err != nil && err != unix.EPERM {
		t.Fatalf("pid %d: %v, want alive", pid, err)
	}
}

func assertMCPProbeProcessesGone(t *testing.T, pids []int) {
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
