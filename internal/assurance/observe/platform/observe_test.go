package platform

import (
	"context"
	"errors"
	"testing"

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
