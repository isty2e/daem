package gitcli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
)

const (
	objectFormatHelpOutputLimit = 64 << 10
	gitHelpUsageExitCode        = 129
)

type objectFormatCapabilityProbe struct {
	done      chan struct{}
	supported bool
	err       error
}

func (resolver Resolver) explicitObjectFormatSupported(ctx context.Context) (bool, error) {
	state, err := resolver.requireState()
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	state.objectFormatMu.Lock()
	if state.objectFormatProbed {
		supported := state.explicitObjectFormat
		state.objectFormatMu.Unlock()
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return supported, nil
	}
	inflight := state.objectFormatInflight
	if inflight == nil {
		inflight = &objectFormatCapabilityProbe{done: make(chan struct{})}
		state.objectFormatInflight = inflight
		state.objectFormatMu.Unlock()

		supported, err := probeExplicitObjectFormatSupport(ctx)
		state.objectFormatMu.Lock()
		inflight.supported = supported
		inflight.err = err
		if err == nil {
			state.explicitObjectFormat = supported
			state.objectFormatProbed = true
		}
		state.objectFormatInflight = nil
		close(inflight.done)
		state.objectFormatMu.Unlock()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		return supported, err
	}
	state.objectFormatMu.Unlock()

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-inflight.done:
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return inflight.supported, inflight.err
	}
}

func probeExplicitObjectFormatSupport(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Timeout:     subprocess.DefaultCommandTimeout,
		OutputLimit: objectFormatHelpOutputLimit,
	})
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, context.DeadlineExceeded
		}
		executor = subprocess.NewCommandExecutor(subprocess.CommandOptions{
			Timeout:     remaining,
			OutputLimit: objectFormatHelpOutputLimit,
		})
	}
	result := executor.Execute(ctx, subprocess.CommandAttemptRequest{
		Command:     gitExecutable,
		Args:        inspectGitInitHelpArgs(),
		OutputLimit: objectFormatHelpOutputLimit,
	})
	if err := ctx.Err(); err != nil {
		return false, err
	}
	output := strings.TrimSpace(result.Stdout() + "\n" + result.Stderr())
	switch {
	case result.TimedOut():
		return false, fmt.Errorf("inspect git object-format capability: timed out")
	case result.Canceled():
		return false, fmt.Errorf("inspect git object-format capability: canceled")
	case result.Reason() == subprocess.CommandReasonMissingRunner:
		return false, fmt.Errorf("inspect git object-format capability: git executable was not found in PATH")
	case !gitInitHelpAdmitted(result):
		detail := strings.TrimSpace(result.ErrorDetail())
		if detail == "" {
			detail = "git init -h failed"
		}
		return false, fmt.Errorf("inspect git object-format capability: %s", detail)
	case output == "":
		return false, fmt.Errorf("inspect git object-format capability: git init -h returned empty output")
	case (result.StdoutTruncated() || result.StderrTruncated()) && !gitHelpSupportsExplicitObjectFormat(output):
		return false, fmt.Errorf("inspect git object-format capability: help output was truncated")
	default:
		return gitHelpSupportsExplicitObjectFormat(output), nil
	}
}

func gitInitHelpAdmitted(result subprocess.CommandAttemptResult) bool {
	if result.Succeeded() {
		return true
	}
	exitCode, ok := result.ExitCode()
	return ok && exitCode == gitHelpUsageExitCode
}

func gitHelpSupportsExplicitObjectFormat(help string) bool {
	return strings.Contains(help, "--object-format")
}
