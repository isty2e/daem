package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/platformsupport"
)

func TestObserveDarwinProductVersionClassifiesCommandEvidence(t *testing.T) {
	tests := []struct {
		name        string
		result      commandResult
		wantVersion string
		wantReason  platformsupport.RuntimeObservationReason
	}{
		{name: "exact floor", result: commandResult{stdout: "26.0\n"}, wantVersion: "26.0"},
		{name: "newer patch", result: commandResult{stdout: "26.5.1\n"}, wantVersion: "26.5.1"},
		{name: "below floor", result: commandResult{stdout: "25.9.9\n"}, wantVersion: "25.9.9"},
		{name: "empty", result: commandResult{}, wantReason: platformsupport.RuntimeObservationInvalidOutput},
		{name: "blank", result: commandResult{stdout: " \n"}, wantReason: platformsupport.RuntimeObservationInvalidOutput},
		{name: "extra line", result: commandResult{stdout: "26.0\nextra\n"}, wantReason: platformsupport.RuntimeObservationInvalidOutput},
		{name: "carriage return", result: commandResult{stdout: "26.0\r\n"}, wantReason: platformsupport.RuntimeObservationInvalidOutput},
		{name: "malformed", result: commandResult{stdout: "26.x\n"}, wantReason: platformsupport.RuntimeObservationInvalidOutput},
		{name: "truncated", result: commandResult{stdout: "26.0", stdoutTruncated: true}, wantReason: platformsupport.RuntimeObservationInvalidOutput},
		{name: "command failure", result: commandResult{err: errors.New("failed")}, wantReason: platformsupport.RuntimeObservationCommandFailed},
		{name: "timeout", result: commandResult{timedOut: true, err: context.DeadlineExceeded}, wantReason: platformsupport.RuntimeObservationTimedOut},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation, err := observeDarwinProductVersion(
				context.Background(),
				func(context.Context) commandResult { return test.result },
			)
			if err != nil {
				t.Fatalf("observeDarwinProductVersion: %v", err)
			}
			version, observed := observation.Version()
			if test.wantVersion != "" {
				if !observed || version.String() != test.wantVersion {
					t.Fatalf("version = %s,%t, want %s", version, observed, test.wantVersion)
				}
			} else if observed {
				t.Fatalf("unexpected observed version %s", version)
			}
			if test.wantReason != platformsupport.RuntimeObservationNotObserved &&
				observation.Reason() != test.wantReason {
				t.Fatalf("reason = %s, want %s", observation.Reason(), test.wantReason)
			}
		})
	}
}

func TestObserveDarwinProductVersionHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	_, err := observeDarwinProductVersion(ctx, func(context.Context) commandResult {
		called = true
		return commandResult{stdout: "26.0\n"}
	})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("error=%v called=%t, want canceled without runner", err, called)
	}
}

func TestObserveDarwinProductVersionKeepsCompletedOutputAfterCallerCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	observation, err := observeDarwinProductVersion(ctx, func(context.Context) commandResult {
		cancel()
		return commandResult{stdout: "26.0\n"}
	})
	if err != nil {
		t.Fatalf("observeDarwinProductVersion: %v", err)
	}
	version, observed := observation.Version()
	if !observed || version.String() != "26.0" {
		t.Fatalf("version = %s,%t, want 26.0 after caller cancel during cleanup", version, observed)
	}
}

func TestObserveDarwinProductVersionKeepsCommandFailureAfterCallerCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	observation, err := observeDarwinProductVersion(ctx, func(context.Context) commandResult {
		cancel()
		return commandResult{err: errors.New("failed")}
	})
	if err != nil {
		t.Fatalf("observeDarwinProductVersion: %v", err)
	}
	if observation.Reason() != platformsupport.RuntimeObservationCommandFailed {
		t.Fatalf("reason = %s, want command_failed after caller cancel", observation.Reason())
	}
}

