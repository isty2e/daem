package recover

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/target"
)

func TestExecuteRejectsNilAndUnavailablePreparedRecoveries(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		_, err := Execute(context.Background(), nil, ExecuteOptions{})
		if !errors.Is(err, ErrPreparedRecoveryUnavailable) {
			t.Fatalf("Execute error = %v, want ErrPreparedRecoveryUnavailable", err)
		}
	})

	t.Run("zero value", func(t *testing.T) {
		_, err := Execute(context.Background(), &PreparedRecovery{}, ExecuteOptions{})
		if !errors.Is(err, ErrPreparedRecoveryUnavailable) {
			t.Fatalf("Execute error = %v, want ErrPreparedRecoveryUnavailable", err)
		}
	})
}

func TestPreparedRecoveryClosePreventsExecution(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	prepared, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}

	_, err = Execute(context.Background(), prepared, ExecuteOptions{})
	if !errors.Is(err, ErrPreparedRecoveryClosed) {
		t.Fatalf("Execute error = %v, want ErrPreparedRecoveryClosed", err)
	}
	if content, readErr := os.ReadFile(fixture.hostPath); readErr != nil {
		t.Fatal(readErr)
	} else if string(content) != string(fixture.newContent) {
		t.Fatalf("host content = %q, want unchanged post-apply content", content)
	}
	if _, statErr := os.Stat(fixture.operationDir); statErr != nil {
		t.Fatalf("closed recovery removed journal: %v", statErr)
	}
}

func TestPreparedRecoveryCopiesShareOneLifecycle(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	prepared, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := *prepared

	if err := duplicate.Close(); err != nil {
		t.Fatalf("duplicate Close returned error: %v", err)
	}
	if _, err := Execute(context.Background(), prepared, ExecuteOptions{}); !errors.Is(err, ErrPreparedRecoveryClosed) {
		t.Fatalf("original Execute error = %v, want ErrPreparedRecoveryClosed", err)
	}
	if content, readErr := os.ReadFile(fixture.hostPath); readErr != nil {
		t.Fatal(readErr)
	} else if string(content) != string(fixture.newContent) {
		t.Fatalf("host content = %q, want unchanged post-apply content", content)
	}
	if _, statErr := os.Stat(fixture.operationDir); statErr != nil {
		t.Fatalf("copied recovery removed journal: %v", statErr)
	}
}

func TestPreparedRecoveryConsumesExactlyOnce(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	prepared, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), prepared, ExecuteOptions{}); err != nil {
		t.Fatalf("first Execute returned error: %v", err)
	}
	if _, err := Execute(context.Background(), prepared, ExecuteOptions{}); !errors.Is(err, ErrPreparedRecoveryConsumed) {
		t.Fatalf("second Execute error = %v, want ErrPreparedRecoveryConsumed", err)
	}
}

func TestPreparedRecoverySerializesConcurrentExecute(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	prepared, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsByAttempt := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)

	for range 2 {
		go func() {
			ready.Done()
			<-start
			_, err := Execute(context.Background(), prepared, ExecuteOptions{})
			errorsByAttempt <- err
		}()
	}
	ready.Wait()
	close(start)

	var successCount int
	var consumedCount int
	for range 2 {
		err := <-errorsByAttempt
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, ErrPreparedRecoveryConsumed):
			consumedCount++
		default:
			t.Fatalf("concurrent Execute error = %v, want success or consumed", err)
		}
	}
	if successCount != 1 || consumedCount != 1 {
		t.Fatalf("outcomes success=%d consumed=%d, want 1 each", successCount, consumedCount)
	}
}

