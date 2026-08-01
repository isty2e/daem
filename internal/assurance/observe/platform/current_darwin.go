//go:build darwin

package platform

import (
	"context"
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
	err := command.Run()
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}
	return commandResult{
		stdout:          stdout.String(),
		stdoutTruncated: stdout.Truncated(),
		timedOut:        commandContext.Err() == context.DeadlineExceeded && ctx.Err() == nil,
		err:             err,
	}
}
