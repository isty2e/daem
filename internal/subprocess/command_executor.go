package subprocess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// NewCommandExecutor constructs an executor with safe defaults for omitted
// dependencies.
func NewCommandExecutor(options CommandOptions) CommandExecutor {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	outputLimit := options.OutputLimit
	if outputLimit <= 0 {
		outputLimit = DefaultCommandOutputLimit
	}
	lookupEnv := options.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	runner := options.Runner
	if runner == nil {
		runner = defaultCommandRunner
	}
	clock := options.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return CommandExecutor{
		timeout:     timeout,
		outputLimit: outputLimit,
		lookupEnv:   lookupEnv,
		runner:      runner,
		clock:       clock,
	}
}

// ExecuteInWorkingDirectory executes one request from a freshly acquired,
// descriptor-backed working directory. Lexical WorkDir remains diagnostic
// input and never authorizes the child process directory.
func (executor CommandExecutor) ExecuteInWorkingDirectory(
	ctx context.Context,
	request CommandAttemptRequest,
	bind WorkingDirectoryBinder,
) CommandAttemptResult {
	if bind == nil {
		bind = func() (WorkingDirectoryBinding, error) {
			return nil, errors.New("working-directory binder is required")
		}
	}
	return executor.execute(ctx, request, bind)
}

func (executor CommandExecutor) execute(
	ctx context.Context,
	request CommandAttemptRequest,
	bind WorkingDirectoryBinder,
) CommandAttemptResult {
	executor = executor.withDefaults()
	attemptedAt := executor.clock()
	outputLimit := request.OutputLimit
	if outputLimit <= 0 {
		outputLimit = executor.outputLimit
	}
	env, secrets, missing := executor.resolveEnv(request.EnvRefs)
	if len(missing) != 0 {
		raw := CommandResult{Err: errors.New("missing env refs: " + strings.Join(missing, ", "))}
		capture := sanitizeCapture(raw, secrets, outputLimit)
		return newCommandAttemptResult(attemptedAt, false, CommandReasonMissingEnvRef, raw, capture)
	}

	var nativeWorkDir *os.File
	if bind != nil {
		binding, err := bind()
		if err != nil {
			return executor.workDirAuthorityFailure(attemptedAt, secrets, outputLimit, err)
		}
		if binding == nil {
			return executor.workDirAuthorityFailure(attemptedAt, secrets, outputLimit, errors.New("working-directory binding is required"))
		}
		defer binding.Close()
		if err := binding.Validate(); err != nil {
			return executor.workDirAuthorityFailure(attemptedAt, secrets, outputLimit, err)
		}
		directory, err := binding.OpenDirectory()
		if err != nil {
			return executor.workDirAuthorityFailure(attemptedAt, secrets, outputLimit, err)
		}
		if err := ValidateWorkingDirectory(directory); err != nil {
			if directory != nil {
				_ = directory.Close()
			}
			return executor.workDirAuthorityFailure(attemptedAt, secrets, outputLimit, err)
		}
		defer directory.Close()
		nativeWorkDir = directory

		runCtx, cancel := context.WithTimeout(ctx, executor.timeout)
		raw := executor.run(runCtx, request, env, outputLimit, nativeWorkDir)
		cancel()
		if err := binding.Validate(); err != nil {
			raw.WorkDirAuthorityFailed = true
			raw.Err = errors.Join(raw.Err, fmt.Errorf("working-directory authority changed: %w", err))
		}
		capture := sanitizeCapture(raw, secrets, outputLimit)
		return newCommandAttemptResult(attemptedAt, true, classifyCommandResult(raw), raw, capture)
	}

	runCtx, cancel := context.WithTimeout(ctx, executor.timeout)
	defer cancel()
	raw := executor.run(runCtx, request, env, outputLimit, nativeWorkDir)
	capture := sanitizeCapture(raw, secrets, outputLimit)
	return newCommandAttemptResult(attemptedAt, true, classifyCommandResult(raw), raw, capture)
}

