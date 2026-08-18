package gitcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
)

type gitProcess struct {
	stdout           *os.File
	stderr           *os.File
	group            *subprocess.ProcessGroup
	stderrDone       chan error
	diagnostic       gitDiagnosticBuffer
	diagnosticPolicy subprocess.CapturePolicy
}

type gitProcessResult struct {
	commandErr       error
	termination      subprocess.ProcessTermination
	terminationErr   error
	stderrReadErr    error
	outputIncomplete bool
}

func startGitProcess(command *exec.Cmd) (*gitProcess, error) {
	if command == nil {
		return nil, fmt.Errorf("start git process: command is required")
	}
	environment := subprocess.ChildEnvironmentFrom(command.Environ())
	diagnosticPolicy := subprocess.NewCapturePolicy(
		environment.SecretValues(),
		maxGitDiagnosticRunes,
	)

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("open git stdout pipe: %w", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		return nil, fmt.Errorf("open git stderr pipe: %w", err)
	}
	closePipes := func() {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
	}

	command.Stdout = stdoutWriter
	command.Stderr = stderrWriter
	group, err := subprocess.BindProcessGroup(command)
	if err != nil {
		closePipes()
		return nil, fmt.Errorf("supervise git process group: %w", err)
	}
	if err := command.Start(); err != nil {
		closePipes()
		return nil, err
	}
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	process := &gitProcess{
		stdout:           stdoutReader,
		stderr:           stderrReader,
		group:            group,
		stderrDone:       make(chan error, 1),
		diagnosticPolicy: diagnosticPolicy,
	}
	group.StartWait()
	go func() {
		_, readErr := io.Copy(&process.diagnostic, process.stderr)
		process.stderrDone <- readErr
	}()
	return process, nil
}

func (process *gitProcess) Stdout() io.Reader {
	return process.stdout
}

func (process *gitProcess) Terminate() (subprocess.ProcessTermination, error) {
	return process.group.Terminate()
}

func (process *gitProcess) closeOutputReaders() {
	_ = process.stdout.Close()
	_ = process.stderr.Close()
}

func (process *gitProcess) finishStderr() (error, bool) {
	timer := time.NewTimer(subprocess.InheritedOutputCloseWait)
	defer timer.Stop()
	select {
	case err := <-process.stderrDone:
		_ = process.stderr.Close()
		return err, false
	case <-timer.C:
		_ = process.stderr.Close()
		return <-process.stderrDone, true
	}
}

func completeGitProcess(
	ctx context.Context,
	process *gitProcess,
	consume func(io.Reader) error,
) (error, gitProcessResult) {
	if ctx == nil {
		ctx = context.Background()
	}
	consumeDone := make(chan error, 1)
	go func() {
		consumeDone <- consume(process.Stdout())
	}()

	var drainTimer *time.Timer
	var drain <-chan time.Time
	startDrain := func() {
		if drain != nil {
			return
		}
		drainTimer = time.NewTimer(subprocess.InheritedOutputCloseWait)
		drain = drainTimer.C
	}
	defer func() {
		if drainTimer != nil {
			drainTimer.Stop()
		}
	}()

	ctxDone := ctx.Done()
	waitDone := process.group.WaitDone()
	var consumeErr error
	incomplete := false
	for {
		select {
		case consumeErr = <-consumeDone:
			if consumeErr != nil {
				_, _ = process.Terminate()
			}
			goto waited
		case <-ctxDone:
			_, _ = process.Terminate()
			startDrain()
			ctxDone = nil
		case <-waitDone:
			startDrain()
			waitDone = nil
		case <-drain:
			process.closeOutputReaders()
			consumeErr = <-consumeDone
			incomplete = consumeErr != nil
			goto waited
		}
	}

waited:
	waitErr := process.group.Await(ctx, subprocess.InheritedOutputCloseWait)
	termination, terminationErr := process.group.ReapAfterLeaderExit()
	stderrReadErr, stderrIncomplete := process.finishStderr()
	_ = process.stdout.Close()
	outputIncomplete := incomplete || stderrIncomplete || errors.Is(waitErr, subprocess.ErrProcessWaitAbandoned)
	stderrTruncated := process.diagnostic.Truncated() || stderrIncomplete
	if waitErr != nil {
		waitErr = gitCommandErrorWithCapture(
			process.diagnosticPolicy,
			waitErr,
			process.diagnostic.String(),
			stderrTruncated,
		)
	}
	return consumeErr, gitProcessResult{
		commandErr:       waitErr,
		termination:      termination,
		terminationErr:   terminationErr,
		stderrReadErr:    stderrReadErr,
		outputIncomplete: outputIncomplete,
	}
}

func gitAttemptContextErr(consumeErr error, result gitProcessResult) error {
	if errors.Is(consumeErr, context.DeadlineExceeded) || errors.Is(result.commandErr, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(consumeErr, context.Canceled) || errors.Is(result.commandErr, context.Canceled) {
		return context.Canceled
	}
	return nil
}

func joinGitProcessGroupTerminateErr(base error, role string, result gitProcessResult) error {
	if result.termination.UnsignalableOccupancy() {
		cause := result.terminationErr
		if cause == nil {
			cause = errors.New("process group occupancy is unsignalable")
		}
		return errors.Join(base, fmt.Errorf("%s process group occupancy is unsignalable: %w", role, cause))
	}
	if result.terminationErr != nil {
		return errors.Join(base, fmt.Errorf("terminate %s process group: %w", role, result.terminationErr))
	}
	if errors.Is(result.commandErr, subprocess.ErrProcessWaitAbandoned) {
		return errors.Join(base, result.commandErr)
	}
	if errors.Is(base, context.Canceled) || errors.Is(base, context.DeadlineExceeded) {
		return base
	}
	if result.outputIncomplete {
		return errors.Join(base, fmt.Errorf("%s output was incomplete after pipe drain bound", role))
	}
	return base
}

func joinGitProcessGroupResidual(base error, role string, result gitProcessResult) error {
	base = joinGitProcessGroupTerminateErr(base, role, result)
	if result.termination.UnsignalableOccupancy() {
		return base
	}
	if result.termination.ProcessesFound() {
		return errors.Join(base, fmt.Errorf("%s exited while process-group members remained; terminated residual process group", role))
	}
	return base
}
