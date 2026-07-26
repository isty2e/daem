//go:build darwin || linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	runtimeprobemcp "github.com/isty2e/daem/internal/assurance/runtimeprobe/mcp"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/subprocess"

	"golang.org/x/sys/unix"
)

const (
	signalLifecycleTreeRole      = "DAEM_SIGNAL_LIFECYCLE_TREE_ROLE"
	signalLifecycleTreeReadyPath = "DAEM_SIGNAL_LIFECYCLE_TREE_READY_PATH"
	signalLifecycleTreeChildPath = "DAEM_SIGNAL_LIFECYCLE_TREE_CHILD_PATH"
	signalLifecycleTreeTERMPath  = "DAEM_SIGNAL_LIFECYCLE_TREE_TERM_PATH"
	signalLifecycleTreeSecret    = "DAEM_SIGNAL_LIFECYCLE_TREE_SECRET"
)

func TestSignalLifecycleSecondSignalDoesNotOrphanActiveProcessTree(t *testing.T) {
	temporaryDirectory := t.TempDir()
	readyPath := filepath.Join(temporaryDirectory, "ready")
	childReadyPath := filepath.Join(temporaryDirectory, "child-ready")
	termPath := filepath.Join(temporaryDirectory, "term")
	cmd, lines := startSignalLifecycleHelperWithEnv(
		t, "force-tree",
		signalLifecycleTreeReadyPath+"="+readyPath,
		signalLifecycleTreeChildPath+"="+childReadyPath,
		signalLifecycleTreeTERMPath+"="+termPath,
	)
	waitForSignalLifecycleLine(t, lines, "ready")
	pids := readSignalLifecyclePIDs(t, readyPath)
	t.Cleanup(func() {
		_ = unix.Kill(-pids[0], unix.SIGKILL)
	})

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("first Signal: %v", err)
	}
	waitForSignalLifecycleLine(t, lines, "canceled")
	waitForSignalLifecycleFile(t, termPath)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("second Signal: %v", err)
	}
	assertSignalLifecycleExit(t, cmd, 130)
	assertSignalLifecycleProcessesGone(t, pids)
}

func TestSignalLifecycleSIGTERMCleansMCPProbeProcessTree(t *testing.T) {
	temporaryDirectory := t.TempDir()
	readyPath := filepath.Join(temporaryDirectory, "ready")
	childReadyPath := filepath.Join(temporaryDirectory, "child-ready")
	termPath := filepath.Join(temporaryDirectory, "term")
	cmd, lines := startSignalLifecycleHelperWithEnv(
		t, "mcp-tree",
		signalLifecycleTreeReadyPath+"="+readyPath,
		signalLifecycleTreeChildPath+"="+childReadyPath,
		signalLifecycleTreeTERMPath+"="+termPath,
	)
	waitForSignalLifecycleLine(t, lines, "ready")
	pids := readSignalLifecyclePIDs(t, readyPath)
	t.Cleanup(func() {
		_ = unix.Kill(-pids[0], unix.SIGKILL)
	})

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Signal: %v", err)
	}
	waitForSignalLifecycleLine(t, lines, "canceled")
	waitForSignalLifecycleFile(t, termPath)
	assertSignalLifecycleExit(t, cmd, 143)
	assertSignalLifecycleProcessesGone(t, pids)
}