func (executor CommandExecutor) run(
	runCtx context.Context,
	request CommandAttemptRequest,
	env []string,
	outputLimit int,
	nativeWorkDir *os.File,
) CommandResult {
	raw := executor.runner(runCtx, CommandRequest{
		Command:       request.Command,
		Args:          append([]string(nil), request.Args...),
		Env:           env,
		WorkDir:       request.WorkDir,
		Stdin:         request.Stdin,
		OutputLimit:   outputLimit,
		nativeWorkDir: nativeWorkDir,
	})
	if runCtx.Err() == context.DeadlineExceeded {
		raw.TimedOut = true
		if raw.Err == nil {
			raw.Err = runCtx.Err()
		}
	}
	if errors.Is(runCtx.Err(), context.Canceled) && !raw.TimedOut {
		raw.Canceled = true
		if raw.Err == nil {
			raw.Err = runCtx.Err()
		}
	}
	return raw
}

func (executor CommandExecutor) workDirAuthorityFailure(
	attemptedAt time.Time,
	secrets []string,
	outputLimit int,
	err error,
) CommandAttemptResult {
	raw := CommandResult{
		WorkDirAuthorityFailed: true,
		Err:                    fmt.Errorf("working-directory authority: %w", err),
	}
	capture := sanitizeCapture(raw, secrets, outputLimit)
	return newCommandAttemptResult(attemptedAt, false, CommandReasonWorkDirAuthority, raw, capture)
}

func (executor CommandExecutor) withDefaults() CommandExecutor {
	return NewCommandExecutor(CommandOptions{
		Timeout:     executor.timeout,
		OutputLimit: executor.outputLimit,
		LookupEnv:   executor.lookupEnv,
		Runner:      executor.runner,
		Clock:       executor.clock,
	})
}

func (executor CommandExecutor) resolveEnv(envRefs []CommandEnvRef) ([]string, []string, []string) {
	env := InheritedChildEnvironment()
	missing := make([]string, 0)
	seen := make(map[string]struct{}, len(envRefs))
	missingSources := make(map[string]struct{}, len(envRefs))
	for _, envRef := range envRefs {
		name := strings.TrimSpace(envRef.Name)
		sourceName := strings.TrimSpace(envRef.SourceName)
		if sourceName == "" {
			sourceName = name
		}
		if name == "" {
			name = sourceName
		}
		key := name + "\x00" + sourceName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		value, ok := executor.lookupEnv(sourceName)
		if !ok {
			if _, reported := missingSources[sourceName]; !reported {
				missingSources[sourceName] = struct{}{}
				missing = append(missing, sourceName)
			}
			continue
		}
		env = env.WithSecret(name, value)
	}
	sort.Strings(missing)
	return env.Entries(), env.SecretValues(), missing
}

func classifyCommandResult(result CommandResult) CommandReason {
	switch {
	case result.WorkDirAuthorityFailed:
		return CommandReasonWorkDirAuthority
	case result.TimedOut:
		return CommandReasonTimeout
	case result.Canceled:
		return CommandReasonCanceled
	case result.Signaled:
		return CommandReasonSignaled
	case result.MissingRunner:
		return CommandReasonMissingRunner
	case result.RunnerSetupFailed:
		return CommandReasonRunnerError
	case result.HasExitCode && result.ExitCode != 0:
		return CommandReasonNonZeroExit
	case result.Err != nil:
		return CommandReasonRunnerError
	default:
		return CommandReasonNone
	}
}

func newCommandAttemptResult(
	attemptedAt time.Time,
	runnerInvoked bool,
	reason CommandReason,
	result CommandResult,
	capture sanitizedCapture,
) CommandAttemptResult {
	return CommandAttemptResult{
		runnerInvoked:   runnerInvoked,
		started:         result.Started,
		attemptedAt:     attemptedAt,
		reason:          reason,
		exitCode:        result.ExitCode,
		hasExitCode:     result.HasExitCode,
		timedOut:        result.TimedOut,
		canceled:        result.Canceled,
		signaled:        result.Signaled,
		stdout:          capture.stdout,
		stderr:          capture.stderr,
		stdoutTruncated: capture.stdoutTruncated,
		stderrTruncated: capture.stderrTruncated,
		redacted:        capture.redacted,
		errorDetail:     capture.errorDetail,
	}
}
