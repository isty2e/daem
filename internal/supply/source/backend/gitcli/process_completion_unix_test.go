//go:build darwin || linux

package gitcli

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestGitConsumerCompletionObservesWaitBeforeContext(t *testing.T) {
	for _, test := range []struct {
		name           string
		completedFirst bool
	}{
		{name: "live_wait"},
		{name: "completed_wait", completedFirst: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pidFile := filepath.Join(t.TempDir(), "pids")
			command := gitProcessHelperCommand(t, ctx, "chain-parent", pidFile)
			process, err := startGitProcess(command)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cancel()
				_, _ = process.Terminate()
				_ = process.stdout.Close()
				_ = process.stderr.Close()
			})
			pids := waitForGitHelperPIDs(t, pidFile, 3)
			if test.completedFirst {
				if _, err := process.Terminate(); err != nil {
					t.Fatal(err)
				}
				select {
				case <-process.group.WaitDone():
				case <-time.After(5 * time.Second):
					t.Fatal("leader wait did not complete before caller cancellation")
				}
			}

			observation := &gitCompletionObservationContext{
				Context:       ctx,
				errObserved:   make(chan struct{}),
				releaseErr:    make(chan struct{}),
				awaitObserved: make(chan struct{}),
			}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(observation.releaseErr) }) }
			result := make(chan gitProcessResult, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				_, completed := completeGitProcess(observation, process, func(io.Reader) error { return nil })
				result <- completed
			}()
			t.Cleanup(func() {
				cancel()
				release()
				_, _ = process.Terminate()
				select {
				case <-finished:
				case <-time.After(6 * time.Second):
					t.Error("completion goroutine did not stop during cleanup")
				}
			})

			select {
			case <-observation.errObserved:
				cancel()
				select {
				case <-process.group.WaitDone():
				case <-time.After(5 * time.Second):
					t.Fatal("leader wait did not complete after cancellation")
				}
				release()
			case <-observation.awaitObserved:
				if test.completedFirst {
					t.Fatal("completed wait was not observed before Await")
				}
				cancel()
			case <-time.After(5 * time.Second):
				t.Fatal("consumer completion did not reach a lifecycle observation")
			}

			select {
			case completed := <-result:
				if test.completedFirst {
					var exitErr *exec.ExitError
					if errors.Is(completed.commandErr, context.Canceled) || !errors.As(completed.commandErr, &exitErr) || exitErr.ExitCode() != -1 {
						t.Errorf("command error = %v, want completed signal exit without caller cancellation", completed.commandErr)
					}
				} else if !errors.Is(completed.commandErr, context.Canceled) {
					t.Errorf("command error = %v, want context.Canceled", completed.commandErr)
				}
				if completed.termination.ResidualMembers() || completed.terminationErr != nil {
					t.Errorf("cancellation cleanup = %+v, %v", completed.termination, completed.terminationErr)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("Git completion did not return after cancellation")
			}
			assertGitHelperProcessesGone(t, pids)
		})
	}
}

// Only completion uses this wrapper; exec uses the same underlying Context.
// Pausing a live Err snapshot lets cancellation and Wait overtake its return.
type gitCompletionObservationContext struct {
	context.Context
	errObserved   chan struct{}
	releaseErr    chan struct{}
	awaitObserved chan struct{}
	doneCalls     int
}

func (ctx *gitCompletionObservationContext) Err() error {
	err := ctx.Context.Err()
	if err == nil {
		close(ctx.errObserved)
		<-ctx.releaseErr
	}
	return err
}

func (ctx *gitCompletionObservationContext) Done() <-chan struct{} {
	ctx.doneCalls++
	// Completion subscribes once; Await subscribes again while the wait is live.
	if ctx.doneCalls == 2 {
		close(ctx.awaitObserved)
	}
	return ctx.Context.Done()
}
