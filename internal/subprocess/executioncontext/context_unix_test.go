//go:build darwin || linux

package executioncontext

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/executionidentity"
)

func TestNativeContextAndRecordedGroupObservationDoNotSignal(t *testing.T) {
	current, err := ObserveExecutionContext(t.Context())
	if err != nil || !current.HasVisibility() || current.Validate() != nil {
		t.Fatal("native execution context qualification failed", err)
	}
	command := exec.Command("/bin/sleep", "30")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	wait := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(wait)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		select {
		case <-wait:
		case <-time.After(5 * time.Second):
			t.Error("test-owned child cleanup did not finish")
		}
	})
	invocation, _ := executionidentity.NewInvocation(strings.Repeat("a", 32))
	binding, err := executionidentity.NewBinding(invocation, current, int64(command.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	proof, err := ObserveExecutionQuiescence(t.Context(), binding)
	var refusal *ExecutionObservationError
	if !errors.As(err, &refusal) || refusal.Kind() != ExecutionGroupOccupied || proof.Require(binding) == nil {
		t.Fatal("occupied process group was treated as quiescent", err)
	}
	select {
	case <-wait:
		t.Fatal("recorded-group observation terminated its live target")
	default:
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wait:
	case <-time.After(5 * time.Second):
		t.Fatal("test-owned child did not terminate")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		proof, err = ObserveExecutionQuiescence(t.Context(), binding)
		if err == nil {
			break
		}
		if !errors.As(err, &refusal) ||
			(refusal.Kind() != ExecutionGroupOccupied && refusal.Kind() != ExecutionGroupUnsignalable) || time.Now().After(deadline) {
			t.Fatal("test-owned process group did not settle to observed absence", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || proof.Require(binding) != nil || proof.Basis() != ProcessGroupAbsent {
		t.Fatal("absent group did not produce exact local evidence", err)
	}
	otherInvocation, _ := executionidentity.NewInvocation(strings.Repeat("b", 32))
	other, _ := executionidentity.NewBinding(otherInvocation, current, binding.Group())
	if proof.Require(other) == nil {
		t.Fatal("quiescence evidence transferred to another invocation")
	}
}

func TestNativeContextRejectsForeignViewsAndQualifiesRecordedOldEpoch(t *testing.T) {
	current, err := ObserveExecutionContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	invocation, _ := executionidentity.NewInvocation(strings.Repeat("a", 32))
	otherRealm := strings.Repeat("c", 64)
	otherView := strings.Repeat("d", 64)
	foreign, _ := executionidentity.NewContextWithVisibility(current.Platform(), otherRealm, current.Epoch(), current.Visibility())
	otherNamespace, _ := executionidentity.NewContextWithVisibility(current.Platform(), current.Realm(), current.Epoch(), otherView)
	for _, value := range []executionidentity.Context{foreign, otherNamespace} {
		binding, _ := executionidentity.NewBinding(invocation, value, 123)
		proof, err := ObserveExecutionQuiescence(t.Context(), binding)
		var refusal *ExecutionObservationError
		if !errors.As(err, &refusal) || refusal.Kind() != ExecutionContextIncomparable || proof.Require(binding) == nil {
			t.Fatal("foreign recorded context acquired process evidence", err)
		}
	}
	oldEpoch := "12345678-1234-1234-1234-123456789abc"
	if current.Epoch() == oldEpoch {
		oldEpoch = "22345678-1234-1234-1234-123456789abc"
	}
	before, _ := executionidentity.NewContextWithVisibility(current.Platform(), current.Realm(), oldEpoch, otherView)
	binding, _ := executionidentity.NewBinding(invocation, before, 123)
	proof, err := ObserveExecutionQuiescence(t.Context(), binding)
	if err != nil || proof.Require(binding) != nil || proof.Basis() != PreviousBootEnded {
		t.Fatal("qualified old boot required namespace persistence or group replay", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ObserveExecutionContext(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled observation did not retain cancellation", err)
	}
	if _, err := ObserveExecutionContext(nil); err == nil {
		t.Fatal("missing observation context was accepted")
	}
}
