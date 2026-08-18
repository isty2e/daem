package subprocess

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestFinalizeDefaultCommandResultPreservesExitWithoutPostWaitDeadline(t *testing.T) {
	result := finalizeDefaultCommandResult(
		CommandResult{Started: true},
		CommandRequest{},
		nil,
		OutputSnapshot{Text: "out"},
		OutputSnapshot{Text: "err"},
		ProcessTermination{},
		nil,
	)
	if result.TimedOut || result.Canceled || !result.HasExitCode || result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %#v, want successful exit 0", result)
	}
	if result.StdoutTruncated || result.StderrTruncated {
		t.Fatalf("truncation = %t/%t, want complete streams", result.StdoutTruncated, result.StderrTruncated)
	}
}

func TestFinalizeDefaultCommandResultClassifiesTimeoutFromWaitErrorOnly(t *testing.T) {
	result := finalizeDefaultCommandResult(
		CommandResult{Started: true},
		CommandRequest{},
		context.DeadlineExceeded,
		OutputSnapshot{Text: "out"},
		OutputSnapshot{Text: "err"},
		ProcessTermination{processesFound: true},
		nil,
	)
	if !result.TimedOut || result.HasExitCode || !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("result = %#v, want timeout from wait error", result)
	}
	if result.StdoutTruncated || result.StderrTruncated {
		t.Fatalf("truncation = %t/%t, want occupancy not to mark complete streams truncated", result.StdoutTruncated, result.StderrTruncated)
	}
}

func TestFinalizeDefaultCommandResultKeepsStreamTruncationIndependent(t *testing.T) {
	result := finalizeDefaultCommandResult(
		CommandResult{Started: true},
		CommandRequest{},
		nil,
		OutputSnapshot{Text: "complete-stdout"},
		OutputSnapshot{Text: "partial-stderr", Incomplete: true},
		ProcessTermination{processesFound: true},
		nil,
	)
	if result.TimedOut || !result.HasExitCode || result.StdoutTruncated || !result.StderrTruncated {
		t.Fatalf("result = %#v, want complete stdout and incomplete stderr", result)
	}
	if result.Stdout != "complete-stdout" || result.Stderr != "partial-stderr" {
		t.Fatalf("streams = %q/%q", result.Stdout, result.Stderr)
	}
	if result.Err == nil ||
		!strings.Contains(result.Err.Error(), "inherited output descriptors open") ||
		!strings.Contains(result.Err.Error(), "process-group members remained") {
		t.Fatalf("error = %v, want process cleanup separate from stream completeness", result.Err)
	}
}

func TestFinalizeDefaultCommandResultDoesNotTruncateCompleteStreamsForResidualProcess(t *testing.T) {
	result := finalizeDefaultCommandResult(
		CommandResult{Started: true},
		CommandRequest{},
		nil,
		OutputSnapshot{Text: "out"},
		OutputSnapshot{Text: "err"},
		ProcessTermination{processesFound: true},
		nil,
	)
	if result.StdoutTruncated || result.StderrTruncated || result.TimedOut || !result.HasExitCode {
		t.Fatalf("result = %#v, want complete streams with residual-process error", result)
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "process-group members remained") {
		t.Fatalf("error = %v, want residual process-group error", result.Err)
	}
}

func TestFinalizeDefaultCommandResultWaitDelayDoesNotMarkCompleteStreamsTruncated(t *testing.T) {
	result := finalizeDefaultCommandResult(
		CommandResult{Started: true},
		CommandRequest{},
		exec.ErrWaitDelay,
		OutputSnapshot{Text: "out"},
		OutputSnapshot{Text: "err"},
		ProcessTermination{},
		nil,
	)
	if !result.HasExitCode || result.StdoutTruncated || result.StderrTruncated || result.TimedOut {
		t.Fatalf("result = %#v, want exit with complete streams after WaitDelay", result)
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "inherited output descriptors open") {
		t.Fatalf("error = %v, want held-descriptor wait failure", result.Err)
	}
}
