package subprocess

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestBindRejectsCommandWithoutContext(t *testing.T) {
	_, err := BindProcessGroup(exec.Command("unused-daem-test-command"))
	if err == nil || !strings.Contains(err.Error(), "exec.CommandContext") {
		t.Fatalf("BindProcessGroup error = %v, want CommandContext requirement", err)
	}
}

func TestJoinCommandProcessGroupCleanupReportsUnsignalableWithoutDescendantLanguage(t *testing.T) {
	err := joinCommandProcessGroupCleanup(
		nil,
		ProcessTermination{processesFound: true, unsignalableOccupancy: true},
		errProcessGroupUnsignalable,
	)
	if err == nil || !errors.Is(err, errProcessGroupUnsignalable) {
		t.Fatalf("cleanup error = %v, want unsignalable occupancy", err)
	}
	message := err.Error()
	if strings.Contains(message, "descendant") || strings.Contains(message, "process tree") {
		t.Fatalf("cleanup error = %q, want no spawn-tree residual claim", message)
	}
	if !strings.Contains(message, "unsignalable") {
		t.Fatalf("cleanup error = %q, want unsignalable occupancy language", message)
	}
}

func TestJoinCommandProcessGroupCleanupDoesNotTreatInitialOccupancyAsResidual(t *testing.T) {
	err := joinCommandProcessGroupCleanup(
		context.Canceled,
		ProcessTermination{processesFound: true},
		nil,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup error = %v, want cancellation without residual diagnostic", err)
	}
	if strings.Contains(err.Error(), "process-group members remained") {
		t.Fatalf("cleanup error = %q, want leader-only occupancy not to claim residual members", err.Error())
	}
}

func TestJoinCommandProcessGroupCleanupReportsResidualMembers(t *testing.T) {
	err := joinCommandProcessGroupCleanup(
		nil,
		ProcessTermination{processesFound: true, residualMembers: true},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "process-group members remained") {
		t.Fatalf("cleanup error = %v, want residual-member diagnostic", err)
	}
}

func TestProcessOwnedWaitResult(t *testing.T) {
	t.Parallel()
	if !processOwnedWaitResult(nil) {
		t.Fatal("nil wait is process-owned")
	}
	if !processOwnedWaitResult(exec.ErrWaitDelay) {
		t.Fatal("ErrWaitDelay is process-owned")
	}
	if processOwnedWaitResult(context.Canceled) {
		t.Fatal("context.Canceled is not process-owned")
	}
	if processOwnedWaitResult(errors.Join(context.DeadlineExceeded, errors.New("signal: killed"))) {
		t.Fatal("joined deadline is not process-owned")
	}
}

func TestCompletedWaitErrDoesNotJoinContextOverProcessOwnedExit(t *testing.T) {
	t.Parallel()
	if got := completedWaitErr(nil, context.DeadlineExceeded); got != nil {
		t.Fatalf("successful wait = %v, want frozen nil", got)
	}
	if got := completedWaitErr(exec.ErrWaitDelay, context.Canceled); !errors.Is(got, exec.ErrWaitDelay) || errors.Is(got, context.Canceled) {
		t.Fatalf("wait delay = %v, want process-owned wait delay", got)
	}
	if got := completedWaitErr(context.Canceled, context.DeadlineExceeded); !errors.Is(got, context.DeadlineExceeded) || !errors.Is(got, context.Canceled) {
		t.Fatalf("non-owned wait = %v, want joined context", got)
	}
}
