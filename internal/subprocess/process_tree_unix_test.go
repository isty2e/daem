//go:build darwin || linux

package subprocess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	processTreeHelperMode     = "DAEM_PROCESSTREE_HELPER_MODE"
	processTreeReadyPath      = "DAEM_PROCESSTREE_READY_PATH"
	processTreeChildReadyPath = "DAEM_PROCESSTREE_CHILD_READY_PATH"
)

func TestGroupTerminatesCompleteProcessTree(t *testing.T) {
	tests := []struct {
		name          string
		ignoreTERM    bool
		wantEscalated bool
	}{
		{name: "graceful term"},
		{name: "kill escalation", ignoreTERM: true, wantEscalated: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			readyPath := t.TempDir() + "/ready"
			childReadyPath := readyPath + ".child"
			mode := "parent"
			if test.ignoreTERM {
				mode = "parent-ignore-term"
			}
			ctx, cancel := context.WithCancel(context.Background())
			cmd := exec.CommandContext(ctx, executable, "-test.run=TestProcessTreeHelperProcess")
			cmd.Env = append(
				os.Environ(),
				processTreeHelperMode+"="+mode,
				processTreeReadyPath+"="+readyPath,
				processTreeChildReadyPath+"="+childReadyPath,
			)
			group, err := bindProcessGroupWithOptions(cmd, processTerminationOptions{GracePeriod: 100 * time.Millisecond, KillWait: time.Second})
			if err != nil {
				t.Fatalf("BindProcessGroup: %v", err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}
			pids := readProcessTreePIDs(t, readyPath)

			cancel()
			waitErr := cmd.Wait()
			if waitErr == nil || (!errors.Is(waitErr, context.Canceled) && !isProcessTreeSignalExit(waitErr)) {
				t.Fatalf("Wait error = %v, want cancellation or signal exit", waitErr)
			}
			termination, err := group.Terminate()
			if err != nil {
				t.Fatalf("Terminate: %v", err)
			}
			if !termination.ProcessesFound() || termination.escalated != test.wantEscalated {
				t.Fatalf("termination = found:%t escalated:%t", termination.ProcessesFound(), termination.escalated)
			}
			assertProcessTreeProcessesGone(t, pids)
		})
	}
}

