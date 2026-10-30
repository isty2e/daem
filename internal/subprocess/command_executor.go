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
		timeout:      timeout,
		outputLimit:  outputLimit,
		lookupEnv:    lookupEnv,
		runner:       runner,
		nativeRunner: options.Runner == nil,
		clock:        clock,
	}
}

// Execute runs one already-authorized command attempt without a
// descriptor-backed working directory. Lexical WorkDir remains diagnostic
// input and never authorizes a child process directory.
func (executor CommandExecutor) Execute(
	ctx context.Context,
	request CommandAttemptRequest,
) CommandAttemptResult {
	return executor.execute(ctx, request, nil, nil)
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
	return executor.execute(ctx, request, bind, nil)
}

func (executor CommandExecutor) execute(
	ctx context.Context,
	request CommandAttemptRequest,
	bind WorkingDirectoryBinder,
	beforeCapture func(),
) CommandAttemptResult {
	executor = executor.withDefaults()
	attemptedAt := executor.clock()
	outputLimit := request.OutputLimit
	if outputLimit <= 0 {
		outputLimit = executor.outputLimit
	}
	env, secrets, missing := executor.resolveEnv(request.EnvRefs)
	finalized := false
	finalize := func() {
		if !finalized && beforeCapture != nil {
			finalized = true
			beforeCapture()
		}
	}
	finish := func(runnerInvoked bool, reason CommandReason, raw CommandResult) CommandAttemptResult {
		finalize()
		capture := sanitizeCapture(raw, secrets, outputLimit)
		return newCommandAttemptResult(attemptedAt, runnerInvoked, reason, raw, capture)
	}
	workDirFailure := func(err error) CommandAttemptResult {
		finalize()
		return finish(false, CommandReasonNone, CommandResult{
			WorkDirAuthorityFailed: true,
			Err:                    fmt.Errorf("working-directory authority: %w", err),
		})
	}

	if len(missing) != 0 {
		raw := CommandResult{Err: errors.New("missing env refs: " + strings.Join(missing, ", "))}
		return finish(false, CommandReasonMissingEnvRef, raw)
	}

	var nativeWorkDir *os.File
	if bind != nil {
		binding, err := bind()
		if err != nil {
			return workDirFailure(err)
		}
		if binding == nil {
			return workDirFailure(errors.New("working-directory binding is required"))
		}
		defer binding.Close()
		if err := binding.Validate(); err != nil {
			return workDirFailure(err)
		}
		directory, err := binding.OpenDirectory()
		if err != nil {
			return workDirFailure(err)
		}
		if err := ValidateWorkingDirectory(directory); err != nil {
			if directory != nil {
				_ = directory.Close()
			}
			return workDirFailure(err)
		}
		defer directory.Close()
		nativeWorkDir = directory

		runCtx, cancel := context.WithTimeout(ctx, executor.timeout)
		raw := executor.run(runCtx, request, env, outputLimit, nativeWorkDir)
		cancel()
		finalize()
		processReason := classifyCommandResult(raw)
		if err := binding.Validate(); err != nil {
			raw.WorkDirAuthorityFailed = true
			raw.Err = errors.Join(raw.Err, fmt.Errorf("working-directory authority changed: %w", err))
		}
		return finish(true, processReason, raw)
	}

	runCtx, cancel := context.WithTimeout(ctx, executor.timeout)
	defer cancel()
	raw := executor.run(runCtx, request, env, outputLimit, nativeWorkDir)
	return finish(true, classifyCommandResult(raw), raw)
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
	if raw.Started {
		// The runner classifies timeout/cancel from Await, before cleanup.
		// Do not re-read the attempt context after the runner returns.
		return raw
	}
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

func (executor CommandExecutor) withDefaults() CommandExecutor {
	normalized := NewCommandExecutor(CommandOptions{
		Timeout:     executor.timeout,
		OutputLimit: executor.outputLimit,
		LookupEnv:   executor.lookupEnv,
		Runner:      executor.runner,
		Clock:       executor.clock,
	})
	normalized.nativeRunner = executor.runner == nil || executor.nativeRunner
	return normalized
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
		runnerInvoked:          runnerInvoked,
		started:                result.Started,
		attemptedAt:            attemptedAt,
		reason:                 reason,
		exitCode:               result.ExitCode,
		hasExitCode:            result.HasExitCode,
		timedOut:               result.TimedOut,
		canceled:               result.Canceled,
		signaled:               result.Signaled,
		stdout:                 capture.stdout,
		stderr:                 capture.stderr,
		stdoutTruncated:        capture.stdoutTruncated,
		stderrTruncated:        capture.stderrTruncated,
		redacted:               capture.redacted,
		errorDetail:            capture.errorDetail,
		workDirAuthorityFailed: result.WorkDirAuthorityFailed,
	}
}
