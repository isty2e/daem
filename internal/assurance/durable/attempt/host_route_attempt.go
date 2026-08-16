package attempt

import (
	"cmp"
	"fmt"
	"time"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	assurancepostcondition "github.com/isty2e/daem/internal/assurance/postcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// HostRouteResultClass is the bounded class of one host-route attempt.
type HostRouteResultClass string

const (
	HostRouteResultAttemptedObservedPresent HostRouteResultClass = "attempted_observed_present"
	HostRouteResultAttemptedObservedAbsent  HostRouteResultClass = "attempted_observed_absent"
	HostRouteResultAmbiguousObservation     HostRouteResultClass = "ambiguous_observation"
	HostRouteResultAttemptedUnverified      HostRouteResultClass = "attempted_unverified"
	HostRouteResultFailed                   HostRouteResultClass = "failed"
	HostRouteResultBlocked                  HostRouteResultClass = "blocked"
	HostRouteResultBlockedPreflight         HostRouteResultClass = "blocked_preflight"
	HostRouteResultUnsupportedSource        HostRouteResultClass = "unsupported_source"
	HostRouteResultUnsupportedScope         HostRouteResultClass = "unsupported_scope"
)

// HostRouteResultReason is the stable classification or preflight reason.
type HostRouteResultReason string

const (
	HostRouteReasonCommandFailed          HostRouteResultReason = "command_failed"
	HostRouteReasonMissingEnvRef          HostRouteResultReason = "missing_env_ref"
	HostRouteReasonMissingRunner          HostRouteResultReason = "missing_runner"
	HostRouteReasonNonZeroExit            HostRouteResultReason = "nonzero_exit"
	HostRouteReasonTimeout                HostRouteResultReason = "timeout"
	HostRouteReasonRunnerError            HostRouteResultReason = "runner_error"
	HostRouteReasonWorkDirAuthority       HostRouteResultReason = "workdir_authority"
	HostRouteReasonObservedPresent        HostRouteResultReason = "observed_present"
	HostRouteReasonObservedAbsent         HostRouteResultReason = "observed_absent"
	HostRouteReasonUnkeyedSameSubject     HostRouteResultReason = "unkeyed_same_subject"
	HostRouteReasonSameSubjectShadow      HostRouteResultReason = "same_name_shadow"
	HostRouteReasonManagedKeyDrift        HostRouteResultReason = "managed_key_drift"
	HostRouteReasonAmbiguousRelation      HostRouteResultReason = "ambiguous_relation"
	HostRouteReasonObservationUnavailable HostRouteResultReason = "observation_unavailable"
	HostRouteReasonObservationUnsupported HostRouteResultReason = "observation_unsupported"
	HostRouteReasonObservationStale       HostRouteResultReason = "observation_stale"
	HostRouteReasonObservationParseFailed HostRouteResultReason = "observation_parse_failed"
	HostRouteReasonEffectMissing          HostRouteResultReason = "effect_postcondition_missing"
	HostRouteReasonEffectUnsatisfied      HostRouteResultReason = "effect_postcondition_unsatisfied"
	HostRouteReasonEffectUnavailable      HostRouteResultReason = "effect_postcondition_unavailable"
	HostRouteReasonEffectStale            HostRouteResultReason = "effect_postcondition_stale"
	HostRouteReasonEffectMalformed        HostRouteResultReason = "effect_postcondition_malformed"
	HostRouteReasonEffectUnsafe           HostRouteResultReason = "effect_postcondition_unsafe"
	HostRouteReasonEffectContradictory    HostRouteResultReason = "effect_postcondition_contradictory"
	HostRouteReasonEffectForeign          HostRouteResultReason = "effect_postcondition_foreign"
	HostRouteReasonPreflightFailed        HostRouteResultReason = "preflight_failed"
	HostRouteReasonUnsupportedAction      HostRouteResultReason = "unsupported_action"
	HostRouteReasonMissingWorkDir         HostRouteResultReason = "missing_workdir"
	HostRouteReasonLockedSubjectMissing   HostRouteResultReason = "locked_subject_missing"
	HostRouteReasonLockedSubjectAmbiguous HostRouteResultReason = "locked_subject_ambiguous"
	HostRouteReasonUnsupportedRoute       HostRouteResultReason = "unsupported_route"
	HostRouteReasonInvalidLockedRecord    HostRouteResultReason = "invalid_locked_record"
	HostRouteReasonRouteRequestMismatch   HostRouteResultReason = "route_request_mismatch"
	HostRouteReasonTargetMismatch         HostRouteResultReason = "target_mismatch"
	HostRouteReasonScopeMismatch          HostRouteResultReason = "scope_mismatch"
	HostRouteReasonRelationKeyMismatch    HostRouteResultReason = "relation_subject_key_mismatch"
	HostRouteReasonUnsupportedScope       HostRouteResultReason = "unsupported_scope"
	HostRouteReasonUnsupportedSource      HostRouteResultReason = "unsupported_source"
)

// HostRouteAttemptReason is the bounded mechanical attempt reason.
type HostRouteAttemptReason string

const (
	HostRouteAttemptReasonNone          HostRouteAttemptReason = ""
	HostRouteAttemptReasonMissingEnvRef HostRouteAttemptReason = "missing_env_ref"
	HostRouteAttemptReasonMissingRunner HostRouteAttemptReason = "missing_runner"
	HostRouteAttemptReasonNonZeroExit   HostRouteAttemptReason = "nonzero_exit"
	HostRouteAttemptReasonTimeout       HostRouteAttemptReason = "timeout"
	HostRouteAttemptReasonCanceled      HostRouteAttemptReason = "canceled"
	HostRouteAttemptReasonSignaled      HostRouteAttemptReason = "signaled"
	HostRouteAttemptReasonRunnerError   HostRouteAttemptReason = "runner_error"
)

// HostRouteAttemptInput contains the bounded facts needed to build route history.
type HostRouteAttemptInput struct {
	Subject              topology.SubjectID
	Target               target.Target
	Scope                target.Scope
	Operation            lock.OperationKind
	RouteID              string
	RouteRequestHash     string
	ObservedAt           time.Time
	ResultClass          HostRouteResultClass
	Reason               HostRouteResultReason
	AttemptObserved      bool
	AttemptReason        HostRouteAttemptReason
	Observation          observerelation.ObservationSummary
	Postcondition        observerelation.PostconditionSummary
	EffectPostconditions assurancepostcondition.SummarySet
	ExitCode             *int
	TimedOut             bool
	Redacted             bool
}

// HostRouteAttempt records bounded, history-only host-route diagnostics.
type HostRouteAttempt struct {
	subject              topology.SubjectID
	target               target.Target
	scope                target.Scope
	operation            lock.OperationKind
	routeID              string
	routeRequestHash     string
	observedAt           time.Time
	resultClass          HostRouteResultClass
	reason               HostRouteResultReason
	attemptObserved      bool
	attemptReason        HostRouteAttemptReason
	observation          observerelation.ObservationSummary
	postcondition        observerelation.PostconditionSummary
	effectPostconditions assurancepostcondition.SummarySet
	exitCode             int
	hasExitCode          bool
	timedOut             bool
	redacted             bool
}

// NewHostRouteAttempt constructs one bounded historical host-route attempt.
func NewHostRouteAttempt(input HostRouteAttemptInput) (HostRouteAttempt, error) {
	observation, err := observerelation.ParseObservationSummary(string(input.Observation))
	if err != nil {
		return HostRouteAttempt{}, fmt.Errorf("host route attempt observation: %w", err)
	}
	postcondition, err := observerelation.ParsePostconditionSummary(string(input.Postcondition))
	if err != nil {
		return HostRouteAttempt{}, fmt.Errorf("host route attempt postcondition: %w", err)
	}
	if err := input.EffectPostconditions.Validate(); err != nil {
		return HostRouteAttempt{}, fmt.Errorf(
			"host route attempt effect postconditions: %w",
			err,
		)
	}
	attempt := HostRouteAttempt{
		subject:              input.Subject,
		target:               input.Target,
		scope:                input.Scope,
		operation:            input.Operation,
		routeID:              input.RouteID,
		routeRequestHash:     input.RouteRequestHash,
		observedAt:           input.ObservedAt.UTC(),
		resultClass:          input.ResultClass,
		reason:               input.Reason,
		attemptObserved:      input.AttemptObserved,
		attemptReason:        input.AttemptReason,
		observation:          observation,
		postcondition:        postcondition,
		effectPostconditions: input.EffectPostconditions,
		timedOut:             input.TimedOut,
		redacted:             input.Redacted,
	}
	if input.ExitCode != nil {
		attempt.exitCode = *input.ExitCode
		attempt.hasExitCode = true
	}
	if err := attempt.Validate(); err != nil {
		return HostRouteAttempt{}, err
	}
	return attempt, nil
}

// Validate rejects a zero, forged, or internally contradictory host-route attempt.
func (attempt HostRouteAttempt) Validate() error {
	if err := validateAttemptIdentity(
		attempt.subject,
		topology.SubjectHostRelation,
		attempt.target,
		attempt.scope,
		"host route attempt",
	); err != nil {
		return err
	}
	if _, err := lock.ParseOperationKind(string(attempt.operation)); err != nil {
		return fmt.Errorf("host route attempt operation: %w", err)
	}
	if err := validateCanonicalIdentityText(
		attempt.routeID,
		"host route attempt route id",
	); err != nil {
		return err
	}
	if err := validateRouteRequestHash(
		attempt.routeRequestHash,
		"host route attempt",
	); err != nil {
		return err
	}
	if attempt.observedAt.IsZero() {
		return fmt.Errorf("host route attempt observed time is required")
	}
	if err := validateHistoricalTime(attempt.observedAt, "host route attempt observed time"); err != nil {
		return err
	}
	if !attempt.resultClass.valid() {
		return fmt.Errorf("unsupported host route result class %q", attempt.resultClass)
	}
	if !attempt.reason.valid() {
		return fmt.Errorf("unsupported host route result reason %q", attempt.reason)
	}
	if !attempt.attemptReason.valid() {
		return fmt.Errorf("unsupported host route attempt reason %q", attempt.attemptReason)
	}
	if !attempt.attemptObserved &&
		(attempt.attemptReason != HostRouteAttemptReasonNone ||
			attempt.hasExitCode ||
			attempt.timedOut) {
		return fmt.Errorf("unobserved host route attempt cannot record process facts")
	}
	if attempt.timedOut && attempt.attemptReason != HostRouteAttemptReasonTimeout {
		return fmt.Errorf("host route attempt timed_out requires timeout attempt reason")
	}
	if attempt.attemptReason == HostRouteAttemptReasonTimeout && !attempt.timedOut {
		return fmt.Errorf("host route timeout attempt reason requires timed_out")
	}
	if attempt.attemptReason == HostRouteAttemptReasonNonZeroExit &&
		(!attempt.hasExitCode || attempt.exitCode == 0) {
		return fmt.Errorf("host route nonzero_exit attempt reason requires a nonzero exit code")
	}
	if err := validateHostRouteClassification(
		attempt.resultClass,
		attempt.reason,
		attempt.attemptObserved,
	); err != nil {
		return err
	}
	if err := validateHostRouteAttemptCorrelation(
		attempt.resultClass,
		attempt.reason,
		attempt.attemptReason,
	); err != nil {
		return err
	}
	if err := validateEffectPostconditionSummaryCorrelation(
		attempt.resultClass,
		attempt.reason,
		attempt.effectPostconditions,
	); err != nil {
		return err
	}
	return nil
}

func (attempt HostRouteAttempt) Subject() topology.SubjectID   { return attempt.subject }
func (attempt HostRouteAttempt) Target() target.Target         { return attempt.target }
func (attempt HostRouteAttempt) Scope() target.Scope           { return attempt.scope }
func (attempt HostRouteAttempt) Operation() lock.OperationKind { return attempt.operation }
func (attempt HostRouteAttempt) RouteID() string               { return attempt.routeID }
func (attempt HostRouteAttempt) RouteRequestHash() string      { return attempt.routeRequestHash }
func (attempt HostRouteAttempt) ObservedAt() time.Time         { return attempt.observedAt }
func (attempt HostRouteAttempt) ResultClass() HostRouteResultClass {
	return attempt.resultClass
}
func (attempt HostRouteAttempt) Reason() HostRouteResultReason { return attempt.reason }
func (attempt HostRouteAttempt) AttemptObserved() bool {
	return attempt.attemptObserved
}

func (attempt HostRouteAttempt) AttemptReason() HostRouteAttemptReason {
	return attempt.attemptReason
}

func (attempt HostRouteAttempt) ObservationSummary() observerelation.ObservationSummary {
	return attempt.observation
}

func (attempt HostRouteAttempt) PostconditionSummary() observerelation.PostconditionSummary {
	return attempt.postcondition
}

func (attempt HostRouteAttempt) EffectPostconditions() assurancepostcondition.SummarySet {
	return attempt.effectPostconditions
}

func (attempt HostRouteAttempt) ExitCode() (int, bool) {
	return attempt.exitCode, attempt.hasExitCode
}
func (attempt HostRouteAttempt) TimedOut() bool { return attempt.timedOut }
func (attempt HostRouteAttempt) Redacted() bool { return attempt.redacted }

// SemanticKey returns the history replacement identity owned by this attempt.
func (attempt HostRouteAttempt) SemanticKey() HostRouteAttemptKey {
	return HostRouteAttemptKey{
		subject:   attempt.subject,
		target:    attempt.target,
		scope:     attempt.scope,
		operation: attempt.operation,
		routeID:   attempt.routeID,
	}
}

// Compare returns the canonical persisted order between host-route attempts.
func (attempt HostRouteAttempt) Compare(other HostRouteAttempt) int {
	return cmp.Or(
		cmp.Compare(attempt.subject.String(), other.subject.String()),
		cmp.Compare(attempt.target, other.target),
		cmp.Compare(attempt.scope, other.scope),
		cmp.Compare(attempt.operation, other.operation),
		cmp.Compare(attempt.routeID, other.routeID),
		cmp.Compare(attempt.routeRequestHash, other.routeRequestHash),
	)
}

// Equal reports semantic equality between host-route attempt records.
func (attempt HostRouteAttempt) Equal(other HostRouteAttempt) bool {
	return attempt.subject == other.subject &&
		attempt.target == other.target &&
		attempt.scope == other.scope &&
		attempt.operation == other.operation &&
		attempt.routeID == other.routeID &&
		attempt.routeRequestHash == other.routeRequestHash &&
		attempt.observedAt.Equal(other.observedAt) &&
		attempt.resultClass == other.resultClass &&
		attempt.reason == other.reason &&
		attempt.attemptObserved == other.attemptObserved &&
		attempt.attemptReason == other.attemptReason &&
		attempt.observation == other.observation &&
		attempt.postcondition == other.postcondition &&
		attempt.effectPostconditions.Equal(other.effectPostconditions) &&
		attempt.exitCode == other.exitCode &&
		attempt.hasExitCode == other.hasExitCode &&
		attempt.timedOut == other.timedOut &&
		attempt.redacted == other.redacted
}
