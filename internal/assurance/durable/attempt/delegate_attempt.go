package attempt

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// DelegateAttemptStatus is the terminal class of a delegated attempt.
type DelegateAttemptStatus string

const (
	DelegateStatusSucceeded DelegateAttemptStatus = "succeeded"
	DelegateStatusFailed    DelegateAttemptStatus = "failed"
	DelegateStatusBlocked   DelegateAttemptStatus = "blocked"
)

// DelegateAttemptReason is the stable terminal reason for a delegated attempt.
type DelegateAttemptReason string

const (
	DelegateReasonNone             DelegateAttemptReason = "none"
	DelegateReasonPolicyBlocked    DelegateAttemptReason = "policy_blocked"
	DelegateReasonMissingEnvRef    DelegateAttemptReason = "missing_env_ref"
	DelegateReasonMissingRunner    DelegateAttemptReason = "missing_runner"
	DelegateReasonNonZeroExit      DelegateAttemptReason = "nonzero_exit"
	DelegateReasonTimeout          DelegateAttemptReason = "timeout"
	DelegateReasonRunnerError      DelegateAttemptReason = "runner_error"
	DelegateReasonWorkDirAuthority DelegateAttemptReason = "workdir_authority"
)

// DelegateAttemptInput contains the bounded facts needed to build history.
type DelegateAttemptInput struct {
	Subject         topology.SubjectID
	Target          target.Target
	Scope           target.Scope
	PlanIdentityKey string
	ObservedAt      time.Time
	Status          DelegateAttemptStatus
	Reason          DelegateAttemptReason
	Observation     observerelation.ObservationSummary
	Postcondition   observerelation.PostconditionSummary
	ExitCode        *int
	TimedOut        bool
	StdoutTruncated bool
	StderrTruncated bool
	Redacted        bool
}

// DelegateAttempt records bounded history for one delegated projection.
type DelegateAttempt struct {
	subject         topology.SubjectID
	target          target.Target
	scope           target.Scope
	planIdentityKey string
	observedAt      time.Time
	status          DelegateAttemptStatus
	reason          DelegateAttemptReason
	observation     observerelation.ObservationSummary
	postcondition   observerelation.PostconditionSummary
	exitCode        int
	hasExitCode     bool
	timedOut        bool
	stdoutTruncated bool
	stderrTruncated bool
	redacted        bool
}

// NewDelegateAttempt constructs one sanitized historical delegate attempt.
func NewDelegateAttempt(input DelegateAttemptInput) (DelegateAttempt, error) {
	observation, err := observerelation.ParseObservationSummary(string(input.Observation))
	if err != nil {
		return DelegateAttempt{}, fmt.Errorf("delegate attempt observation: %w", err)
	}
	postcondition, err := observerelation.ParsePostconditionSummary(string(input.Postcondition))
	if err != nil {
		return DelegateAttempt{}, fmt.Errorf("delegate attempt postcondition: %w", err)
	}
	attempt := DelegateAttempt{
		subject:         input.Subject,
		target:          input.Target,
		scope:           input.Scope,
		planIdentityKey: input.PlanIdentityKey,
		observedAt:      input.ObservedAt.UTC(),
		status:          input.Status,
		reason:          input.Reason,
		observation:     observation,
		postcondition:   postcondition,
		timedOut:        input.TimedOut,
		stdoutTruncated: input.StdoutTruncated,
		stderrTruncated: input.StderrTruncated,
		redacted:        input.Redacted,
	}
	if input.ExitCode != nil {
		attempt.exitCode = *input.ExitCode
		attempt.hasExitCode = true
	}
	if err := attempt.Validate(); err != nil {
		return DelegateAttempt{}, err
	}
	return attempt, nil
}

// Validate rejects a zero, forged, or internally contradictory attempt.
func (attempt DelegateAttempt) Validate() error {
	if err := validateAttemptIdentity(
		attempt.subject,
		topology.SubjectProjection,
		attempt.target,
		attempt.scope,
		"delegate attempt",
	); err != nil {
		return err
	}
	if err := validateCanonicalIdentityText(
		attempt.planIdentityKey,
		"delegate attempt plan identity key",
	); err != nil {
		return err
	}
	if attempt.observedAt.IsZero() {
		return fmt.Errorf("delegate attempt observed time is required")
	}
	if err := validateHistoricalTime(attempt.observedAt, "delegate attempt observed time"); err != nil {
		return err
	}
	if err := validateDelegateStatusReason(attempt.status, attempt.reason); err != nil {
		return err
	}
	if attempt.timedOut && attempt.reason != DelegateReasonTimeout {
		return fmt.Errorf("delegate attempt timed_out requires timeout reason")
	}
	if attempt.reason == DelegateReasonTimeout && !attempt.timedOut {
		return fmt.Errorf("delegate attempt timeout reason requires timed_out")
	}
	if attempt.status == DelegateStatusSucceeded && attempt.hasExitCode && attempt.exitCode != 0 {
		return fmt.Errorf("succeeded delegate attempt cannot record nonzero exit code")
	}
	if attempt.reason == DelegateReasonNonZeroExit &&
		(!attempt.hasExitCode || attempt.exitCode == 0) {
		return fmt.Errorf("delegate attempt nonzero_exit reason requires a nonzero exit code")
	}
	if attempt.reason == DelegateReasonPolicyBlocked ||
		attempt.reason == DelegateReasonMissingEnvRef ||
		attempt.reason == DelegateReasonMissingRunner {
		if attempt.hasProcessFacts() {
			return fmt.Errorf("delegate attempt %s cannot record process facts", attempt.reason)
		}
	}
	if attempt.reason == DelegateReasonRunnerError &&
		attempt.hasExitCode && attempt.exitCode != 0 {
		return fmt.Errorf("delegate attempt runner_error cannot record a nonzero exit code")
	}
	return nil
}

