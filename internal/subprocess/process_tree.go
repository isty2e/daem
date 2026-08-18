package subprocess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	defaultQuiescencePeriod = 50 * time.Millisecond
	defaultGracePeriod      = 500 * time.Millisecond
	defaultKillWait         = 2 * time.Second

	// InheritedOutputCloseWait bounds parent-side pipe draining after leader
	// exit or cancellation. Session-escaped descendants are outside dedicated
	// process-group cleanup and may keep inherited writers open.
	InheritedOutputCloseWait = 250 * time.Millisecond
)

var (
	errProcessTreeUnsupported   = errors.New("process-group supervision is unsupported")
	errProcessGroupUnsignalable = errors.New("process group occupancy is unsignalable")
	// ErrProcessWaitAbandoned reports that command.Wait was left running in the
	// background after termination because the leader did not exit in bound.
	ErrProcessWaitAbandoned = errors.New("process wait abandoned while occupancy remained")
)

type processTerminationOptions struct {
	QuiescencePeriod time.Duration
	GracePeriod      time.Duration
	KillWait         time.Duration
}

// ProcessTermination describes one idempotent process-group termination attempt.
type ProcessTermination struct {
	processesFound        bool
	escalated             bool
	unsignalableOccupancy bool
}

// ProcessesFound reports whether the dedicated group still had signalable
// members when termination began.
func (termination ProcessTermination) ProcessesFound() bool {
	return termination.processesFound
}

// UnsignalableOccupancy reports that the dedicated pgid was occupied but not
// signalable. That is not proof of in-group descendants.
func (termination ProcessTermination) UnsignalableOccupancy() bool {
	return termination.unsignalableOccupancy
}

// ProcessGroup owns termination and bounded leader waiting for one command's
// fresh dedicated process group.
type ProcessGroup struct {
	command *exec.Cmd
	options processTerminationOptions

	terminateOnce sync.Once
	terminateDone chan struct{}
	termination   ProcessTermination
	terminateErr  error

	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
}

// afterProcessGroupWaitDone is a test hook invoked after command.Wait returns
// and before Await classifies a still-live attempt context against that wait.
var afterProcessGroupWaitDone func()

// BindProcessGroup configures command to create a fresh process group and replaces the
// CommandContext direct-child cancellation hook with dedicated-group cleanup.
// BindProcessGroup must be called before command.Start. Session-escaped
// descendants are outside this primitive.
func BindProcessGroup(command *exec.Cmd) (*ProcessGroup, error) {
	return bindProcessGroupWithOptions(command, processTerminationOptions{})
}

func bindProcessGroupWithOptions(command *exec.Cmd, options processTerminationOptions) (*ProcessGroup, error) {
	if command == nil {
		return nil, fmt.Errorf("bind process group: command is required")
	}
	if command.Process != nil {
		return nil, fmt.Errorf("bind process group: command already started")
	}
	if command.Cancel == nil {
		return nil, fmt.Errorf("bind process group: command must be created with exec.CommandContext")
	}
	if err := configureDedicatedProcessGroup(command); err != nil {
		return nil, fmt.Errorf("bind process group: %w", err)
	}
	group := &ProcessGroup{
		command:       command,
		options:       options.withDefaults(),
		terminateDone: make(chan struct{}),
	}
	command.Cancel = group.cancel
	return group, nil
}

// Terminate gracefully stops every member of the bound process group, then
// escalates and verifies disappearance within the configured bounds.
func (group *ProcessGroup) Terminate() (ProcessTermination, error) {
	return group.terminate(false)
}

// ReapAfterLeaderExit allows short-lived in-group descendants to finish
// naturally before terminating and reporting residual group members.
func (group *ProcessGroup) ReapAfterLeaderExit() (ProcessTermination, error) {
	return group.terminate(true)
}

func (group *ProcessGroup) terminate(quiesce bool) (ProcessTermination, error) {
	if group == nil || group.command == nil {
		return ProcessTermination{}, fmt.Errorf("terminate process group: group is required")
	}
	if group.command.Process == nil {
		return ProcessTermination{}, fmt.Errorf("terminate process group: command has not started")
	}

	group.terminateOnce.Do(func() {
		group.termination, group.terminateErr = terminateDedicatedProcessGroup(
			group.command.Process.Pid,
			group.options,
			quiesce,
		)
		close(group.terminateDone)
	})
	<-group.terminateDone
	return group.termination, group.terminateErr
}

