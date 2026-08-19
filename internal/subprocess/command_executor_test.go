package subprocess

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func (executor CommandExecutor) executeWithoutWorkingDirectory(
	ctx context.Context,
	request CommandAttemptRequest,
) CommandAttemptResult {
	return executor.Execute(ctx, request)
}

func sameNameEnvRef(name string) CommandEnvRef {
	return CommandEnvRef{Name: name, SourceName: name}
}

func TestExecuteClassifiesSuccessExit(t *testing.T) {
	attemptedAt := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	executor := NewCommandExecutor(CommandOptions{
		Clock: func() time.Time { return attemptedAt },
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			return CommandResult{Started: true, HasExitCode: true, Stdout: "ok"}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{Command: "success-test"})

	if !result.Succeeded() ||
		result.Reason() != CommandReasonNone ||
		!result.RunnerInvoked() ||
		!result.Started() {
		t.Fatalf("result = %#v, want started success", result)
	}
	if result.Stdout() != "ok" {
		t.Fatalf("stdout = %q, want ok", result.Stdout())
	}
	if !result.AttemptedAt().Equal(attemptedAt) {
		t.Fatalf("attempted_at = %s, want %s", result.AttemptedAt(), attemptedAt)
	}
}

func TestExecuteClassifiesNonZeroExit(t *testing.T) {
	executor := NewCommandExecutor(CommandOptions{
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			return CommandResult{
				Started:     true,
				ExitCode:    17,
				HasExitCode: true,
				Stderr:      "failed",
				Err:         errors.New("exit status 17"),
			}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{Command: "nonzero-test"})

	exitCode, ok := result.ExitCode()
	if result.Reason() != CommandReasonNonZeroExit || !ok || exitCode != 17 {
		t.Fatalf("result = %#v exit=(%d,%t), want nonzero 17", result, exitCode, ok)
	}
	if result.Stderr() != "failed" {
		t.Fatalf("stderr = %q, want failed", result.Stderr())
	}
}

func TestExecuteClassifiesTimeout(t *testing.T) {
	executor := NewCommandExecutor(CommandOptions{
		Timeout: time.Millisecond,
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			<-ctx.Done()
			return CommandResult{Started: true, TimedOut: true, Err: ctx.Err()}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{Command: "timeout-test"})

	if result.Reason() != CommandReasonTimeout || !result.TimedOut() || !result.Started() {
		t.Fatalf("result = %#v, want started timeout", result)
	}
}

func TestExecuteDoesNotReclassifyExitAfterAttemptContextExpires(t *testing.T) {
	executor := NewCommandExecutor(CommandOptions{
		Timeout: time.Millisecond,
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			<-ctx.Done()
			return CommandResult{Started: true, HasExitCode: true, ExitCode: 17, Err: errors.New("exit status 17")}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{Command: "cleanup-after-exit"})

	exitCode, ok := result.ExitCode()
	if result.Reason() != CommandReasonNonZeroExit || result.TimedOut() || !ok || exitCode != 17 {
		t.Fatalf("result = %#v exit=(%d,%t), want nonzero 17 without timeout overwrite", result, exitCode, ok)
	}
}

func TestExecuteDoesNotReclassifySuccessAfterAttemptContextExpires(t *testing.T) {
	executor := NewCommandExecutor(CommandOptions{
		Timeout: time.Millisecond,
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			<-ctx.Done()
			return CommandResult{Started: true, HasExitCode: true}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{Command: "cleanup-after-success"})

	if result.Reason() != CommandReasonNone || result.TimedOut() || !result.Succeeded() {
		t.Fatalf("result = %#v, want success preserved through post-exit context expiry", result)
	}
}

func TestExecuteClassifiesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	executor := NewCommandExecutor(CommandOptions{
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			<-ctx.Done()
			return CommandResult{Started: true, Canceled: true, Err: ctx.Err()}
		},
	})

	result := executor.executeWithoutWorkingDirectory(ctx, CommandAttemptRequest{Command: "cancel-test"})

	if result.Reason() != CommandReasonCanceled || !result.Canceled() || !result.Started() {
		t.Fatalf("result = %#v, want started cancellation", result)
	}
}

func TestExecuteClassifiesSignal(t *testing.T) {
	executor := NewCommandExecutor(CommandOptions{
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			return CommandResult{Started: true, Signaled: true, Err: errors.New("signal: killed")}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{Command: "signal-test"})

	if result.Reason() != CommandReasonSignaled || !result.Signaled() || !result.Started() {
		t.Fatalf("result = %#v, want started signal classification", result)
	}
}

func TestExecuteBoundsStderrOnlyOutput(t *testing.T) {
	executor := NewCommandExecutor(CommandOptions{
		OutputLimit: 4,
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			return CommandResult{Started: true, HasExitCode: true, Stderr: "abcdef"}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{Command: "stderr-test"})

	if !result.Succeeded() {
		t.Fatalf("result = %#v, want success", result)
	}
	if result.Stdout() != "" {
		t.Fatalf("stdout = %q, want empty", result.Stdout())
	}
	if !result.StderrTruncated() || result.Stderr() != "abcd\n[truncated]" {
		t.Fatalf("stderr = %q truncated=%t, want bounded stderr", result.Stderr(), result.StderrTruncated())
	}
}

func TestExecuteRedactsEnvSecretsAndSecretLookingFragments(t *testing.T) {
	const secret = "super-secret-value"
	t.Setenv("CHILD_TOKEN", "old-secret-value")
	executor := NewCommandExecutor(CommandOptions{
		LookupEnv: func(name string) (string, bool) {
			if name == "HOST_TOKEN" {
				return secret, true
			}
			return "", false
		},
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			if !envContains(request.Env, "CHILD_TOKEN="+secret) {
				t.Fatalf("request env missing child token")
			}
			if envContains(request.Env, "CHILD_TOKEN=old-secret-value") {
				t.Fatalf("request env retained old child token")
			}
			return CommandResult{
				Started: true,
				Stdout:  "token=" + secret + " api_key=abc123",
				Stderr:  `password: hunter2 secret="quoted secret"`,
				Err:     errors.New("runner saw " + secret),
			}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: "redact-test",
		EnvRefs: []CommandEnvRef{{
			Name:       "CHILD_TOKEN",
			SourceName: "HOST_TOKEN",
		}},
	})

	combined := result.Stdout() + result.Stderr() + result.ErrorDetail()
	for _, forbidden := range []string{secret, "abc123", "hunter2", "quoted secret"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("result leaked %q in %q", forbidden, combined)
		}
	}
	if !result.Redacted() {
		t.Fatal("Redacted = false, want true")
	}
}

func TestExecuteRedactsSensitiveInheritedEnvironmentValue(t *testing.T) {
	const ambientSecret = "ambient-command-secret-value"
	t.Setenv("DAEM_TEST_AMBIENT_TOKEN", ambientSecret)
	executor := NewCommandExecutor(CommandOptions{
		Runner: func(_ context.Context, request CommandRequest) CommandResult {
			if !envContains(request.Env, "DAEM_TEST_AMBIENT_TOKEN="+ambientSecret) {
				t.Fatal("request env did not inherit ambient token")
			}
			return CommandResult{
				Started: true,
				Stdout:  "runner echoed " + ambientSecret,
				Err:     errors.New("runner failed with " + ambientSecret),
			}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{Command: "ambient-redaction-test"})
	combined := result.Stdout() + result.ErrorDetail()
	if strings.Contains(combined, ambientSecret) {
		t.Fatalf("result leaked inherited ambient secret in %q", combined)
	}
	if !result.Redacted() || !strings.Contains(combined, "[REDACTED]") {
		t.Fatalf("result = %#v, want inherited-secret redaction", result)
	}
}

func TestExecuteRedactsQuotedKeysAndTruncatedSecretPrefixes(t *testing.T) {
	const secret = "super-secret"
	executor := NewCommandExecutor(CommandOptions{
		OutputLimit: 256,
		LookupEnv: func(name string) (string, bool) {
			if name == "HOST_TOKEN" {
				return secret, true
			}
			return "", false
		},
		Runner: func(context.Context, CommandRequest) CommandResult {
			return CommandResult{
				Started:         true,
				Stdout:          `{"token":"unlisted-secret"}`,
				Stderr:          "runner saw super-",
				StderrTruncated: true,
				Err:             errors.New(`password="quoted value"`),
			}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: "redaction-boundary-test",
		EnvRefs: []CommandEnvRef{{
			Name:       "CHILD_TOKEN",
			SourceName: "HOST_TOKEN",
		}},
	})

	combined := result.Stdout() + result.Stderr() + result.ErrorDetail()
	for _, forbidden := range []string{"unlisted-secret", "super-", "quoted value"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("result leaked %q in %q", forbidden, combined)
		}
	}
	if result.Stdout() != `{"token":[REDACTED]}` || result.Stderr() != "runner saw [REDACTED]" {
		t.Fatalf("sanitized output = %q/%q", result.Stdout(), result.Stderr())
	}
	if !result.StderrTruncated() || !result.Redacted() {
		t.Fatalf("flags = truncated:%t redacted:%t, want true/true", result.StderrTruncated(), result.Redacted())
	}
}

func TestSanitizeCaptureRedactsSecretSplitAcrossRunnerWrites(t *testing.T) {
	buffer := NewBoundedBuffer(64)
	for _, chunk := range []string{"runner saw ", "super-", "secret"} {
		if _, err := buffer.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write(%q): %v", chunk, err)
		}
	}

	capture := sanitizeCapture(CommandResult{
		Stdout:          buffer.String(),
		StdoutTruncated: buffer.Truncated(),
	}, []string{"super-secret"}, 64)
	if capture.stdout != "runner saw [REDACTED]" || !capture.redacted || capture.stdoutTruncated {
		t.Fatalf("capture = %#v", capture)
	}
}

func TestExecuteReportsMissingEnvRefWithoutLaunchingRunner(t *testing.T) {
	called := false
	executor := NewCommandExecutor(CommandOptions{
		LookupEnv: func(name string) (string, bool) { return "", false },
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			called = true
			return CommandResult{}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: "missing-env-test",
		EnvRefs: []CommandEnvRef{
			sameNameEnvRef("ZZ_TOKEN"),
			sameNameEnvRef("AA_TOKEN"),
		},
	})

	if called {
		t.Fatal("runner was called for missing env refs")
	}
	if result.RunnerInvoked() {
		t.Fatal("missing env refs reported reaching the runner boundary")
	}
	if result.Reason() != CommandReasonMissingEnvRef {
		t.Fatalf("result = %#v, want missing env ref", result)
	}
	if !strings.Contains(result.ErrorDetail(), "AA_TOKEN, ZZ_TOKEN") {
		t.Fatalf("error detail = %q, want sorted missing env names", result.ErrorDetail())
	}
}

func TestExecuteInWorkingDirectoryDoesNotAcquireBindingForMissingEnv(t *testing.T) {
	acquired := false
	executor := NewCommandExecutor(CommandOptions{
		LookupEnv: func(name string) (string, bool) { return "", false },
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			t.Fatal("runner was called for missing env refs")
			return CommandResult{}
		},
	})

	result := executor.ExecuteInWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: "missing-env-bound-test",
		EnvRefs: []CommandEnvRef{sameNameEnvRef("MISSING_BOUND_TOKEN")},
	}, func() (WorkingDirectoryBinding, error) {
		acquired = true
		return nil, errors.New("must not acquire")
	})

	if acquired {
		t.Fatal("working-directory binding was acquired before env preflight")
	}
	if result.Reason() != CommandReasonMissingEnvRef {
		t.Fatalf("result reason = %q, want %q", result.Reason(), CommandReasonMissingEnvRef)
	}
}

func TestExecuteInWorkingDirectoryRejectsMissingBinderWithoutLaunching(t *testing.T) {
	tests := []struct {
		name    string
		options CommandOptions
	}{
		{name: "default runner"},
		{
			name: "custom runner",
			options: CommandOptions{Runner: func(context.Context, CommandRequest) CommandResult {
				t.Fatal("runner was called without working-directory authority")
				return CommandResult{}
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := NewCommandExecutor(test.options).ExecuteInWorkingDirectory(
				context.Background(),
				CommandAttemptRequest{Command: "must-not-launch-without-working-directory-authority"},
				nil,
			)

			if result.RunnerInvoked() ||
				result.Started() ||
				result.Reason() != CommandReasonNone ||
				!result.WorkDirAuthorityFailed() {
				t.Fatalf("result = %#v, want unstarted working-directory authority failure", result)
			}
			if !strings.Contains(result.ErrorDetail(), "binder is required") {
				t.Fatalf("error detail = %q, want missing binder detail", result.ErrorDetail())
			}
		})
	}
}

func TestExecuteInWorkingDirectoryReportsMissingEnvBeforeMissingBinder(t *testing.T) {
	executor := NewCommandExecutor(CommandOptions{
		LookupEnv: func(string) (string, bool) { return "", false },
		Runner: func(context.Context, CommandRequest) CommandResult {
			t.Fatal("runner was called for missing env refs")
			return CommandResult{}
		},
	})

	result := executor.ExecuteInWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: "missing-env-without-binder",
		EnvRefs: []CommandEnvRef{sameNameEnvRef("MISSING_BOUND_TOKEN")},
	}, nil)

	if result.Reason() != CommandReasonMissingEnvRef {
		t.Fatalf("result = %#v, want missing env ref before binder validation", result)
	}
}

func TestExecuteCanonicalizesAndDeduplicatesEnvRefs(t *testing.T) {
	lookups := make(map[string]int)
	executor := NewCommandExecutor(CommandOptions{
		LookupEnv: func(name string) (string, bool) {
			lookups[name]++
			switch name {
			case "HOST_ONLY":
				return "host-only-value", true
			case "TOKEN":
				return "token-value", true
			default:
				return "", false
			}
		},
		Runner: func(ctx context.Context, request CommandRequest) CommandResult {
			if !envContains(request.Env, "HOST_ONLY=host-only-value") {
				t.Fatalf("request env missing default child name for HOST_ONLY")
			}
			if !envContains(request.Env, "TOKEN=token-value") {
				t.Fatalf("request env missing TOKEN")
			}
			if envCount(request.Env, "TOKEN=token-value") != 1 {
				t.Fatalf("request env duplicated TOKEN: %#v", request.Env)
			}
			return CommandResult{Started: true, HasExitCode: true}
		},
	})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: "env-dedupe-test",
		EnvRefs: []CommandEnvRef{
			{SourceName: "HOST_ONLY"},
			sameNameEnvRef("TOKEN"),
			sameNameEnvRef("TOKEN"),
		},
	})

	if !result.Succeeded() {
		t.Fatalf("result = %#v, want success", result)
	}
	if lookups["TOKEN"] != 1 || lookups["HOST_ONLY"] != 1 {
		t.Fatalf("lookups = %#v, want deduped lookups", lookups)
	}
}