func (attempt DelegateAttempt) hasProcessFacts() bool {
	return attempt.hasExitCode ||
		attempt.timedOut ||
		attempt.stdoutTruncated ||
		attempt.stderrTruncated
}

// Subject returns the projection subject associated with this attempt.
func (attempt DelegateAttempt) Subject() topology.SubjectID { return attempt.subject }

// Target returns the delegated host target.
func (attempt DelegateAttempt) Target() target.Target { return attempt.target }

// Scope returns the delegated host scope.
func (attempt DelegateAttempt) Scope() target.Scope { return attempt.scope }

// PlanIdentityKey returns the exact locked delegate-plan identity.
func (attempt DelegateAttempt) PlanIdentityKey() string { return attempt.planIdentityKey }

// ObservedAt returns the historical attempt timestamp.
func (attempt DelegateAttempt) ObservedAt() time.Time { return attempt.observedAt }

// Status returns the attempt terminal class.
func (attempt DelegateAttempt) Status() DelegateAttemptStatus { return attempt.status }

// Reason returns the attempt terminal reason.
func (attempt DelegateAttempt) Reason() DelegateAttemptReason { return attempt.reason }

// ObservationSummary returns the persisted passive observation summary.
func (attempt DelegateAttempt) ObservationSummary() observerelation.ObservationSummary {
	return attempt.observation
}

// PostconditionSummary returns the persisted postcondition summary.
func (attempt DelegateAttempt) PostconditionSummary() observerelation.PostconditionSummary {
	return attempt.postcondition
}

// ExitCode returns the bounded process exit code when one was observed.
func (attempt DelegateAttempt) ExitCode() (int, bool) {
	return attempt.exitCode, attempt.hasExitCode
}

func (attempt DelegateAttempt) TimedOut() bool        { return attempt.timedOut }
func (attempt DelegateAttempt) StdoutTruncated() bool { return attempt.stdoutTruncated }
func (attempt DelegateAttempt) StderrTruncated() bool { return attempt.stderrTruncated }
func (attempt DelegateAttempt) Redacted() bool        { return attempt.redacted }

// SemanticKey returns the history replacement identity owned by this attempt.
func (attempt DelegateAttempt) SemanticKey() DelegateAttemptKey {
	return DelegateAttemptKey{
		subject: attempt.subject,
		target:  attempt.target,
		scope:   attempt.scope,
	}
}

// Compare returns the canonical persisted order between delegate attempts.
func (attempt DelegateAttempt) Compare(other DelegateAttempt) int {
	return cmp.Or(
		cmp.Compare(attempt.subject.String(), other.subject.String()),
		cmp.Compare(attempt.target, other.target),
		cmp.Compare(attempt.scope, other.scope),
		cmp.Compare(attempt.planIdentityKey, other.planIdentityKey),
	)
}

// MatchesPlanIdentity establishes historical relevance only.
func (attempt DelegateAttempt) MatchesPlanIdentity(identityKey string) bool {
	return attempt.planIdentityKey == strings.TrimSpace(identityKey) &&
		strings.TrimSpace(identityKey) != ""
}

// Equal reports semantic equality between delegate-attempt records.
func (attempt DelegateAttempt) Equal(other DelegateAttempt) bool {
	return attempt.subject == other.subject &&
		attempt.target == other.target &&
		attempt.scope == other.scope &&
		attempt.planIdentityKey == other.planIdentityKey &&
		attempt.observedAt.Equal(other.observedAt) &&
		attempt.status == other.status &&
		attempt.reason == other.reason &&
		attempt.observation == other.observation &&
		attempt.postcondition == other.postcondition &&
		attempt.exitCode == other.exitCode &&
		attempt.hasExitCode == other.hasExitCode &&
		attempt.timedOut == other.timedOut &&
		attempt.stdoutTruncated == other.stdoutTruncated &&
		attempt.stderrTruncated == other.stderrTruncated &&
		attempt.redacted == other.redacted
}

func validateDelegateStatusReason(status DelegateAttemptStatus, reason DelegateAttemptReason) error {
	switch status {
	case DelegateStatusSucceeded:
		if reason != DelegateReasonNone {
			return fmt.Errorf("succeeded delegate attempt requires reason %q", DelegateReasonNone)
		}
	case DelegateStatusFailed:
		switch reason {
		case DelegateReasonMissingEnvRef, DelegateReasonMissingRunner, DelegateReasonNonZeroExit,
			DelegateReasonTimeout, DelegateReasonRunnerError, DelegateReasonWorkDirAuthority:
		default:
			return fmt.Errorf("failed delegate attempt reason %q is unsupported", reason)
		}
	case DelegateStatusBlocked:
		if reason != DelegateReasonPolicyBlocked {
			return fmt.Errorf("blocked delegate attempt requires reason %q", DelegateReasonPolicyBlocked)
		}
	default:
		return fmt.Errorf("delegate attempt status %q is unsupported", status)
	}
	return nil
}
