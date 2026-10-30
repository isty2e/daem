//go:build darwin || linux

package subprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/executionidentity"
)

func protectedCommandTestInvocation(t *testing.T) executionidentity.Invocation {
	t.Helper()
	value, err := executionidentity.NewInvocation(strings.Repeat("c", 32))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func protectedCommandTestDirectory(root string) WorkingDirectoryBinder {
	return func() (WorkingDirectoryBinding, error) {
		directory, err := os.Open(root)
		if err != nil {
			return nil, err
		}
		return &testWorkingDirectoryBinding{directory: directory}, nil
	}
}

func protectedCommandTestFinalizer(context.Context, ProtectedCommandCompletion) error { return nil }

func TestProtectedCommandRetainsNativeDefaultsRedactionAndQuiescence(t *testing.T) {
	for _, executor := range []CommandExecutor{{}, NewCommandExecutor(CommandOptions{}).withDefaults()} {
		root := t.TempDir()
		confirmed := false
		executor.lookupEnv = func(name string) (string, bool) { return "private-protected-secret", name == "HOST_TOKEN" }
		result, finalizationErr := executor.ExecuteProtectedInWorkingDirectory(t.Context(), CommandAttemptRequest{
			Command: "/bin/sh", Args: []string{"-c", `printf '%s' "$TOKEN"; printf '%s' "$1"; cat`, "protected", "literal arg;not-shell"},
			EnvRefs: []CommandEnvRef{{Name: "TOKEN", SourceName: "HOST_TOKEN"}}, Stdin: "stdin-value",
		}, protectedCommandTestDirectory(root), protectedCommandTestInvocation(t), func(ctx context.Context, binding executionidentity.Binding) error {
			confirmed = ctx.Err() == nil && binding.Validate() == nil
			return nil
		}, protectedCommandTestFinalizer)
		if finalizationErr != nil {
			t.Fatal(finalizationErr)
		}
		attempt := result.Attempt()
		if !confirmed || !result.ReleaseAttempted() || !attempt.Succeeded() || !attempt.Redacted() ||
			strings.Contains(attempt.Stdout(), "private-protected-secret") || !strings.Contains(attempt.Stdout(), "literal arg;not-shellstdin-value") {
			t.Fatal("protected command lost native preflight/capture behavior", attempt.Reason(), attempt.ErrorDetail())
		}
		binding, present := result.Binding()
		proof, err := result.Quiescence()
		if !present || err != nil || proof.Require(binding) != nil {
			t.Fatal("native completion did not retain exact fresh quiescence", err)
		}
	}
}

func TestProtectedCommandDoesNotReleaseFailedBindingOrInvokeAnOrdinaryRunner(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target-ran")
	request := CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", `printf ran > "$1"`, "protected", target}}
	invocation := protectedCommandTestInvocation(t)
	result, finalizationErr := NewCommandExecutor(CommandOptions{}).ExecuteProtectedInWorkingDirectory(t.Context(), request,
		protectedCommandTestDirectory(root), invocation, func(context.Context, executionidentity.Binding) error {
			return errors.New("binding durability is indeterminate")
		}, protectedCommandTestFinalizer)
	if finalizationErr != nil {
		t.Fatal(finalizationErr)
	}
	if result.ReleaseAttempted() || result.Attempt().Succeeded() {
		t.Fatal("indeterminate binding was treated as released")
	}
	if _, present := result.Attempt().ExitCode(); present {
		t.Fatal("unreleased helper exit was reported as target completion")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed publication executed the target", err)
	}
	binding, present := result.Binding()
	proof, err := result.Quiescence()
	if !present || err != nil || proof.Require(binding) != nil {
		t.Fatal("unreleased helper was not independently observed quiescent", err)
	}
	called := false
	executor := NewCommandExecutor(CommandOptions{Runner: func(context.Context, CommandRequest) CommandResult {
		called = true
		return CommandResult{Started: true, HasExitCode: true}
	}})
	result, finalizationErr = executor.ExecuteProtectedInWorkingDirectory(t.Context(), request, protectedCommandTestDirectory(root), invocation,
		func(context.Context, executionidentity.Binding) error { called = true; return nil }, protectedCommandTestFinalizer)
	if finalizationErr == nil {
		t.Fatal("ordinary runner rejection lost its admission error")
	}
	if called || result.ReleaseAttempted() || result.Attempt().RunnerInvoked() || result.Attempt().Succeeded() {
		t.Fatal("ordinary runner bypassed or was silently ignored by protected execution")
	}
}

func TestProtectedCommandPreflightDoesNotPublishBinding(t *testing.T) {
	for _, mode := range []string{"missing env", "missing directory", "nil context"} {
		t.Run(mode, func(t *testing.T) {
			executor := NewCommandExecutor(CommandOptions{LookupEnv: func(string) (string, bool) { return "", false }})
			request := CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", "exit 0"}}
			ctx := t.Context()
			binder := protectedCommandTestDirectory(t.TempDir())
			if mode == "missing env" {
				request.EnvRefs = []CommandEnvRef{{Name: "MISSING_ENV"}}
			}
			if mode == "missing directory" {
				binder = nil
			}
			if mode == "nil context" {
				ctx = nil
			}
			called := false
			result, finalizationErr := executor.ExecuteProtectedInWorkingDirectory(ctx, request, binder, protectedCommandTestInvocation(t),
				func(context.Context, executionidentity.Binding) error { called = true; return nil }, protectedCommandTestFinalizer)
			if (finalizationErr != nil) != (mode == "nil context") {
				t.Fatalf("unexpected finalization/admission outcome: %v", finalizationErr)
			}
			if called || result.Attempt().RunnerInvoked() || result.ReleaseAttempted() || result.Attempt().Succeeded() {
				t.Fatal("failed protected preflight reached binding publication")
			}
			if _, err := result.Quiescence(); err == nil {
				t.Fatal("missing execution acquired quiescence evidence")
			}
		})
	}
	if _, err := (ProtectedCommandAttemptResult{}).Quiescence(); err == nil {
		t.Fatal("zero protected result acquired quiescence")
	}
}

func TestProtectedCommandFailureDoesNotDiscardQuiescence(t *testing.T) {
	result, finalizationErr := NewCommandExecutor(CommandOptions{}).ExecuteProtectedInWorkingDirectory(t.Context(),
		CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", "exit 17"}},
		protectedCommandTestDirectory(t.TempDir()), protectedCommandTestInvocation(t),
		func(context.Context, executionidentity.Binding) error { return nil }, protectedCommandTestFinalizer)
	if finalizationErr != nil {
		t.Fatal(finalizationErr)
	}
	code, present := result.Attempt().ExitCode()
	if !result.ReleaseAttempted() || result.Attempt().Reason() != CommandReasonNonZeroExit || !present || code != 17 {
		t.Fatal("protected target failure was reinterpreted as setup or success")
	}
	binding, present := result.Binding()
	proof, err := result.Quiescence()
	if !present || err != nil || proof.Require(binding) != nil {
		t.Fatal("mechanical failure hid independent safety evidence", err)
	}
}

func TestProtectedCommandCancellationRetainsFreshSafetyEvidence(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(root, "ready")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	invocation := protectedCommandTestInvocation(t)
	type completedAttempt struct {
		result ProtectedCommandAttemptResult
		err    error
	}
	finalizerCalls := 0
	completed := make(chan completedAttempt, 1)
	go func() {
		result, err := NewCommandExecutor(CommandOptions{}).ExecuteProtectedInWorkingDirectory(ctx,
			CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", `printf ready > "$1"; /bin/sleep 30`, "protected", ready}},
			protectedCommandTestDirectory(root), invocation, func(context.Context, executionidentity.Binding) error { return nil }, func(cleanup context.Context, completion ProtectedCommandCompletion) error {
				finalizerCalls++
				if cleanup.Err() != nil || cleanup.Done() != nil {
					return errors.New("cleanup inherited normal cancellation")
				}
				return nil
			})
		completed <- completedAttempt{result, err}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-completed:
		case <-time.After(DefaultCommandTimeout + 5*time.Second):
			t.Error("protected cancellation fixture did not finish")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("protected target did not reach cancellation fixture")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	var outcome completedAttempt
	select {
	case outcome = <-completed:
		completed <- outcome
	case <-time.After(DefaultCommandTimeout + 5*time.Second):
		t.Fatal("protected target cancellation did not return")
	}
	if outcome.err != nil || finalizerCalls != 1 {
		t.Fatal(outcome.err)
	}
	result := outcome.result
	if !result.ReleaseAttempted() || !result.Attempt().Canceled() || result.Attempt().Succeeded() {
		t.Fatal("protected cancellation lost its mechanical outcome")
	}
	binding, present := result.Binding()
	proof, err := result.Quiescence()
	if !present || err != nil || proof.Require(binding) != nil {
		t.Fatal("normal cancellation suppressed fresh safety observation", err)
	}
}

type captureOrderingError struct {
	finalized *bool
	premature *bool
	calls     *int
}

func (failure captureOrderingError) Error() string {
	*failure.calls++
	if !*failure.finalized {
		*failure.premature = true
	}
	return "captured runner failure"
}

func TestCommandFinalizationPrecedesCaptureAndDirectoryRevalidation(t *testing.T) {
	for _, withDirectory := range []bool{false, true} {
		name := "without-directory"
		if withDirectory {
			name = "with-directory"
		}
		t.Run(name, func(t *testing.T) {
			finalized, premature, captureCalls, finalizerCalls := false, false, 0, 0
			executor := NewCommandExecutor(CommandOptions{Runner: func(context.Context, CommandRequest) CommandResult {
				return CommandResult{Started: true, Err: captureOrderingError{&finalized, &premature, &captureCalls}}
			}})
			var bind WorkingDirectoryBinder
			if withDirectory {
				directory, err := os.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				validations := 0
				bind = func() (WorkingDirectoryBinding, error) {
					return &testWorkingDirectoryBinding{directory: directory, validate: func() error {
						validations++
						if validations > 1 && !finalized {
							premature = true
						}
						return nil
					}}, nil
				}
			}
			attempt := executor.execute(t.Context(), CommandAttemptRequest{Command: "fixture"}, bind, func() { finalizerCalls++; finalized = true })
			if premature || finalizerCalls != 1 || captureCalls == 0 || !attempt.Failed() {
				t.Fatalf("misordered finalization: directory=%t early=%t finalizers=%d capture=%d", withDirectory, premature, finalizerCalls, captureCalls)
			}
		})
	}
}

func TestProtectedEarlyFailureFinalizesBeforeDiagnosticFormatting(t *testing.T) {
	finalized, premature, captureCalls, finalizerCalls := false, false, 0, 0
	binder := func() (WorkingDirectoryBinding, error) {
		return nil, captureOrderingError{&finalized, &premature, &captureCalls}
	}
	result, err := NewCommandExecutor(CommandOptions{}).ExecuteProtectedInWorkingDirectory(t.Context(), CommandAttemptRequest{Command: "unused"}, binder, protectedCommandTestInvocation(t), func(context.Context, executionidentity.Binding) error { t.Fatal("binding should not run"); return nil }, func(context.Context, ProtectedCommandCompletion) error {
		finalizerCalls++
		finalized = true
		return nil
	})
	if err != nil || premature || captureCalls == 0 || finalizerCalls != 1 || !result.Attempt().WorkDirAuthorityFailed() {
		t.Fatalf("early diagnostic preceded safety: err=%v early=%t capture=%d finalize=%d", err, premature, captureCalls, finalizerCalls)
	}
}

func TestProtectedFinalizerRunsOnceOnEveryAdmittedEarlyFailure(t *testing.T) {
	for _, failure := range []string{"env", "nil binder", "nil binding", "bind", "validate", "open", "lookup", "already canceled"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithCancel(context.WithValue(t.Context(), protectedFinalizerContextKey{}, "retained"))
			defer cancel()
			request := CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", "exit 0"}}
			binder := protectedCommandTestDirectory(root)
			executor := NewCommandExecutor(CommandOptions{LookupEnv: func(string) (string, bool) { return "", false }})
			switch failure {
			case "env":
				request.EnvRefs = []CommandEnvRef{{Name: "ABSENT"}}
			case "nil binder":
				binder = nil
			case "nil binding":
				binder = func() (WorkingDirectoryBinding, error) { return nil, nil }
			case "bind":
				binder = func() (WorkingDirectoryBinding, error) { return nil, errors.New("bind refused") }
			case "validate", "open":
				directory, err := os.Open(root)
				if err != nil {
					t.Fatal(err)
				}
				binding := &testWorkingDirectoryBinding{directory: directory}
				if failure == "validate" {
					binding.validate = func() error { return errors.New("validation refused") }
				} else {
					binding.nilDirectory = true
				}
				binder = func() (WorkingDirectoryBinding, error) { return binding, nil }
			case "lookup":
				request.Command = filepath.Join(root, "missing-command")
			case "already canceled":
				cancel()
			}
			calls := 0
			result, err := executor.ExecuteProtectedInWorkingDirectory(ctx, request, binder, protectedCommandTestInvocation(t), func(context.Context, executionidentity.Binding) error {
				t.Fatal("early failure published binding")
				return nil
			}, func(cleanup context.Context, completion ProtectedCommandCompletion) error {
				calls++
				if cleanup.Err() != nil || cleanup.Done() != nil || cleanup.Value(protectedFinalizerContextKey{}) != "retained" {
					t.Fatal("normal cancellation leaked into finalization")
				}
				if completion.ReleaseAttempted() {
					t.Fatal("early failure fabricated release")
				}
				return nil
			})
			if err != nil || calls != 1 || result.Attempt().Succeeded() {
				t.Fatalf("early finalization lost: calls=%d err=%v reason=%s", calls, err, result.Attempt().Reason())
			}
		})
	}
}