func TestDefaultRunnerReportsMissingExecutable(t *testing.T) {
	executor := NewCommandExecutor(CommandOptions{})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: "definitely-missing-daem-command-test-runner",
	})

	if result.Reason() != CommandReasonMissingRunner || result.Started() {
		t.Fatalf("result = %#v, want missing runner before start", result)
	}
}

func TestDefaultRunnerInvalidWorkDirFailsBeforeStart(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executor := NewCommandExecutor(CommandOptions{Timeout: DefaultCommandTimeout})

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args:    []string{"-test.run=TestCommandExecHelperProcess", "--", "cwd"},
		WorkDir: filepath.Join(t.TempDir(), "missing"),
	})

	if result.Reason() != CommandReasonRunnerError || result.Started() {
		t.Fatalf("result = %#v, want runner error before start", result)
	}
}

func TestDefaultRunnerHonorsCwdAndStdin(t *testing.T) {
	t.Setenv("GO_WANT_COMMAND_EXEC_HELPER_PROCESS", "1")
	tempDir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executor := NewCommandExecutor(CommandOptions{Timeout: DefaultCommandTimeout, OutputLimit: 1024})

	cwdResult := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args:    []string{"-test.run=TestCommandExecHelperProcess", "--", "cwd"},
		WorkDir: tempDir,
	})
	if !cwdResult.Succeeded() {
		t.Fatalf("cwd result = %#v", cwdResult)
	}
	gotCWD, err := filepath.EvalSymlinks(strings.TrimSpace(cwdResult.Stdout()))
	if err != nil {
		t.Fatalf("resolve cwd stdout %q: %v", cwdResult.Stdout(), err)
	}
	wantCWD, err := filepath.EvalSymlinks(tempDir)
	if err != nil {
		t.Fatalf("resolve temp dir %q: %v", tempDir, err)
	}
	if gotCWD != wantCWD {
		t.Fatalf("cwd stdout = %q, want %q", gotCWD, wantCWD)
	}

	stdinResult := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args:    []string{"-test.run=TestCommandExecHelperProcess", "--", "stdin"},
		Stdin:   "hello from stdin",
	})
	if !stdinResult.Succeeded() {
		t.Fatalf("stdin result = %#v", stdinResult)
	}
	if stdinResult.Stdout() != "hello from stdin" {
		t.Fatalf("stdin stdout = %q, want input echoed", stdinResult.Stdout())
	}
}

