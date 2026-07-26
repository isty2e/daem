package mcp

import (
	"context"
	"errors"
	"testing"
)

func TestCommandResultWithContextOutcome(t *testing.T) {
	runnerErr := errors.New("runner stopped")
	tests := []struct {
		name          string
		result        commandResult
		contextErr    error
		wantTimedOut  bool
		wantCanceled  bool
		wantResultErr error
	}{
		{
			name:          "deadline after initialize preserves success",
			result:        commandResult{Started: true, InitializeSucceeded: true},
			contextErr:    context.DeadlineExceeded,
			wantResultErr: nil,
		},
		{
			name:          "cancellation after initialize preserves success",
			result:        commandResult{Started: true, InitializeSucceeded: true},
			contextErr:    context.Canceled,
			wantResultErr: nil,
		},
		{
			name:          "deadline before initialize classifies timeout",
			result:        commandResult{Started: true},
			contextErr:    context.DeadlineExceeded,
			wantTimedOut:  true,
			wantResultErr: context.DeadlineExceeded,
		},
		{
			name:          "cancellation preserves runner failure",
			result:        commandResult{Started: true, Err: runnerErr},
			contextErr:    context.Canceled,
			wantCanceled:  true,
			wantResultErr: runnerErr,
		},
		{
			name:          "timeout remains stronger than cancellation",
			result:        commandResult{Started: true, TimedOut: true, Err: runnerErr},
			contextErr:    context.Canceled,
			wantTimedOut:  true,
			wantResultErr: runnerErr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.result.withContextOutcome(test.contextErr)
			if got.TimedOut != test.wantTimedOut || got.Canceled != test.wantCanceled {
				t.Fatalf("result = %#v, want timed_out=%t canceled=%t", got, test.wantTimedOut, test.wantCanceled)
			}
			if !errors.Is(got.Err, test.wantResultErr) || (test.wantResultErr == nil && got.Err != nil) {
				t.Fatalf("result error = %v, want %v", got.Err, test.wantResultErr)
			}
		})
	}
}
