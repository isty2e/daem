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
	commandContext, cancel := context.WithTimeoutCause(
		ctx,
		productVersionCommandTimeout,
		errDarwinProductVersionTimeout,
	)
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
		return freezeDarwinCommandResult(commandContext, err, stdout.String(), stdout.Truncated())
	}
	waitErr := group.Await(commandContext, subprocess.InheritedOutputCloseWait)
	result := freezeDarwinCommandResult(commandContext, waitErr, stdout.String(), stdout.Truncated())
	termination, terminationErr := group.ReapAfterLeaderExit()
	return joinDarwinCommandCleanup(result, termination, terminationErr)
}

func joinDarwinCommandCleanup(
	result commandResult,
	termination subprocess.ProcessTermination,
	terminationErr error,
) commandResult {
	if result.err == nil {
		result.err = terminationErr
	} else if terminationErr != nil {
		result.err = errors.Join(result.err, terminationErr)
	}
	if occupancyErr := processGroupOccupancyErr(termination); occupancyErr != nil {
		result.err = errors.Join(result.err, occupancyErr)
	}
	return result
}

func processGroupOccupancyErr(termination subprocess.ProcessTermination) error {
	if termination.UnsignalableOccupancy() {
		return errors.New("sw_vers process group occupancy is unsignalable")
	}
	if termination.ResidualMembers() {
		return errors.New("sw_vers exited while process-group members remained")
	}
	return nil
}
