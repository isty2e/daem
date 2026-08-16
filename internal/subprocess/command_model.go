package subprocess

import (
	"context"
	"os"
	"time"
)

const (
	DefaultCommandTimeout     = 30 * time.Second
	DefaultCommandOutputLimit = 8192
)

// CommandReason classifies the mechanical outcome of one host command attempt.
type CommandReason string

const (
	CommandReasonNone          CommandReason = ""
	CommandReasonMissingEnvRef CommandReason = "missing_env_ref"
	CommandReasonMissingRunner CommandReason = "missing_runner"
	CommandReasonNonZeroExit   CommandReason = "nonzero_exit"
	CommandReasonTimeout       CommandReason = "timeout"
	CommandReasonCanceled      CommandReason = "canceled"
	CommandReasonSignaled      CommandReason = "signaled"
	CommandReasonRunnerError   CommandReason = "runner_error"
)

// CommandEnvLookup resolves one host env reference at the execution boundary.
type CommandEnvLookup func(name string) (string, bool)

// CommandClock returns the attempt timestamp recorded by the execution boundary.
type CommandClock func() time.Time

// CommandRunner executes a prepared command request.
type CommandRunner func(ctx context.Context, request CommandRequest) CommandResult

// CommandEnvRef maps a host environment reference to a child process environment name.
type CommandEnvRef struct {
	Name       string
	SourceName string
}

// CommandAttemptRequest is the canonical command attempt request after route
// admission and host-specific request construction have already happened.
type CommandAttemptRequest struct {
	Command     string
	Args        []string
	EnvRefs     []CommandEnvRef
	WorkDir     string
	Stdin       string
	OutputLimit int
}

// CommandRequest is the effectful command request passed to the runner.
type CommandRequest struct {
	Command       string
	Args          []string
	Env           []string
	WorkDir       string
	Stdin         string
	OutputLimit   int
	nativeWorkDir *os.File
}

// CommandResult is the effectful runner result before redaction and
// classification.
type CommandResult struct {
	Started                bool
	Stdout                 string
	Stderr                 string
	StdoutTruncated        bool
	StderrTruncated        bool
	ExitCode               int
	HasExitCode            bool
	TimedOut               bool
	Canceled               bool
	Signaled               bool
	MissingRunner          bool
	RunnerSetupFailed      bool
	WorkDirAuthorityFailed bool
	Err                    error
}

// CommandOptions configures command attempt execution.
type CommandOptions struct {
	Timeout     time.Duration
	OutputLimit int
	LookupEnv   CommandEnvLookup
	Runner      CommandRunner
	Clock       CommandClock
}

// CommandExecutor executes already-authorized command attempts.
type CommandExecutor struct {
	timeout     time.Duration
	outputLimit int
	lookupEnv   CommandEnvLookup
	runner      CommandRunner
	clock       CommandClock
}

// CommandAttemptResult is the sanitized mechanical result of one command attempt.
type CommandAttemptResult struct {
	runnerInvoked          bool
	started                bool
	attemptedAt            time.Time
	reason                 CommandReason
	exitCode               int
	hasExitCode            bool
	timedOut               bool
	canceled               bool
	signaled               bool
	stdout                 string
	stderr                 string
	stdoutTruncated        bool
	stderrTruncated        bool
	redacted               bool
	errorDetail            string
	workDirAuthorityFailed bool
}

// RunnerInvoked reports whether environment and working-directory preflight
// reached the runner boundary.
func (result CommandAttemptResult) RunnerInvoked() bool {
	return result.runnerInvoked
}

// AttemptedAt returns the execution-boundary timestamp for this attempt result.
func (result CommandAttemptResult) AttemptedAt() time.Time {
	return result.attemptedAt
}

// Started reports whether the runner observed the child process as started.
func (result CommandAttemptResult) Started() bool {
	return result.started
}

// Succeeded reports whether the mechanical command attempt succeeded.
func (result CommandAttemptResult) Succeeded() bool {
	return result.runnerInvoked && result.reason == CommandReasonNone
}

// Failed reports whether the mechanical command attempt failed.
func (result CommandAttemptResult) Failed() bool {
	return !result.Succeeded()
}

// Reason returns the stable mechanical outcome reason.
func (result CommandAttemptResult) Reason() CommandReason {
	return result.reason
}

// WorkDirAuthorityFailed reports a working-directory authority failure that is
// independent from the child process's mechanical outcome.
func (result CommandAttemptResult) WorkDirAuthorityFailed() bool {
	return result.workDirAuthorityFailed
}

// ExitCode returns the observed process exit code when one was available.
func (result CommandAttemptResult) ExitCode() (int, bool) {
	return result.exitCode, result.hasExitCode
}

// TimedOut reports whether the command attempt exceeded its timeout.
func (result CommandAttemptResult) TimedOut() bool {
	return result.timedOut
}

// Canceled reports whether the command attempt context was canceled.
func (result CommandAttemptResult) Canceled() bool {
	return result.canceled
}

// Signaled reports whether the process ended because of a signal.
func (result CommandAttemptResult) Signaled() bool {
	return result.signaled
}

// Stdout returns sanitized bounded stdout.
func (result CommandAttemptResult) Stdout() string {
	return result.stdout
}

// Stderr returns sanitized bounded stderr.
func (result CommandAttemptResult) Stderr() string {
	return result.stderr
}

// StdoutTruncated reports whether stdout was bounded.
func (result CommandAttemptResult) StdoutTruncated() bool {
	return result.stdoutTruncated
}

// StderrTruncated reports whether stderr was bounded.
func (result CommandAttemptResult) StderrTruncated() bool {
	return result.stderrTruncated
}

// Redacted reports whether output or error detail was redacted.
func (result CommandAttemptResult) Redacted() bool {
	return result.redacted
}

// ErrorDetail returns sanitized runner error detail.
func (result CommandAttemptResult) ErrorDetail() string {
	return result.errorDetail
}