func TestGroupTerminatesResidualGrandchildAfterLeaderExit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	childReadyPath := readyPath + ".child"
	cmd := exec.CommandContext(context.Background(), executable, "-test.run=TestProcessTreeHelperProcess")
	cmd.Env = append(
		os.Environ(),
		processTreeHelperMode+"=parent-exit",
		processTreeReadyPath+"="+readyPath,
		processTreeChildReadyPath+"="+childReadyPath,
	)
	group, err := bindProcessGroupWithOptions(cmd, processTerminationOptions{GracePeriod: 100 * time.Millisecond, KillWait: time.Second})
	if err != nil {
		t.Fatalf("BindProcessGroup: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pids := readProcessTreePIDs(t, readyPath)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	termination, err := group.Terminate()
	if err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if !termination.ProcessesFound() {
		t.Fatal("ProcessesFound = false, want residual grandchild")
	}
	assertProcessTreeProcessesGone(t, pids[1:])
}

func TestAwaitJoinsAlreadyExpiredContextWhenLeaderAlreadyExited(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "/bin/echo", "await-join")
	group, err := BindProcessGroup(cmd)
	if err != nil {
		t.Fatalf("BindProcessGroup: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := group.Await(context.Background(), InheritedOutputCloseWait); err != nil {
		t.Fatalf("first Await: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = group.Await(ctx, InheritedOutputCloseWait)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("second Await error = %v, want canceled joined after leader wait already completed", err)
	}
}

func TestGroupAllowsNaturalGrandchildQuiescenceAfterLeaderExit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	childReadyPath := readyPath + ".child"
	cmd := exec.CommandContext(context.Background(), executable, "-test.run=TestProcessTreeHelperProcess")
	cmd.Env = append(
		os.Environ(),
		processTreeHelperMode+"=parent-exit-natural",
		processTreeReadyPath+"="+readyPath,
		processTreeChildReadyPath+"="+childReadyPath,
	)
	group, err := bindProcessGroupWithOptions(cmd, processTerminationOptions{
		QuiescencePeriod: 500 * time.Millisecond,
		GracePeriod:      100 * time.Millisecond,
		KillWait:         time.Second,
	})
	if err != nil {
		t.Fatalf("BindProcessGroup: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pids := readProcessTreePIDs(t, readyPath)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	termination, err := group.ReapAfterLeaderExit()
	if err != nil {
		t.Fatalf("ReapAfterLeaderExit: %v", err)
	}
	if termination.ProcessesFound() {
		t.Fatal("ProcessesFound = true for naturally quiescent grandchild")
	}
	assertProcessTreeProcessesGone(t, pids[1:])
}

func TestGroupConcurrentTerminationIsIdempotent(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	childReadyPath := readyPath + ".child"
	cmd := exec.CommandContext(context.Background(), executable, "-test.run=TestProcessTreeHelperProcess")
	cmd.Env = append(
		os.Environ(),
		processTreeHelperMode+"=parent-ignore-term",
		processTreeReadyPath+"="+readyPath,
		processTreeChildReadyPath+"="+childReadyPath,
	)
	group, err := bindProcessGroupWithOptions(cmd, processTerminationOptions{GracePeriod: 100 * time.Millisecond, KillWait: time.Second})
	if err != nil {
		t.Fatalf("BindProcessGroup: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pids := readProcessTreePIDs(t, readyPath)
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	const callers = 8
	results := make(chan ProcessTermination, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Go(func() {
			termination, err := group.Terminate()
			results <- termination
			errorsFound <- err
		})
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("Terminate: %v", err)
		}
	}
	for result := range results {
		if !result.ProcessesFound() || !result.escalated {
			t.Fatalf("termination = found:%t escalated:%t", result.ProcessesFound(), result.escalated)
		}
	}
	if err := <-waitDone; !isProcessTreeSignalExit(err) {
		t.Fatalf("Wait error = %v, want signal exit", err)
	}
	assertProcessTreeProcessesGone(t, pids)
}

func TestBindRejectsConflictingProcessGroupPolicy(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "unused-daem-test-command")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	_, err := BindProcessGroup(cmd)
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("BindProcessGroup error = %v, want process-group conflict", err)
	}
}

func TestClassifyProcessGroupProbeIsThreeValued(t *testing.T) {
	tests := []struct {
		err        error
		occupancy  processGroupOccupancy
		recognized bool
	}{
		{err: nil, occupancy: processGroupSignalable, recognized: true},
		{err: unix.ESRCH, occupancy: processGroupAbsent, recognized: true},
		{err: unix.EPERM, occupancy: processGroupUnsignalable, recognized: true},
		{err: unix.EINVAL, occupancy: processGroupAbsent, recognized: false},
	}
	for _, test := range tests {
		occupancy, recognized := classifyProcessGroupProbe(test.err)
		if occupancy != test.occupancy || recognized != test.recognized {
			t.Fatalf(
				"classifyProcessGroupProbe(%v) = %d,%t, want %d,%t",
				test.err,
				occupancy,
				recognized,
				test.occupancy,
				test.recognized,
			)
		}
	}
}

func TestGroupCancelLeavesSetsidChildAlive(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	childReadyPath := readyPath + ".child"
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, executable, "-test.run=TestProcessTreeHelperProcess")
	cmd.Env = append(
		os.Environ(),
		processTreeHelperMode+"=parent-mixed-setsid",
		processTreeReadyPath+"="+readyPath,
		processTreeChildReadyPath+"="+childReadyPath,
	)
	group, err := bindProcessGroupWithOptions(cmd, processTerminationOptions{GracePeriod: 100 * time.Millisecond, KillWait: time.Second})
	if err != nil {
		t.Fatalf("BindProcessGroup: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pids := readProcessTreePIDs(t, readyPath)
	if len(pids) != 3 {
		t.Fatalf("pids = %v, want parent, in-group child, setsid child", pids)
	}
	escaped := pids[2]
	t.Cleanup(func() { _ = unix.Kill(escaped, unix.SIGKILL) })

	cancel()
	waitErr := cmd.Wait()
	if waitErr == nil || (!errors.Is(waitErr, context.Canceled) && !isProcessTreeSignalExit(waitErr)) {
		t.Fatalf("Wait error = %v, want cancellation or signal exit", waitErr)
	}
	termination, err := group.Terminate()
	if err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if termination.UnsignalableOccupancy() {
		t.Fatalf("termination = %#v, want signalable in-group cleanup", termination)
	}
	assertProcessTreeProcessesGone(t, pids[:2])
	assertProcessTreeProcessAlive(t, escaped)
}

func TestGroupReapAfterLeaderExitLeavesSetsidChildAlive(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := t.TempDir() + "/ready"
	childReadyPath := readyPath + ".child"
	cmd := exec.CommandContext(context.Background(), executable, "-test.run=TestProcessTreeHelperProcess")
	cmd.Env = append(
		os.Environ(),
		processTreeHelperMode+"=parent-exit-mixed-setsid",
		processTreeReadyPath+"="+readyPath,
		processTreeChildReadyPath+"="+childReadyPath,
	)
	group, err := bindProcessGroupWithOptions(cmd, processTerminationOptions{
		QuiescencePeriod: 500 * time.Millisecond,
		GracePeriod:      100 * time.Millisecond,
		KillWait:         time.Second,
	})
	if err != nil {
		t.Fatalf("BindProcessGroup: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pids := readProcessTreePIDs(t, readyPath)
	if len(pids) != 3 {
		t.Fatalf("pids = %v, want parent, in-group child, setsid child", pids)
	}
	escaped := pids[2]
	t.Cleanup(func() { _ = unix.Kill(escaped, unix.SIGKILL) })
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	termination, err := group.ReapAfterLeaderExit()
	if err != nil {
		t.Fatalf("ReapAfterLeaderExit: %v", err)
	}
	if termination.ProcessesFound() || termination.UnsignalableOccupancy() {
		t.Fatalf("termination = found:%t unsignalable:%t, want empty dedicated group", termination.ProcessesFound(), termination.UnsignalableOccupancy())
	}
	assertProcessTreeProcessesGone(t, pids[:2])
	assertProcessTreeProcessAlive(t, escaped)
}

func TestProcessTreeHelperProcess(t *testing.T) {
	mode := os.Getenv(processTreeHelperMode)
	if mode == "" {
		return
	}
	readyPath := os.Getenv(processTreeReadyPath)
	childReadyPath := os.Getenv(processTreeChildReadyPath)

	switch mode {
	case "grandchild", "grandchild-ignore-term", "grandchild-natural", "grandchild-setsid":
		if mode == "grandchild-ignore-term" {
			signal.Ignore(syscall.SIGTERM)
		}
		if err := writeProcessTreeHelperFixture(childReadyPath, []byte(strconv.Itoa(os.Getpid()))); err != nil {
			os.Exit(91)
		}
		if mode == "grandchild-natural" {
			time.Sleep(25 * time.Millisecond)
			os.Exit(0)
		}
		for {
			time.Sleep(time.Hour)
		}
	case "parent", "parent-ignore-term", "parent-exit", "parent-exit-natural", "parent-mixed-setsid", "parent-exit-mixed-setsid":
		ignoreTERM := mode == "parent-ignore-term"
		if ignoreTERM {
			signal.Ignore(syscall.SIGTERM)
		}
		executable, err := os.Executable()
		if err != nil {
			os.Exit(92)
		}
		childMode := "grandchild"
		if ignoreTERM {
			childMode = "grandchild-ignore-term"
		} else if mode == "parent-exit-natural" || mode == "parent-exit-mixed-setsid" {
			childMode = "grandchild-natural"
		}
		child := exec.Command(executable, "-test.run=TestProcessTreeHelperProcess")
		child.Env = append(
			os.Environ(),
			processTreeHelperMode+"="+childMode,
			processTreeChildReadyPath+"="+childReadyPath,
		)
		if err := child.Start(); err != nil {
			os.Exit(93)
		}
		if err := waitForProcessTreeHelperFile(childReadyPath, 5*time.Second); err != nil {
			_ = child.Process.Kill()
			os.Exit(94)
		}
		content := fmt.Sprintf("%d,%d", os.Getpid(), child.Process.Pid)
		if strings.HasSuffix(mode, "mixed-setsid") {
			setsidReadyPath := childReadyPath + ".setsid"
			setsidChild := exec.Command(executable, "-test.run=TestProcessTreeHelperProcess")
			setsidChild.Env = append(
				os.Environ(),
				processTreeHelperMode+"=grandchild-setsid",
				processTreeChildReadyPath+"="+setsidReadyPath,
			)
			setsidChild.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if err := setsidChild.Start(); err != nil {
				_ = child.Process.Kill()
				os.Exit(97)
			}
			if err := waitForProcessTreeHelperFile(setsidReadyPath, 5*time.Second); err != nil {
				_ = child.Process.Kill()
				_ = setsidChild.Process.Kill()
				os.Exit(98)
			}
			content = fmt.Sprintf("%s,%d", content, setsidChild.Process.Pid)
		}
		if err := writeProcessTreeHelperFixture(readyPath, []byte(content)); err != nil {
			_ = child.Process.Kill()
			os.Exit(95)
		}
		if mode == "parent-exit" || mode == "parent-exit-natural" || mode == "parent-exit-mixed-setsid" {
			os.Exit(0)
		}
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(96)
	}
}

func readProcessTreePIDs(t *testing.T, path string) []int {
	t.Helper()
	if err := waitForProcessTreeHelperFile(path, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSpace(string(content)), ",")
	if len(parts) < 2 {
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

func waitForProcessTreeHelperFile(path string, timeout time.Duration) error {
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

func writeProcessTreeHelperFixture(path string, content []byte) error {
	temporaryPath := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(temporaryPath, content, 0o600); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func assertProcessTreeProcessAlive(t *testing.T, pid int) {
	t.Helper()
	if err := unix.Kill(pid, 0); err != nil && err != unix.EPERM {
		t.Fatalf("pid %d not alive: %v", pid, err)
	}
}

func assertProcessTreeProcessesGone(t *testing.T, pids []int) {
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

func isProcessTreeSignalExit(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() < 0
}
