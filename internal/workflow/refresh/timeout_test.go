package refresh

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
)

func TestHostCommandTimeoutBoundsExplicitDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		requested time.Duration
		want      time.Duration
		wantError string
	}{
		{name: "minimum", requested: MinimumHostCommandTimeout, want: MinimumHostCommandTimeout},
		{name: "maximum", requested: MaximumHostCommandTimeout, want: MaximumHostCommandTimeout},
		{name: "zero", wantError: "between"},
		{name: "below minimum", requested: MinimumHostCommandTimeout - time.Nanosecond, wantError: "between"},
		{name: "above maximum", requested: MaximumHostCommandTimeout + time.Second, wantError: "between"},
		{name: "fractional second", requested: time.Second + time.Millisecond, wantError: "whole seconds"},
		{name: "negative", requested: -time.Second, wantError: "between"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			timeout, err := NewHostCommandTimeout(test.requested)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want substring %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewHostCommandTimeout returned error: %v", err)
			}
			if timeout.Duration() != test.want ||
				timeout.Seconds() != int(test.want/time.Second) {
				t.Fatalf(
					"timeout = %s/%ds, want %s/%ds",
					timeout.Duration(),
					timeout.Seconds(),
					test.want,
					int(test.want/time.Second),
				)
			}
		})
	}
}

func TestNormalizeHostCommandTimeoutDefaultsOnlyOmittedWorkflowInput(t *testing.T) {
	t.Parallel()

	timeout, err := normalizeHostCommandTimeout(0)
	if err != nil {
		t.Fatalf("normalizeHostCommandTimeout returned error: %v", err)
	}
	if timeout.Duration() != DefaultHostCommandTimeout {
		t.Fatalf(
			"timeout = %s, want %s",
			timeout.Duration(),
			DefaultHostCommandTimeout,
		)
	}
}

func TestRefreshRejectsInvalidTimeoutAtWorkflowIngress(t *testing.T) {
	builderCalls := 0
	result, err := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: "manifest-must-not-be-read",
		ExtensionID:  "formatter",
		Timeout:      1500 * time.Millisecond,
	}, PlanOptions{
		CommandBuilder: func(
			CommandBuildInput,
		) (CommandSpec, error) {
			builderCalls++
			return CommandSpec{}, nil
		},
	})
	if err == nil ||
		result.ResultClass != ResultRefused ||
		result.ReasonCode != ReasonInvalidTimeout ||
		builderCalls != 0 {
		t.Fatalf("result=%#v builderCalls=%d err=%v", result, builderCalls, err)
	}
}

func TestRefreshTimeoutParticipatesInDisclosureAndFingerprint(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	first, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
		Timeout:      time.Minute,
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("first PlanWrite returned error: %v", err)
	}
	firstDisclosure := first.Disclosure()
	firstFingerprint := first.lifecycle.planned.fingerprint
	if err := first.Close(); err != nil {
		t.Fatalf("close first prepared command: %v", err)
	}

	second, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
		Timeout:      2 * time.Minute,
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("second PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	secondDisclosure := second.Disclosure()
	secondFingerprint := second.lifecycle.planned.fingerprint

	if firstDisclosure.Disclosure.Invocation.TimeoutSeconds != 60 ||
		secondDisclosure.Disclosure.Invocation.TimeoutSeconds != 120 {
		t.Fatalf(
			"timeout disclosures = %d and %d",
			firstDisclosure.Disclosure.Invocation.TimeoutSeconds,
			secondDisclosure.Disclosure.Invocation.TimeoutSeconds,
		)
	}
	if firstFingerprint.Equal(secondFingerprint) {
		t.Fatal("refresh fingerprints ignored the selected timeout")
	}
}

func TestRefreshPassesDisclosedBudgetToSuccessfulAttempt(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
		Timeout:      2 * time.Minute,
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	calls := 0
	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Timeout: time.Millisecond,
			Runner: func(
				ctx context.Context,
				_ subprocess.CommandRequest,
			) subprocess.CommandResult {
				calls++
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("refresh runner context has no deadline")
				}
				remaining := time.Until(deadline)
				if remaining < 119*time.Second || remaining > 2*time.Minute {
					t.Fatalf("runner deadline remaining = %s, want approximately 2m", remaining)
				}
				return subprocess.CommandResult{
					Started:     true,
					ExitCode:    0,
					HasExitCode: true,
				}
			},
		},
	})
	if err != nil ||
		calls != 1 ||
		result.ResultClass != ResultAttemptedUnverified ||
		!result.Attempted ||
		result.ProcessOutcome == nil ||
		result.ProcessOutcome.TimedOut ||
		!result.AttemptHistory.Persisted {
		t.Fatalf("result=%#v calls=%d err=%v", result, calls, err)
	}
}

func TestRefreshStartedTimeoutIsPartialAndPersisted(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
		Timeout:      time.Second,
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	calls := 0
	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(
				_ context.Context,
				_ subprocess.CommandRequest,
			) subprocess.CommandResult {
				calls++
				return subprocess.CommandResult{
					Started:  true,
					TimedOut: true,
					Err:      context.DeadlineExceeded,
				}
			},
		},
	})
	if err == nil ||
		calls != 1 ||
		result.ResultClass != ResultPartial ||
		result.ReasonCode != ReasonCommandFailed ||
		!result.Attempted ||
		result.ProcessOutcome == nil ||
		!result.ProcessOutcome.TimedOut ||
		result.ProcessOutcome.Reason != subprocess.CommandReasonTimeout ||
		!result.AttemptHistory.Persisted {
		t.Fatalf("result=%#v calls=%d err=%v", result, calls, err)
	}
}