func (group *ProcessGroup) cancel() error {
	termination, err := group.Terminate()
	if err != nil {
		return err
	}
	if !termination.ProcessesFound() {
		return os.ErrProcessDone
	}
	return nil
}

// StartWait begins command.Wait in the background so callers can return after
// termination without abandoning eventual child reaping. It must run after Start.
//
// Wait closes exec-managed StdinPipe, StdoutPipe, and StderrPipe after the
// leader exits. Callers that use those pipes must finish or abandon the parent
// reads, typically by closing the parent ends, before StartWait, WaitDone, or
// Await. Caller-owned os.Pipe readers and cmd.Stdout/Stderr writers are
// different closer models; Wait does not close those parent readers.
func (group *ProcessGroup) StartWait() {
	if group == nil || group.command == nil {
		return
	}
	group.waitOnce.Do(func() {
		group.waitDone = make(chan struct{})
		go func() {
			group.waitErr = group.command.Wait()
			close(group.waitDone)
		}()
	})
}

// WaitDone is closed after command.Wait returns. It starts the background wait
// if needed.
func (group *ProcessGroup) WaitDone() <-chan struct{} {
	if group == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	group.StartWait()
	return group.waitDone
}

// Await waits for the leader to exit. After ctx is done or the dedicated group
// has already been terminated, it returns if the leader has not exited within
// abandonBound. command.Wait continues in the background so the child can
// still be reaped.
//
// Once command.Wait has returned, that wait result is frozen. A later expired
// attempt context does not rewrite a process-owned exit. CommandContext kill
// during this Await still classifies timeout or cancel from a wait that has
// not completed yet, or from a non-owned wait (typically a signal) that races
// with ctx.Done.
func (group *ProcessGroup) Await(ctx context.Context, abandonBound time.Duration) error {
	if group == nil {
		return fmt.Errorf("await process group: group is required")
	}
	if abandonBound <= 0 {
		abandonBound = InheritedOutputCloseWait
	}
	if ctx == nil {
		ctx = context.Background()
	}
	group.StartWait()
	select {
	case <-group.waitDone:
		invokeAfterProcessGroupWaitDone()
		return completedWaitErr(group.waitErr, ctx.Err())
	default:
	}
	select {
	case <-group.waitDone:
		invokeAfterProcessGroupWaitDone()
		return completedWaitErr(group.waitErr, ctx.Err())
	case <-ctx.Done():
		_, terminateErr := group.Terminate()
		return group.finishAwait(abandonBound, ctx.Err(), terminateErr)
	case <-group.terminateDone:
		return group.finishAwait(abandonBound, ctx.Err(), group.terminateErr)
	}
}

func (group *ProcessGroup) finishAwait(abandonBound time.Duration, ctxErr error, terminateErr error) error {
	timer := time.NewTimer(abandonBound)
	defer timer.Stop()
	select {
	case <-group.waitDone:
		invokeAfterProcessGroupWaitDone()
		return completedWaitErr(group.waitErr, ctxErr)
	case <-timer.C:
		return errors.Join(ctxErr, terminateErr, ErrProcessWaitAbandoned)
	}
}

func invokeAfterProcessGroupWaitDone() {
	if hook := afterProcessGroupWaitDone; hook != nil {
		hook()
	}
}

func completedWaitErr(waitErr error, ctxErr error) error {
	if processOwnedWaitResult(waitErr) {
		return waitErr
	}
	if ctxErr != nil {
		return errors.Join(ctxErr, waitErr)
	}
	return waitErr
}

func processOwnedWaitResult(err error) bool {
	if err == nil || errors.Is(err, exec.ErrWaitDelay) {
		return true
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() >= 0
}

func (options processTerminationOptions) withDefaults() processTerminationOptions {
	if options.QuiescencePeriod <= 0 {
		options.QuiescencePeriod = defaultQuiescencePeriod
	}
	if options.GracePeriod <= 0 {
		options.GracePeriod = defaultGracePeriod
	}
	if options.KillWait <= 0 {
		options.KillWait = defaultKillWait
	}
	return options
}