func TestDefaultRunnerPassesArgvWithoutShell(t *testing.T) {
	t.Setenv("GO_WANT_COMMAND_EXEC_HELPER_PROCESS", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executor := NewCommandExecutor(CommandOptions{Timeout: DefaultCommandTimeout, OutputLimit: 1024})
	want := []string{"space arg", "semi;colon", "$(echo nope)", "", "star*"}

	result := executor.executeWithoutWorkingDirectory(context.Background(), CommandAttemptRequest{
		Command: executable,
		Args: append(
			[]string{"-test.run=TestCommandExecHelperProcess", "--", "argv"},
			want...,
		),
	})

	if !result.Succeeded() {
		t.Fatalf("result = %#v", result)
	}
	var got []string
	if err := json.Unmarshal([]byte(result.Stdout()), &got); err != nil {
		t.Fatalf("decode stdout %q: %v", result.Stdout(), err)
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestCommandExecHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_COMMAND_EXEC_HELPER_PROCESS") != "1" {
		return
	}
	args := argsAfterDoubleDash(os.Args)
	if len(args) == 0 {
		os.Exit(2)
	}
	switch args[0] {
	case "argv":
		if err := json.NewEncoder(os.Stdout).Encode(args[1:]); err != nil {
			os.Exit(3)
		}
	case "cwd":
		cwd, err := os.Getwd()
		if err != nil {
			os.Exit(4)
		}
		_, _ = os.Stdout.WriteString(cwd)
	case "stdin":
		payload, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(5)
		}
		_, _ = os.Stdout.Write(payload)
	case "read":
		if len(args) != 2 {
			os.Exit(7)
		}
		payload, err := os.ReadFile(args[1])
		if err != nil {
			os.Exit(8)
		}
		_, _ = os.Stdout.Write(payload)
	case "env-present":
		if len(args) != 2 {
			os.Exit(9)
		}
		if _, ok := os.LookupEnv(args[1]); ok {
			_, _ = os.Stdout.WriteString("present")
		} else {
			_, _ = os.Stdout.WriteString("absent")
		}
	default:
		os.Exit(6)
	}
	os.Exit(0)
}

func argsAfterDoubleDash(args []string) []string {
	for index, arg := range args {
		if arg == "--" {
			return args[index+1:]
		}
	}
	return nil
}

func envContains(env []string, value string) bool {
	return slices.Contains(env, value)
}

func envCount(env []string, value string) int {
	count := 0
	for _, item := range env {
		if item == value {
			count++
		}
	}
	return count
}