type protectedFinalizerContextKey struct{}

func TestProtectedFinalizationFailureIsSeparateFromMechanicalSuccess(t *testing.T) {
	want := errors.New("permission settlement remains pending")
	calls := 0
	result, err := NewCommandExecutor(CommandOptions{}).ExecuteProtectedInWorkingDirectory(t.Context(), CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", `printf '[malformed'`}}, protectedCommandTestDirectory(t.TempDir()), protectedCommandTestInvocation(t), func(context.Context, executionidentity.Binding) error { return nil }, func(ctx context.Context, completion ProtectedCommandCompletion) error {
		calls++
		binding, present := completion.Binding()
		proof, err := completion.Quiescence()
		if !present || err != nil || proof.Require(binding) != nil || !completion.ReleaseAttempted() {
			t.Fatal("finalizer lacked actual completion evidence", err)
		}
		return want
	})
	if !errors.Is(err, want) || calls != 1 || !result.Attempt().Succeeded() || result.Attempt().Stdout() != "[malformed" {
		t.Fatalf("safety failure rewrote mechanical facts: calls=%d err=%v reason=%s", calls, err, result.Attempt().Reason())
	}
}

func TestProtectedCommandRequiresSafetyFinalizerBeforeLaunch(t *testing.T) {
	called := false
	result, err := NewCommandExecutor(CommandOptions{}).ExecuteProtectedInWorkingDirectory(t.Context(), CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", "exit 0"}}, protectedCommandTestDirectory(t.TempDir()), protectedCommandTestInvocation(t), func(context.Context, executionidentity.Binding) error { called = true; return nil }, nil)
	if err == nil || called || result.Attempt().RunnerInvoked() || result.ReleaseAttempted() {
		t.Fatal("missing finalizer bypassed protected admission")
	}
}

func TestProtectedFinalizerRetainsNativeFailureAndQuiescenceAxes(t *testing.T) {
	for _, mode := range []string{"success", "nonzero", "timeout", "target exec", "bind failure"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			request := CommandAttemptRequest{Command: "/bin/sh", Args: []string{"-c", "exit 0"}}
			executor := NewCommandExecutor(CommandOptions{})
			if mode == "nonzero" {
				request.Args = []string{"-c", "exit 19"}
			}
			if mode == "timeout" {
				executor = NewCommandExecutor(CommandOptions{Timeout: 2 * time.Second})
				request.Args = []string{"-c", "/bin/sleep 30"}
			}
			if mode == "target exec" {
				request.Command = filepath.Join(root, "invalid-interpreter")
				request.Args = nil
				if err := os.WriteFile(request.Command, []byte("#!/missing/protected-test-interpreter\n"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(request.Command, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			result, err := executor.ExecuteProtectedInWorkingDirectory(t.Context(), request, protectedCommandTestDirectory(root), protectedCommandTestInvocation(t), func(context.Context, executionidentity.Binding) error {
				if mode == "bind failure" {
					return errors.New("binding not confirmed")
				}
				return nil
			}, func(ctx context.Context, completion ProtectedCommandCompletion) error {
				calls++
				binding, present := completion.Binding()
				proof, err := completion.Quiescence()
				if !present {
					if mode != "timeout" || completion.ReleaseAttempted() || err == nil {
						t.Fatal("unqualified completion fabricated execution evidence")
					}
					return nil
				}
				if err != nil || proof.Require(binding) != nil {
					t.Fatal("completion discarded independent Q", err)
				}
				if mode != "timeout" && completion.ReleaseAttempted() != (mode != "bind failure") {
					t.Fatal("release attempt and binding were conflated")
				}
				if ctx.Err() != nil {
					t.Fatal("normal timeout prevented finalization")
				}
				return nil
			})
			if err != nil || calls != 1 || result.Attempt().Succeeded() != (mode == "success") {
				t.Fatalf("mechanical/finalization conflation: mode=%s calls=%d err=%v reason=%s", mode, calls, err, result.Attempt().Reason())
			}
			if mode == "timeout" && !result.Attempt().TimedOut() {
				t.Fatal("finalization overwrote timeout")
			}
		})
	}
}
