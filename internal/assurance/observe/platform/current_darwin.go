//go:build darwin

package platform

import (
	"context"
	"errors"
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
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Timeout:     productVersionCommandTimeout,
		OutputLimit: productVersionOutputLimit,
	})
	result := executor.Execute(ctx, subprocess.CommandAttemptRequest{
		Command:     "/usr/bin/sw_vers",
		Args:        []string{"--productVersion"},
		OutputLimit: productVersionOutputLimit,
	})
	var err error
	if !result.Succeeded() {
		if detail := result.ErrorDetail(); detail != "" {
			err = errors.New(detail)
		} else {
			err = errors.New("sw_vers failed")
		}
	}
	return commandResult{
		stdout:          result.Stdout(),
		stdoutTruncated: result.StdoutTruncated(),
		timedOut:        result.TimedOut(),
		err:             err,
	}
}
