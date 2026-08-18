//go:build darwin

package platform

import (
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
)

const (
	productVersionCommandTimeout = 5 * time.Second
	productVersionOutputLimit    = 256
)

func currentCommandRunner() (commandRunner, bool) {
	return runProductVersionCommand, true
}

func runProductVersionCommand(ctx context.Context) commandResult {
	commandContext, cancel := context.WithTimeout(ctx, productVersionCommandTimeout)
	defer cancel()

	stdout := subprocess.NewBoundedBuffer(productVersionOutputLimit)
	stderr := subprocess.NewBoundedBuffer(productVersionOutputLimit)
	command := exec.CommandContext(commandContext, "/usr/bin/sw_vers", "--productVersion")
	command.Env = []string{"LANG=C", "LC_ALL=C"}
	command.Stdout = stdout
	command.Stderr = stderr
	command.WaitDelay = subprocess.InheritedOutputCloseWait
	group, err := subprocess.BindProcessGroup(command)
	if err != nil {
		return commandResult{err: err}
	}
	if err := command.Start(); err != nil {
		return commandResult{
			timedOut: commandContext.Err() == context.DeadlineExceeded && ctx.Err() == nil,
			err:      err,
		}
	}
	waitErr := group.Await(commandContext, subprocess.InheritedOutputCloseWait)
	termination, terminationErr := group.ReapAfterLeaderExit()
	if err := ctx.Err(); err != nil {
		waitErr = err
	}
	if waitErr == nil {
		waitErr = terminationErr
	} else if terminationErr != nil {
		waitErr = errors.Join(waitErr, terminationErr)
	}
	truncated := stdout.Truncated() ||
		termination.ProcessesFound() ||
		termination.UnsignalableOccupancy() ||
		errors.Is(waitErr, subprocess.ErrProcessWaitAbandoned)
	return commandResult{
		stdout:          stdout.String(),
		stdoutTruncated: truncated,
		timedOut:        commandContext.Err() == context.DeadlineExceeded && ctx.Err() == nil,
		err:             waitErr,
	}
}