func TestPreparedRecoverySerializesCloseAgainstExecute(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	prepared, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	executeResult := make(chan error, 1)
	closeResult := make(chan error, 1)
	go func() {
		<-start
		_, err := Execute(context.Background(), prepared, ExecuteOptions{})
		executeResult <- err
	}()
	go func() {
		<-start
		closeResult <- prepared.Close()
	}()
	close(start)

	executeErr := <-executeResult
	if executeErr != nil && !errors.Is(executeErr, ErrPreparedRecoveryClosed) {
		t.Fatalf("concurrent Execute error = %v, want nil or closed", executeErr)
	}
	if closeErr := <-closeResult; closeErr != nil {
		t.Fatalf("concurrent Close error = %v", closeErr)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
}

func TestPreparedRecoveryCancellationConsumesWithoutEffects(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	prepared, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Execute(ctx, prepared, ExecuteOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context.Canceled", err)
	}
	if _, err := Execute(context.Background(), prepared, ExecuteOptions{}); !errors.Is(err, ErrPreparedRecoveryConsumed) {
		t.Fatalf("second Execute error = %v, want ErrPreparedRecoveryConsumed", err)
	}
	if content, readErr := os.ReadFile(fixture.hostPath); readErr != nil {
		t.Fatal(readErr)
	} else if string(content) != string(fixture.newContent) {
		t.Fatalf("host content = %q, want unchanged post-apply content", content)
	}
	if _, statErr := os.Stat(fixture.operationDir); statErr != nil {
		t.Fatalf("canceled recovery removed journal: %v", statErr)
	}
}

func TestPreparedRecoveryBlockedPlanConsumesWithoutEffects(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	dirty := []byte("neither before nor expected after\n")
	writeRecoverTestFile(t, fixture.hostPath, dirty)
	prepared, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	active, ok := journal.ActiveRecoveryPlan(prepared.Disclosure())
	if !ok {
		t.Fatal("blocked prepared recovery did not disclose an active plan")
	}
	if classification := active.Classification(); classification != recovery.ClassificationBlocked {
		t.Fatalf("classification = %q, want blocked", classification)
	}

	if _, err := Execute(context.Background(), prepared, ExecuteOptions{}); err == nil || !strings.Contains(err.Error(), "recovery is blocked") {
		t.Fatalf("Execute error = %v, want blocked recovery", err)
	}
	if _, err := Execute(context.Background(), prepared, ExecuteOptions{}); !errors.Is(err, ErrPreparedRecoveryConsumed) {
		t.Fatalf("second Execute error = %v, want ErrPreparedRecoveryConsumed", err)
	}
	if content, readErr := os.ReadFile(fixture.hostPath); readErr != nil {
		t.Fatal(readErr)
	} else if string(content) != string(dirty) {
		t.Fatalf("blocked host content changed to %q", content)
	}
	if _, statErr := os.Stat(fixture.operationDir); statErr != nil {
		t.Fatalf("blocked recovery removed journal: %v", statErr)
	}
}

func TestPreparedRecoveryPreservesContextPrecedenceOverBlockedPlan(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	writeRecoverTestFile(t, fixture.hostPath, []byte("neither before nor expected after\n"))
	prepared, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Execute(ctx, prepared, ExecuteOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context.Canceled", err)
	}
	if _, err := Execute(context.Background(), prepared, ExecuteOptions{}); !errors.Is(err, ErrPreparedRecoveryConsumed) {
		t.Fatalf("second Execute error = %v, want ErrPreparedRecoveryConsumed", err)
	}
}

func TestPreparedRecoveryDisclosureMutationCannotAlterExecution(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	prepared, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	disclosed := prepared.Disclosure()
	active, ok := journal.ActiveRecoveryPlan(disclosed)
	if !ok {
		t.Fatal("prepared recovery did not disclose an active plan")
	}
	actions := active.Actions()
	if len(actions) == 0 {
		t.Fatal("disclosure has no recovery actions")
	}
	actions[0].Destination = "mutated"
	actions[0].ConsumerTargets[0] = target.TargetClaudeCode
	if actions[0].BeforePathMode != nil {
		*actions[0].BeforePathMode = recovery.PermissionMode(0o777)
	}
	if actions[0].ExpectedAfter.PathMode != nil {
		*actions[0].ExpectedAfter.PathMode = recovery.PermissionMode(0o777)
	}

	freshPlan, ok := journal.ActiveRecoveryPlan(prepared.Disclosure())
	if !ok {
		t.Fatal("prepared recovery did not retain an active disclosure")
	}
	fresh := freshPlan.Actions()
	if fresh[0].Destination == "mutated" || fresh[0].ConsumerTargets[0] != target.TargetCodex {
		t.Fatalf("mutated disclosure leaked into prepared recovery: %#v", fresh[0])
	}
	if _, err := Execute(context.Background(), prepared, ExecuteOptions{}); err != nil {
		t.Fatalf("Execute after disclosure mutation returned error: %v", err)
	}
	content, err := os.ReadFile(fixture.hostPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(fixture.oldContent) {
		t.Fatalf("restored content = %q, want %q", content, fixture.oldContent)
	}
}
