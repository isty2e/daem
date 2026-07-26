package refresh

import (
	"context"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/subprocess"
)

func TestRefreshCancelCannotMisreportConcurrentExecution(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	started := make(chan struct{})
	release := make(chan struct{})
	executionDone := make(chan error, 1)
	go func() {
		_, executeErr := Execute(context.Background(), prepared, ExecuteOptions{
			CommandOptions: subprocess.CommandOptions{
				Runner: func(
					context.Context,
					subprocess.CommandRequest,
				) subprocess.CommandResult {
					close(started)
					<-release
					return successfulRefreshCommandResult()
				},
			},
		})
		executionDone <- executeErr
	}()
	<-started

	cancelled, cancelErr := Cancel(prepared)
	if !errors.Is(cancelErr, ErrPreparedCommandConsumed) {
		close(release)
		t.Fatalf(
			"Cancel result=%#v error=%v, want ErrPreparedCommandConsumed",
			cancelled,
			cancelErr,
		)
	}
	if cancelled.ResultClass == ResultCancelled {
		close(release)
		t.Fatalf("Cancel falsely reported pre-attempt cancellation: %#v", cancelled)
	}
	close(release)
	if executeErr := <-executionDone; executeErr != nil {
		t.Fatalf("Execute returned error: %v", executeErr)
	}
}

func TestRefreshCancelRejectsClosedAndUnavailablePlans(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if result, err := Cancel(prepared); !errors.Is(
		err,
		ErrPreparedCommandClosed,
	) || result.ResultClass == ResultCancelled {
		t.Fatalf("Cancel closed result=%#v error=%v", result, err)
	}
	unavailable := unavailablePreparedCommand(CommandResult{
		ResultClass: ResultRefused,
	})
	if result, err := Cancel(unavailable); !errors.Is(
		err,
		ErrPreparedCommandUnavailable,
	) || result.ResultClass == ResultCancelled {
		t.Fatalf("Cancel unavailable result=%#v error=%v", result, err)
	}
}
