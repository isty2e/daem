package invocation

import (
	"context"
	"errors"
	"testing"
	"time"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

func TestPostObservationRetainsAttemptAndItsCallerSelectedCancellationPolicy(t *testing.T) {
	completion, err := CompletionWindow(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		completion CompletionContext
		wantError  error
	}{
		{"caller", CallerContext(), context.Canceled},
		{"completion-window", completion, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			documents := nativeconfig.InvocationDocuments{}
			reads, err := documents.ReserveReads(physicalwork.NewCaptureBudget(nil), physicalwork.InvocationWork{})
			if err != nil {
				t.Fatal(err)
			}
			invocation := New(documents, reads, relationhost.NativeSettingsRewrites{}, nil, test.completion)
			bindCalls := 0
			bind := func() (subprocess.WorkingDirectoryBinding, error) {
				bindCalls++
				cancel()
				return nil, errors.New("working-directory authority unavailable")
			}
			runnerCalls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				runnerCalls++
				return subprocess.CommandResult{Started: true, HasExitCode: true}
			}})
			attempt, observed, err := invocation.Execute(ctx, executor, subprocess.CommandAttemptRequest{Command: "not-started"}, bind, nil)
			if !observed || !attempt.WorkDirAuthorityFailed() || !errors.Is(err, test.wantError) || bindCalls != 1 || runnerCalls != 0 {
				t.Fatal("post-observation changed attempt visibility or cancellation policy", observed, attempt.WorkDirAuthorityFailed(), err, bindCalls)
			}
			if _, observed, err := invocation.Execute(t.Context(), executor, subprocess.CommandAttemptRequest{}, bind, nil); observed || !errors.Is(err, ErrUnavailable) || bindCalls != 1 || runnerCalls != 0 {
				t.Fatal("used invocation regained execution or working-directory authority", observed, err, bindCalls)
			}
		})
	}
}

func TestCompletionObservationRequiresAPositiveBound(t *testing.T) {
	for _, window := range []time.Duration{0, -time.Second} {
		if _, err := CompletionWindow(window); err == nil {
			t.Fatal("invalid completion window admitted", window)
		}
	}
}