func TestObserveDarwinProductVersionUsesFrozenCanceledCommandResult(t *testing.T) {
	_, err := observeDarwinProductVersion(context.Background(), func(context.Context) commandResult {
		return commandResult{canceled: true, err: context.Canceled}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want frozen canceled command result", err)
	}
}

func TestObserveDarwinProductVersionKeepsTypedTimeoutAfterCallerCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	observation, err := observeDarwinProductVersion(ctx, func(context.Context) commandResult {
		cancel()
		return commandResult{timedOut: true, err: context.DeadlineExceeded}
	})
	if err != nil {
		t.Fatalf("observeDarwinProductVersion: %v", err)
	}
	if observation.Reason() != platformsupport.RuntimeObservationTimedOut {
		t.Fatalf("reason = %s, want typed timeout after later caller cancel", observation.Reason())
	}
}

func TestFreezeDarwinCommandResultClassifiesAttemptCause(t *testing.T) {
	failure := freezeDarwinCommandResult(context.Background(), errors.New("command failed"), "26.0\n", false)
	if failure.canceled || failure.timedOut || failure.err == nil || failure.err.Error() != "command failed" {
		t.Fatalf("frozen failure = %#v, want command error without later cancel", failure)
	}

	timedOut := freezeDarwinProductVersionTimeout(t, context.Background())
	if !timedOut.timedOut || timedOut.canceled {
		t.Fatalf("command timeout = %#v, want timedOut", timedOut)
	}

	canceled := freezeDarwinCommandResult(context.Background(), context.Canceled, "", false)
	if !canceled.canceled || canceled.timedOut {
		t.Fatalf("command cancel = %#v, want canceled", canceled)
	}

	timeoutParent, cancelTimeout := context.WithCancel(context.Background())
	frozenTimeout := freezeDarwinProductVersionTimeout(t, timeoutParent)
	cancelTimeout()
	if !frozenTimeout.timedOut || frozenTimeout.canceled {
		t.Fatalf("frozen timeout = %#v, want timedOut after later parent cancel", frozenTimeout)
	}

	parentDeadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
	defer cancelDeadline()
	attempt, cancelAttempt := context.WithTimeoutCause(parentDeadline, time.Hour, errDarwinProductVersionTimeout)
	defer cancelAttempt()
	parentTimedOut := freezeDarwinCommandResult(attempt, attempt.Err(), "", false)
	if parentTimedOut.timedOut {
		t.Fatalf("parent deadline = %#v, want wait deadline kept distinct from command timeout", parentTimedOut)
	}

	bareDeadline := freezeDarwinCommandResult(context.Background(), context.DeadlineExceeded, "", false)
	if bareDeadline.timedOut {
		t.Fatalf("bare deadline = %#v, want no Darwin timeout without attempt cause", bareDeadline)
	}
}

func TestFreezeDarwinCommandResultKeepsTimeoutCauseAfterLaterCallerCancel(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	attempt, cancelAttempt := context.WithTimeoutCause(parent, time.Nanosecond, errDarwinProductVersionTimeout)
	defer cancelAttempt()
	select {
	case <-attempt.Done():
	case <-time.After(time.Second):
		t.Fatal("attempt timeout did not fire")
	}
	cancelParent()
	result := freezeDarwinCommandResult(attempt, attempt.Err(), "", false)
	if !result.timedOut || result.canceled {
		t.Fatalf("result = %#v, want internal timeout preserved after later caller cancel", result)
	}
}

func freezeDarwinProductVersionTimeout(t *testing.T, parent context.Context) commandResult {
	t.Helper()
	attempt, cancel := context.WithTimeoutCause(parent, time.Nanosecond, errDarwinProductVersionTimeout)
	t.Cleanup(cancel)
	select {
	case <-attempt.Done():
	case <-time.After(time.Second):
		t.Fatal("attempt timeout did not fire")
	}
	return freezeDarwinCommandResult(attempt, attempt.Err(), "", false)
}

func TestCanonicalProductVersionOutputAcceptsOnlyOneOptionalNewline(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "26.0", want: "26.0", ok: true},
		{input: "26.0\n", want: "26.0", ok: true},
		{input: "26.0\n\n"},
		{input: "26.0 \n"},
		{input: ""},
	}
	for _, test := range tests {
		got, err := canonicalProductVersionOutput(test.input)
		if (err == nil) != test.ok || got != test.want {
			t.Fatalf("canonicalProductVersionOutput(%q) = %q,%v; want %q ok=%t", test.input, got, err, test.want, test.ok)
		}
	}
}