func runSignalLifecycleTreeMode(ctx context.Context, mode string) int {
	canceled := make(chan struct{})
	go func() {
		<-ctx.Done()
		fmt.Println("canceled")
		close(canceled)
	}()
	executable, err := os.Executable()
	if err != nil {
		return 90
	}
	if err := os.Setenv(signalLifecycleTreeRole, "parent"); err != nil {
		return 88
	}
	if mode == "mcp-tree" {
		_, _ = runtimeprobemcp.NewExecutor(time.Minute).Probe(ctx, runtimeprobemcp.ProbeRequest{
			Transport: runtimeprobemcp.TransportStdio,
			Command:   executable,
			Args:      []string{"-test.run=TestSignalLifecycleProcessTreeHelper"},
		}, signalLifecycleWorkingDirectoryBinder())
		<-canceled
		return 1
	}
	result := subprocess.NewCommandExecutor(subprocess.CommandOptions{Timeout: time.Minute}).ExecuteInWorkingDirectory(
		ctx,
		subprocess.CommandAttemptRequest{
			Command: executable,
			Args:    []string{"-test.run=TestSignalLifecycleProcessTreeHelper"},
		},
		signalLifecycleWorkingDirectoryBinder(),
	)
	<-canceled
	if result.Canceled() {
		return 1
	}
	return 89
}

func signalLifecycleWorkingDirectoryBinder() subprocess.WorkingDirectoryBinder {
	return func() (subprocess.WorkingDirectoryBinding, error) {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		root, err := rootedpath.CaptureRoot(cwd)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		return root.AcquireWorkingDirectory()
	}
}

func TestSignalLifecycleProcessTreeHelper(t *testing.T) {
	role := os.Getenv(signalLifecycleTreeRole)
	if role == "" {
		return
	}

	switch role {
	case "parent":
		termSignals := make(chan os.Signal, 1)
		signal.Notify(termSignals, syscall.SIGTERM)
		defer signal.Stop(termSignals)
		executable, err := os.Executable()
		if err != nil {
			os.Exit(88)
		}
		child := exec.Command(executable, "-test.run=TestSignalLifecycleProcessTreeHelper")
		child.Env = append(os.Environ(), signalLifecycleTreeRole+"=child")
		if err := child.Start(); err != nil {
			os.Exit(87)
		}
		if err := waitForSignalLifecycleFilePath(os.Getenv(signalLifecycleTreeChildPath), subprocessTestTimeout); err != nil {
			_ = child.Process.Kill()
			os.Exit(86)
		}
		if secret := os.Getenv(signalLifecycleTreeSecret); secret != "" {
			fmt.Printf("token=%s\n", secret)
		}
		pids := fmt.Sprintf("%d,%d", os.Getpid(), child.Process.Pid)
		if err := writeSignalLifecycleFile(os.Getenv(signalLifecycleTreeReadyPath), pids); err != nil {
			_ = child.Process.Kill()
			os.Exit(85)
		}
		for range termSignals {
			if err := writeSignalLifecycleFile(os.Getenv(signalLifecycleTreeTERMPath), strconv.Itoa(os.Getpid())); err != nil {
				os.Exit(84)
			}
		}
	case "child":
		signal.Ignore(syscall.SIGTERM)
		if err := writeSignalLifecycleFile(os.Getenv(signalLifecycleTreeChildPath), strconv.Itoa(os.Getpid())); err != nil {
			os.Exit(83)
		}
		select {}
	default:
		os.Exit(82)
	}
}

func readSignalLifecyclePIDs(t *testing.T, path string) []int {
	t.Helper()
	waitForSignalLifecycleFile(t, path)
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

func waitForSignalLifecycleFile(t *testing.T, path string) {
	t.Helper()
	if err := waitForSignalLifecycleFilePath(path, subprocessTestTimeout); err != nil {
		t.Fatal(err)
	}
}

func waitForSignalLifecycleFilePath(path string, timeout time.Duration) error {
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

func writeSignalLifecycleFile(path string, content string) error {
	temporaryPath := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(temporaryPath, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func assertSignalLifecycleProcessesGone(t *testing.T, pids []int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		alive := make([]int, 0, len(pids))
		for _, pid := range pids {
			err := unix.Kill(pid, 0)
			if err == nil || err == unix.EPERM {
				alive = append(alive, pid)
			} else if err != unix.ESRCH {
				t.Fatalf("probe pid %d: %v", pid, err)
			}
		}
		if len(alive) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("processes still alive after signal lifecycle exit: %v", alive)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
