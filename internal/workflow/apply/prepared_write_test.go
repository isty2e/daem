package apply

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestExecuteRejectsNilAndUnavailablePreparedWrites(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		_, err := ExecuteWithOptions(context.Background(), nil, ExecuteOptions{})
		if !errors.Is(err, ErrPreparedWriteUnavailable) {
			t.Fatalf("Execute error = %v, want ErrPreparedWriteUnavailable", err)
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		_, err := ExecuteWithOptions(context.Background(), unavailablePreparedWrite(CommandResult{}), ExecuteOptions{})
		if !errors.Is(err, ErrPreparedWriteUnavailable) {
			t.Fatalf("Execute error = %v, want ErrPreparedWriteUnavailable", err)
		}
	})
}

func TestPreparedWriteConsumesExactlyOnce(t *testing.T) {
	prepared := preparedWriteForLifecycleTest(commandPlan{})

	_, firstErr := ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{})
	var stale mutation.StaleSnapshotError
	if !errors.As(firstErr, &stale) {
		t.Fatalf("first Execute error = %v, want StaleSnapshotError", firstErr)
	}

	_, secondErr := ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{})
	if !errors.Is(secondErr, ErrPreparedWriteConsumed) {
		t.Fatalf("second Execute error = %v, want ErrPreparedWriteConsumed", secondErr)
	}
}

func TestPreparedWriteSerializesConcurrentExecute(t *testing.T) {
	prepared := preparedWriteForLifecycleTest(commandPlan{})
	start := make(chan struct{})
	errorsByAttempt := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)

	for range 2 {
		go func() {
			ready.Done()
			<-start
			_, err := ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{})
			errorsByAttempt <- err
		}()
	}
	ready.Wait()
	close(start)

	var staleCount int
	var consumedCount int
	for range 2 {
		err := <-errorsByAttempt
		var stale mutation.StaleSnapshotError
		switch {
		case errors.As(err, &stale):
			staleCount++
		case errors.Is(err, ErrPreparedWriteConsumed):
			consumedCount++
		default:
			t.Fatalf("concurrent Execute error = %v, want stale or consumed", err)
		}
	}
	if staleCount != 1 || consumedCount != 1 {
		t.Fatalf("outcomes stale=%d consumed=%d, want 1 each", staleCount, consumedCount)
	}
}

func TestPreparedWriteSerializesCloseAgainstExecuteAndReleasesRoot(t *testing.T) {
	root, err := rootedpath.CaptureRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prepared := preparedWriteForLifecycleTest(commandPlan{projectRoot: root})
	start := make(chan struct{})
	results := make(chan error, 2)

	go func() {
		<-start
		_, executeErr := ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{})
		results <- executeErr
	}()
	go func() {
		<-start
		results <- prepared.Close()
	}()
	close(start)

	first := <-results
	second := <-results
	if first != nil && !errors.Is(first, ErrPreparedWriteClosed) {
		var stale mutation.StaleSnapshotError
		if !errors.As(first, &stale) {
			t.Fatalf("first concurrent outcome = %v, want nil, closed, or stale", first)
		}
	}
	if second != nil && !errors.Is(second, ErrPreparedWriteClosed) {
		var stale mutation.StaleSnapshotError
		if !errors.As(second, &stale) {
			t.Fatalf("second concurrent outcome = %v, want nil, closed, or stale", second)
		}
	}
	if _, err := root.Authority(); err == nil {
		t.Fatal("prepared root authority remained usable after concurrent Close/Execute")
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
}

func TestPreparedWriteCopiesShareOneLifecycle(t *testing.T) {
	root, err := rootedpath.CaptureRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prepared := preparedWriteForLifecycleTest(commandPlan{projectRoot: root})
	duplicate := *prepared

	if err := duplicate.Close(); err != nil {
		t.Fatalf("duplicate Close returned error: %v", err)
	}
	if _, err := ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{}); !errors.Is(err, ErrPreparedWriteClosed) {
		t.Fatalf("original Execute error = %v, want ErrPreparedWriteClosed", err)
	}
	if _, err := root.Authority(); err == nil {
		t.Fatal("copied prepared write retained independent root authority")
	}
}

func TestPreparedWriteDisclosureDoesNotAliasManagedDecisionOrDiagnostics(t *testing.T) {
	managedPlan := applyAuthorityManagedPathPlan(
		t,
		"review",
		"review",
		"desired",
		target.TargetCodex,
		target.ScopeProject,
		ownership.OwnerAuthority{},
		nil,
	)
	planned := commandPlan{result: CommandResult{
		Reconciliation: managedPlan,
		Diagnostics: []findings.Diagnostic{{
			RepairActions: []string{"repair"},
			ManualReasons: []string{"manual"},
		}},
	}}
	prepared := preparedWriteForLifecycleTest(planned)

	disclosedConsumers := prepared.Reconciliation.ManagedPaths()[0].ConsumerTargets()
	disclosedConsumers[0] = target.TargetClaudeCode
	prepared.Reconciliation.ManagedPaths()[0] = reconcile.ManagedPathDecision{}
	prepared.Diagnostics[0].RepairActions[0] = "mutated repair"
	prepared.Diagnostics[0].ManualReasons[0] = "mutated reason"

	canonical := prepared.lifecycle.planned.result.Reconciliation.ManagedPaths()[0]
	if canonical.Kind() != reconcile.ManagedPathCreate {
		t.Fatalf("private decision kind = %q, want create", canonical.Kind())
	}
	if consumers := canonical.ConsumerTargets(); len(consumers) != 1 || consumers[0] != target.TargetCodex {
		t.Fatalf("private consumers = %#v, want codex", consumers)
	}
	diagnostic := prepared.lifecycle.planned.result.Diagnostics[0]
	if diagnostic.RepairActions[0] != "repair" {
		t.Fatalf("private repair action = %q, want repair", diagnostic.RepairActions[0])
	}
	if diagnostic.ManualReasons[0] != "manual" {
		t.Fatalf("private manual reason = %q, want manual", diagnostic.ManualReasons[0])
	}
}

func preparedWriteForLifecycleTest(planned commandPlan) *PreparedWrite {
	return newPreparedWrite(
		planned,
		CommandInput{},
		reconcile.ContextApply,
		mutation.OperationFingerprint{},
		applyAuthorityEvidence{},
	)
}
