package subprocess

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func defaultCommandRunner(ctx context.Context, request CommandRequest) CommandResult {
	path, err := exec.LookPath(request.Command)
	if err != nil {
		return CommandResult{MissingRunner: true, Err: err}
	}

	var cmd *exec.Cmd
	if request.nativeWorkDir != nil {
		cmd, err = PrepareCommandInWorkingDirectory(ctx, path, request.Args, request.Env, request.nativeWorkDir)
		if err != nil {
			return CommandResult{WorkDirAuthorityFailed: true, Err: err}
		}
	} else {
		cmd = exec.CommandContext(ctx, path, request.Args...)
		cmd.Env = append([]string(nil), request.Env...)
	}
	if request.nativeWorkDir == nil && strings.TrimSpace(request.WorkDir) != "" {
		cmd.Dir = request.WorkDir
	}
	if request.Stdin != "" {
		cmd.Stdin = strings.NewReader(request.Stdin)
	}
	outputLimit := request.OutputLimit
	if outputLimit <= 0 {
		outputLimit = DefaultCommandOutputLimit
	}
	stdout := NewBoundedBuffer(outputLimit)
	stderr := NewBoundedBuffer(outputLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// A successful leader may leave descendants holding inherited output
	// descriptors. Bound exec's pipe wait so process-group reaping can run.
	cmd.WaitDelay = InheritedOutputCloseWait
	group, err := BindProcessGroup(cmd)
	if err != nil {
		return CommandResult{Err: err}
	}

	if err := cmd.Start(); err != nil {
		return CommandResult{Err: err}
	}
	result := CommandResult{Started: true}
	err = group.Await(ctx, InheritedOutputCloseWait)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.StdoutTruncated = stdout.Truncated()
	result.StderrTruncated = stderr.Truncated()
	termination, terminationErr := group.ReapAfterLeaderExit()
	if termination.ProcessesFound() || termination.UnsignalableOccupancy() || terminationErr != nil || errors.Is(err, ErrProcessWaitAbandoned) {
		// Forced, unsignalable, or abandoned wait means the command did not
		// reach natural output closure.
		result.StdoutTruncated = true
		result.StderrTruncated = true
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.Err = joinCommandProcessGroupCleanup(errors.Join(ctx.Err(), err), termination, terminationErr)
		return result
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		result.Canceled = true
		result.Err = joinCommandProcessGroupCleanup(errors.Join(ctx.Err(), err), termination, terminationErr)
		return result
	}
	outputDescriptorsHeldOpen := errors.Is(err, exec.ErrWaitDelay)
	if err == nil || outputDescriptorsHeldOpen {
		result.HasExitCode = true
		if outputDescriptorsHeldOpen {
			// WaitDelay does not identify which copy pipe it closed. Mark both
			// fields incomplete so downstream redaction treats any secret
			// suffix at the capture boundary conservatively.
			result.StdoutTruncated = true
			result.StderrTruncated = true
			result.Err = errors.New("command exited while descendant processes kept inherited output descriptors open")
		}
	} else {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() < 0 {
				result.Signaled = true
			} else {
				result.HasExitCode = true
				result.ExitCode = exitErr.ExitCode()
			}
		}
		result.Err = err
	}
	if request.nativeWorkDir != nil && result.HasExitCode && result.ExitCode == 126 {
		result.WorkDirAuthorityFailed = reportsWorkingDirectorySetupFailure(result.Stderr)
		result.RunnerSetupFailed = reportsTargetExecFailure(result.Stderr)
		if result.WorkDirAuthorityFailed || result.RunnerSetupFailed {
			// Exit 126 belongs to the descriptor helper, not the requested target.
			result.ExitCode = 0
			result.HasExitCode = false
		}
	}
	result.Err = joinCommandProcessGroupCleanup(result.Err, termination, terminationErr)
	if errors.Is(err, ErrProcessWaitAbandoned) {
		result.Err = errors.Join(result.Err, err)
	}
	return result
}

func joinCommandProcessGroupCleanup(resultErr error, termination ProcessTermination, terminationErr error) error {
	if termination.UnsignalableOccupancy() {
		cause := terminationErr
		if cause == nil {
			cause = errProcessGroupUnsignalable
		}
		return errors.Join(resultErr, fmt.Errorf("command process group occupancy is unsignalable: %w", cause))
	}
	if terminationErr != nil {
		return errors.Join(resultErr, fmt.Errorf("terminate command process group: %w", terminationErr))
	}
	if termination.ProcessesFound() {
		return errors.Join(resultErr, errors.New("command exited while process-group members remained; terminated residual process group"))
	}
	return resultErr
}
