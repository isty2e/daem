//go:build darwin || linux

package gitcli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	sourcearchive "github.com/isty2e/daem/internal/supply/source/archive"
)

func TestGitCancellationSurvivesInheritedOutputDrain(t *testing.T) {
	for _, consumer := range []string{"output", "archive"} {
		for _, stage := range []string{"stderr-hang-parent", "setsid-stdout-hang-parent", "setsid-stderr-hang-parent"} {
			t.Run(consumer+"/"+stage, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				assertGitContextSurvivesOutputDrain(t, ctx, cancel, consumer, stage, context.Canceled)
			})
		}
	}
}

func TestGitDeadlineSurvivesInheritedOutputDrain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	assertGitContextSurvivesOutputDrain(t, ctx, func() {}, "output", "setsid-stdout-hang-parent", context.DeadlineExceeded)
}

func TestGitOutputDrainKeepsCompletedWaitBeforeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "pids")
	command := gitProcessHelperCommand(t, ctx, "setsid-stdout-parent", pidFile)
	process, err := startGitProcess(command)
	if err != nil {
		t.Fatal(err)
	}
	pids := waitForGitHelperPIDs(t, pidFile, 2)
	t.Cleanup(func() { _ = syscall.Kill(pids[1], syscall.SIGKILL) })
	select {
	case <-process.group.WaitDone():
	case <-time.After(5 * time.Second):
		t.Fatal("leader did not finish before caller cancellation")
	}
	cancel()
	readErr, result := completeGitProcess(ctx, process, func(reader io.Reader) error {
		_, err := io.Copy(io.Discard, reader)
		return err
	})
	if !errors.Is(readErr, os.ErrClosed) || !result.outputIncomplete || result.commandErr != nil {
		t.Fatalf("read=%v, incomplete=%t, command=%v; want completed command with incomplete pipe", readErr, result.outputIncomplete, result.commandErr)
	}
	if got := gitObservedLifecycleError(readErr, result); got != nil {
		t.Errorf("lifecycle error = %v, want completed wait preserved", got)
	}
	assertGitHelperProcessesGone(t, pids[:1])
	assertGitHelperProcessAlive(t, pids[1])
}

func assertGitContextSurvivesOutputDrain(
	t *testing.T,
	ctx context.Context,
	trigger func(),
	consumer string,
	stage string,
	want error,
) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "pids")
	outputRoot := filepath.Join(t.TempDir(), "output")
	command := gitProcessHelperCommand(t, ctx, stage, pidFile)
	readStarted := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- runGitReader(ctx, command, func(reader io.Reader) error {
			observed := &gitCancelReadObserver{reader: reader, started: readStarted}
			if consumer == "archive" {
				return sourcearchive.ExtractTar(ctx, observed, outputRoot)
			}
			_, err := io.Copy(io.Discard, observed)
			return err
		})
	}()

	count := 1
	if stage != "stderr-hang-parent" {
		count = 2
	}
	pids := waitForGitHelperPIDs(t, pidFile, count)
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	select {
	case <-readStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not reach its first read")
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("caller stopped before the fixture was ready: %v", err)
	}
	assertGitHelperProcessAlive(t, pids[0])
	trigger()

	select {
	case err := <-result:
		if !errors.Is(err, want) {
			t.Errorf("Git result = %v, want preserved %v", err, want)
		}
		if err == nil || !strings.Contains(err.Error(), "cancel-stderr-marker") {
			t.Errorf("Git result = %v, want retained stderr diagnostic", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Git did not return after caller cancellation/deadline")
	}
	assertGitHelperProcessesGone(t, pids[:1])
	if len(pids) > 1 {
		assertGitHelperProcessAlive(t, pids[1])
	}
}

type gitCancelReadObserver struct {
	reader  io.Reader
	started chan struct{}
}

func (observer *gitCancelReadObserver) Read(buffer []byte) (int, error) {
	if observer.started != nil {
		close(observer.started)
		observer.started = nil
	}
	return observer.reader.Read(buffer)
}
