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
		return freezeDarwinCommandResult(ctx, err, stdout.String(), stdout.Truncated())
	}
	waitErr := group.Await(commandContext, subprocess.InheritedOutputCloseWait)
	termination, terminationErr := group.ReapAfterLeaderExit()
	if waitErr == nil {
		waitErr = terminationErr
	} else if terminationErr != nil {
		waitErr = errors.Join(waitErr, terminationErr)
	}
	if occupancyErr := processGroupOccupancyErr(termination); occupancyErr != nil {
		waitErr = errors.Join(waitErr, occupancyErr)
	}
	return freezeDarwinCommandResult(ctx, waitErr, stdout.String(), stdout.Truncated())
}

func processGroupOccupancyErr(termination subprocess.ProcessTermination) error {
	if termination.UnsignalableOccupancy() {
		return errors.New("sw_vers process group occupancy is unsignalable")
	}
	if termination.ProcessesFound() {
		return errors.New("sw_vers exited while process-group members remained")
	}
	return nil
}
