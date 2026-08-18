//go:build darwin || linux

package gitcli

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/supply/source"
)

const (
	gitProcessHelperStageEnv = "DAEM_GIT_PROCESS_HELPER_STAGE"
	gitProcessHelperPIDEnv   = "DAEM_GIT_PROCESS_HELPER_PID_FILE"
)

func TestRunGitOutputCancelsCompleteProcessTree(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pids")
	ctx, cancel := context.WithCancel(context.Background())
	command := gitProcessHelperCommand(t, ctx, "chain-parent", pidFile)

	result := make(chan error, 1)
	go func() {
		_, err := runGitOutput(ctx, command)
		result <- err
	}()

	pids := waitForGitHelperPIDs(t, pidFile, 3)
	cancel()
	select {
	case err := <-result:
		if err != context.Canceled {
			t.Fatalf("runGitOutput error = %#v, want exact context.Canceled", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("runGitOutput did not return after cancellation")
	}
	assertGitHelperProcessesGone(t, pids)
}

func TestRunGitOutputRejectsAndCleansResidualDescendant(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pids")
	command := gitProcessHelperCommand(t, context.Background(), "residual-parent", pidFile)

	_, err := runGitOutput(context.Background(), command)
	if err == nil || !strings.Contains(err.Error(), "process-group members remained") {
		t.Fatalf("runGitOutput error = %v, want residual process-group classification", err)
	}
	assertGitHelperProcessesGone(t, waitForGitHelperPIDs(t, pidFile, 2))
}

func TestRunGitOutputAllowsSetsidChildToOutliveLeader(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pids")
	command := gitProcessHelperCommand(t, context.Background(), "setsid-parent", pidFile)

	_, err := runGitOutput(context.Background(), command)
	if err != nil {
		t.Fatalf("runGitOutput error = %v, want success with out-of-group setsid child", err)
	}
	pids := waitForGitHelperPIDs(t, pidFile, 2)
	t.Cleanup(func() { _ = syscall.Kill(pids[1], syscall.SIGKILL) })
	assertGitHelperProcessesGone(t, pids[:1])
	assertGitHelperProcessAlive(t, pids[1])
}

func TestExtractGitArchiveCommandRejectsAndCleansResidualDescendant(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pids")
	outputRoot := filepath.Join(t.TempDir(), "output")
	command := gitProcessHelperCommand(t, context.Background(), "archive-parent", pidFile)

	err := extractGitArchiveCommand(context.Background(), command, outputRoot)
	if err == nil || !strings.Contains(err.Error(), "process-group members remained") {
		t.Fatalf("extractGitArchiveCommand error = %v, want residual process-group classification", err)
	}
	content, readErr := os.ReadFile(filepath.Join(outputRoot, "payload.txt"))
	if readErr != nil || string(content) != "payload" {
		t.Fatalf("extracted payload = %q, %v; want payload", content, readErr)
	}
	assertGitHelperProcessesGone(t, waitForGitHelperPIDs(t, pidFile, 2))
}

func TestExtractGitArchiveCommandStopsTreeWhenExtractionFailsFirst(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pids")
	outputRoot := filepath.Join(t.TempDir(), "output")
	command := gitProcessHelperCommand(t, context.Background(), "archive-invalid-parent", pidFile)

	started := time.Now()
	err := extractGitArchiveCommand(context.Background(), command, outputRoot)
	if err == nil {
		t.Fatal("extractGitArchiveCommand accepted malformed tar")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("malformed archive cleanup took %s, want bounded termination", elapsed)
	}
	assertGitHelperProcessesGone(t, waitForGitHelperPIDs(t, pidFile, 2))
}

func TestRunGitOutputBoundsAndRedactsActualStderr(t *testing.T) {
	t.Parallel()
	command := gitProcessHelperCommand(t, context.Background(), "stderr-parent", filepath.Join(t.TempDir(), "pids"))
	command.Env = append(command.Env, "DAEM_GIT_PROCESS_TOKEN=synthetic-secret")

	_, err := runGitOutput(context.Background(), command)
	if err == nil {
		t.Fatal("runGitOutput accepted failing helper")
	}
	message := err.Error()
	if strings.Contains(message, "synthetic-secret") {
		t.Fatalf("runGitOutput disclosed a secret: %q", message)
	}
	if !strings.Contains(message, "[REDACTED]") || !strings.Contains(message, "[truncated]") {
		t.Fatalf("runGitOutput error = %q, want redaction and truncation markers", message)
	}
	if len([]byte(message)) > maxGitDiagnosticBytes+1024 {
		t.Fatalf("runGitOutput diagnostic retained %d bytes, want bounded output", len(message))
	}
}

func TestRunGitReaderTerminatesProcessTreeWhenListingBudgetFails(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pids")
	command := gitProcessHelperCommand(t, context.Background(), "listing-overflow-parent", pidFile)
	budget := source.NewRootListingBudget()

	err := runGitReader(context.Background(), command, func(output io.Reader) error {
		_, readErr := readGitTreeDirectories(output, budget)
		return readErr
	})
	if !errors.Is(err, source.ErrRootListingLimitExceeded) {
		t.Fatalf("runGitReader error = %v, want source listing limit", err)
	}
	assertGitHelperProcessesGone(t, waitForGitHelperPIDs(t, pidFile, 2))
}

func TestGitProcessHelper(t *testing.T) {
	stage := os.Getenv(gitProcessHelperStageEnv)
	if stage == "" {
		return
	}
	if err := runGitProcessHelper(stage, os.Getenv(gitProcessHelperPIDEnv)); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func runGitProcessHelper(stage string, pidFile string) error {
	if err := appendGitHelperPID(pidFile, os.Getpid()); err != nil {
		return err
	}

	switch stage {
	case "chain-parent":
		return runGitProcessHelperChild("chain-child", pidFile, true)
	case "chain-child":
		return runGitProcessHelperChild("chain-grandchild", pidFile, true)
	case "chain-grandchild", "residual-child", "archive-child", "archive-invalid-child", "listing-overflow-child", "setsid-child":
		for {
			time.Sleep(time.Hour)
		}
	case "residual-parent":
		if err := startGitProcessHelperChild("residual-child", pidFile); err != nil {
			return err
		}
		return waitForGitHelperPIDCount(pidFile, 2, 3*time.Second)
	case "setsid-parent":
		if err := startGitProcessHelperChild("setsid-child", pidFile); err != nil {
			return err
		}
		return waitForGitHelperPIDCount(pidFile, 2, 3*time.Second)
	case "archive-parent":
		if err := startGitProcessHelperChild("archive-child", pidFile); err != nil {
			return err
		}
		if err := waitForGitHelperPIDCount(pidFile, 2, 3*time.Second); err != nil {
			return err
		}
		return writeGitHelperArchive(os.Stdout)
	case "archive-invalid-parent":
		if err := startGitProcessHelperChild("archive-invalid-child", pidFile); err != nil {
			return err
		}
		if err := waitForGitHelperPIDCount(pidFile, 2, 3*time.Second); err != nil {
			return err
		}
		if _, err := io.WriteString(os.Stdout, strings.Repeat("x", 512)); err != nil {
			return err
		}
		for {
			time.Sleep(time.Hour)
		}
	case "listing-overflow-parent":
		if err := startGitProcessHelperChild("listing-overflow-child", pidFile); err != nil {
			return err
		}
		if err := waitForGitHelperPIDCount(pidFile, 2, 3*time.Second); err != nil {
			return err
		}
		if _, err := io.WriteString(os.Stdout, strings.Repeat("x", 5_000)); err != nil {
			return err
		}
		for {
			time.Sleep(time.Hour)
		}
	case "stderr-parent":
		secret := os.Getenv("DAEM_GIT_PROCESS_TOKEN")
		_, _ = io.WriteString(
			os.Stderr,
			"Authorization: Bearer "+secret+" inherited="+secret+" "+
				strings.Repeat("x", maxGitDiagnosticBytes+1024),
		)
		return fmt.Errorf("forced helper failure")
	default:
		return fmt.Errorf("unknown helper stage %q", stage)
	}
}

func gitProcessHelperCommand(t *testing.T, ctx context.Context, stage string, pidFile string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	command := exec.CommandContext(ctx, executable, "-test.run=^TestGitProcessHelper$")
	command.Env = gitProcessHelperEnv(stage, pidFile)
	return command
}

func runGitProcessHelperChild(stage string, pidFile string, wait bool) error {
	command, err := newGitProcessHelperChild(stage, pidFile)
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	if wait {
		return command.Wait()
	}
	return nil
}

func startGitProcessHelperChild(stage string, pidFile string) error {
	return runGitProcessHelperChild(stage, pidFile, false)
}

func newGitProcessHelperChild(stage string, pidFile string) (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := exec.Command(executable, "-test.run=^TestGitProcessHelper$")
	command.Env = gitProcessHelperEnv(stage, pidFile)
	if stage == "setsid-child" {
		command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		command.Stdout = io.Discard
		command.Stderr = io.Discard
	} else if stage == "residual-child" || stage == "archive-child" || stage == "archive-invalid-child" {
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
	} else {
		command.Stdout = io.Discard
		command.Stderr = io.Discard
	}
	return command, nil
}

func gitProcessHelperEnv(stage string, pidFile string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, gitProcessHelperStageEnv+"=") || strings.HasPrefix(entry, gitProcessHelperPIDEnv+"=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, gitProcessHelperStageEnv+"="+stage, gitProcessHelperPIDEnv+"="+pidFile)
}

func appendGitHelperPID(path string, pid int) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(file, "%d\n", pid); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func waitForGitHelperPIDs(t *testing.T, path string, count int) []int {
	t.Helper()
	if err := waitForGitHelperPIDCount(path, count, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read helper PIDs: %v", err)
	}
	lines := strings.Fields(string(content))
	pids := make([]int, 0, len(lines))
	for _, line := range lines {
		pid, err := strconv.Atoi(line)
		if err != nil {
			t.Fatalf("parse helper PID %q: %v", line, err)
		}
		pids = append(pids, pid)
	}
	return pids
}

func waitForGitHelperPIDCount(path string, count int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(path)
		if err == nil && len(strings.Fields(string(content))) >= count {
			return nil
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %d helper PIDs", count)
}

func assertGitHelperProcessAlive(t *testing.T, pid int) {
	t.Helper()
	err := syscall.Kill(pid, 0)
	if err != nil && !errors.Is(err, syscall.EPERM) {
		t.Fatalf("helper process %d not alive: %v", pid, err)
	}
}

func assertGitHelperProcessesGone(t *testing.T, pids []int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range pids {
		for {
			err := syscall.Kill(pid, 0)
			if errors.Is(err, syscall.ESRCH) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("helper process %d remains after cleanup: %v", pid, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func writeGitHelperArchive(output io.Writer) error {
	writer := tar.NewWriter(output)
	payload := []byte("payload")
	if err := writer.WriteHeader(&tar.Header{
		Name: "payload.txt",
		Mode: 0o600,
		Size: int64(len(payload)),
	}); err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.Close()
}
