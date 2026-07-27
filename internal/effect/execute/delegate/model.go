package delegate

import (
	"fmt"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"

	"github.com/isty2e/daem/internal/topology"
)

const (
	DefaultTimeout     = subprocess.DefaultCommandTimeout
	DefaultOutputLimit = subprocess.DefaultCommandOutputLimit
)

// Reason classifies one delegate attempt without presentation wording.
type Reason string

const (
	ReasonNone             Reason = ""
	ReasonNotScheduled     Reason = "not_scheduled"
	ReasonPolicyBlocked    Reason = "policy_blocked"
	ReasonMissingEnvRef    Reason = "missing_env_ref"
	ReasonMissingRunner    Reason = "missing_runner"
	ReasonNonZeroExit      Reason = "nonzero_exit"
	ReasonTimeout          Reason = "timeout"
	ReasonRunnerError      Reason = "runner_error"
	ReasonWorkDirAuthority Reason = "workdir_authority"
)

// Options configures delegate attempt execution.
type Options struct {
	Timeout     time.Duration
	OutputLimit int
	LookupEnv   subprocess.CommandEnvLookup
	Runner      subprocess.CommandRunner
}

// Executor executes scheduled delegate actions.
type Executor struct {
	timeout     time.Duration
	outputLimit int
	lookupEnv   subprocess.CommandEnvLookup
	runner      subprocess.CommandRunner
}

// BinderForAction selects working-directory authority for one action.
type BinderForAction func(reconcile.DelegateAction) subprocess.WorkingDirectoryBinder

// AttemptStatus is an Effect-owned observed attempt outcome.
type AttemptStatus string

const (
	AttemptSkipped   AttemptStatus = "skipped"
	AttemptBlocked   AttemptStatus = "blocked"
	AttemptSucceeded AttemptStatus = "succeeded"
	AttemptFailed    AttemptStatus = "failed"
)

// AttemptRecord is the sanitized in-memory record for one delegate attempt.
type AttemptRecord struct {
	subject         topology.SubjectID
	target          string
	scope           string
	identityKey     string
	status          AttemptStatus
	reason          Reason
	exitCode        int
	hasExitCode     bool
	timedOut        bool
	stdout          string
	stderr          string
	stdoutTruncated bool
	stderrTruncated bool
	redacted        bool
	errorDetail     string
}

// ExecutionError reports one or more failed delegate attempts.
type ExecutionError struct {
	records []AttemptRecord
}

// Error returns sanitized failure detail.
func (err ExecutionError) Error() string {
	if len(err.records) == 0 {
		return "delegate attempt failed"
	}
	parts := make([]string, 0, len(err.records))
	for _, record := range err.records {
		parts = append(parts, fmt.Sprintf("%s/%s %q: %s", record.subject.Kind(), record.subject.Namespace(), record.subject.Key(), record.reason))
	}
	return "delegate attempt failed: " + strings.Join(parts, "; ")
}

// AttemptRecords returns the failed attempt records that caused the error.
func (err ExecutionError) AttemptRecords() []AttemptRecord {
	return cloneAttemptRecords(err.records)
}

// Subject returns the locked subject for this attempt record.
func (record AttemptRecord) Subject() topology.SubjectID {
	return record.subject
}

// Target returns the selected target for this attempt record.
func (record AttemptRecord) Target() string {
	return record.target
}

// Scope returns the selected scope for this attempt record.
func (record AttemptRecord) Scope() string {
	return record.scope
}

// IdentityKey returns the locked delegate plan identity key.
func (record AttemptRecord) IdentityKey() string {
	return record.identityKey
}

// Status returns the execution lifecycle status.
func (record AttemptRecord) Status() AttemptStatus {
	return record.status
}

// Reason returns the stable execution reason.
func (record AttemptRecord) Reason() Reason {
	return record.reason
}

// ExitCode returns the process exit code when the runner observed one.
func (record AttemptRecord) ExitCode() (int, bool) {
	return record.exitCode, record.hasExitCode
}

// TimedOut reports whether the process timed out.
func (record AttemptRecord) TimedOut() bool {
	return record.timedOut
}

// Stdout returns sanitized bounded stdout.
func (record AttemptRecord) Stdout() string {
	return record.stdout
}

// Stderr returns sanitized bounded stderr.
func (record AttemptRecord) Stderr() string {
	return record.stderr
}

// StdoutTruncated reports whether stdout was bounded.
func (record AttemptRecord) StdoutTruncated() bool {
	return record.stdoutTruncated
}

// StderrTruncated reports whether stderr was bounded.
func (record AttemptRecord) StderrTruncated() bool {
	return record.stderrTruncated
}

// Redacted reports whether any output or error detail was redacted.
func (record AttemptRecord) Redacted() bool {
	return record.redacted
}

// ErrorDetail returns sanitized runner error detail.
func (record AttemptRecord) ErrorDetail() string {
	return record.errorDetail
}

// Failed reports whether the record represents a failed delegate attempt.
func (record AttemptRecord) Failed() bool {
	return record.status == AttemptFailed || record.status == AttemptBlocked
}

func newAttemptRecord(
	action reconcile.DelegateAction,
	status AttemptStatus,
	reason Reason,
	result subprocess.CommandAttemptResult,
) AttemptRecord {
	subject := action.Subject()
	exitCode, hasExitCode := result.ExitCode()
	return AttemptRecord{
		subject:         subject,
		target:          string(action.Target()),
		scope:           string(action.Scope()),
		identityKey:     action.Plan().IdentityKey(),
		status:          status,
		reason:          reason,
		exitCode:        exitCode,
		hasExitCode:     hasExitCode,
		timedOut:        result.TimedOut(),
		stdout:          result.Stdout(),
		stderr:          result.Stderr(),
		stdoutTruncated: result.StdoutTruncated(),
		stderrTruncated: result.StderrTruncated(),
		redacted:        result.Redacted(),
		errorDetail:     result.ErrorDetail(),
	}
}

func cloneAttemptRecords(values []AttemptRecord) []AttemptRecord {
	return append([]AttemptRecord(nil), values...)
}
