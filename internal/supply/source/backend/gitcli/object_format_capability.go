package gitcli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
)

const objectFormatHelpOutputLimit = 64 << 10

func (resolver Resolver) explicitObjectFormatSupported(ctx context.Context) (bool, error) {
	state, err := resolver.requireState()
	if err != nil {
		return false, err
	}
	state.objectFormatMu.Lock()
	defer state.objectFormatMu.Unlock()
	if state.objectFormatProbed {
		return state.explicitObjectFormat, nil
	}
	supported, err := probeExplicitObjectFormatSupport(ctx)
	if err != nil {
		return false, err
	}
	state.explicitObjectFormat = supported
	state.objectFormatProbed = true
	return supported, nil
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
	output := strings.TrimSpace(result.Stdout() + "\n" + result.Stderr())
	switch {
	case result.TimedOut():
		return false, fmt.Errorf("inspect git object-format capability: timed out")
	case result.Canceled():
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return false, fmt.Errorf("inspect git object-format capability: canceled")
	case result.Reason() == subprocess.CommandReasonMissingRunner:
		return false, fmt.Errorf("inspect git object-format capability: git executable was not found in PATH")
	case output == "" && result.Failed():
		detail := strings.TrimSpace(result.ErrorDetail())
		if detail == "" {
			detail = "git init -h failed"
		}
		return false, fmt.Errorf("inspect git object-format capability: %s", detail)
	case (result.StdoutTruncated() || result.StderrTruncated()) && !gitHelpSupportsExplicitObjectFormat(output):
		return false, fmt.Errorf("inspect git object-format capability: help output was truncated")
	default:
		return gitHelpSupportsExplicitObjectFormat(output), nil
	}
}

func gitHelpSupportsExplicitObjectFormat(help string) bool {
	return strings.Contains(help, "--object-format")
}
