// Package refresh implements the explicit, single-relation delegated carrier
// refresh workflow.
package refresh

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type Mode string

const (
	ModeDryRun  Mode = "dry_run"
	ModeExecute Mode = "execute"
)

type ResultClass string

const (
	ResultPlanned             ResultClass = "planned"
	ResultRefused             ResultClass = "refused"
	ResultCancelled           ResultClass = "cancelled"
	ResultAttemptedUnverified ResultClass = "attempted_unverified"
	ResultObservedRelation    ResultClass = "observed_relation"
	ResultFailed              ResultClass = "failed"
	ResultPartial             ResultClass = "partial"
)

type ReasonCode string

const (
	ReasonNone                   ReasonCode = ""
	ReasonInvalidSelection       ReasonCode = "invalid_selection"
	ReasonManifestUnavailable    ReasonCode = "manifest_unavailable"
	ReasonLockUnavailable        ReasonCode = "lock_unavailable"
	ReasonLockMismatch           ReasonCode = "lock_mismatch"
	ReasonRefreshUnsupported     ReasonCode = "refresh_unsupported"
	ReasonRelationMissing        ReasonCode = "relation_missing"
	ReasonRelationAmbiguous      ReasonCode = "relation_ambiguous"
	ReasonObservationUnavailable ReasonCode = "observation_unavailable"
	ReasonStalePlan              ReasonCode = "stale_plan"
	ReasonMutationAuthority      ReasonCode = "mutation_authority"
	ReasonCommandFailed          ReasonCode = "command_failed"
	ReasonInvalidTimeout         ReasonCode = "invalid_timeout"
	ReasonPostObservationFailed  ReasonCode = "post_observation_failed"
	ReasonAttemptPersistence     ReasonCode = "attempt_persistence_failed"
	ReasonCancelled              ReasonCode = "cancelled"
)

type ObservationPosture string

const (
	PostureRequireCurrent         ObservationPosture = "require_current"
	PostureAttemptWhenUnsupported ObservationPosture = "attempt_when_unsupported"
)

// CommandInput identifies one exact declared extension relation.
type CommandInput struct {
	ManifestPath string
	ExtensionID  string
	TargetValue  string
	ScopeValue   string
	Timeout      time.Duration
}

// Selection is the selected user-facing relation identity.
type Selection struct {
	ID      string
	Target  target.Target
	Scope   target.Scope
	Carrier string
}

// Route is the exact operation-indexed locked route identity.
type Route struct {
	Operation              lock.OperationKind
	RouteID                string
	AdapterContractVersion string
	RequestHash            string
	ExecutionSubject       string
	ObservationPosture     ObservationPosture
}

// Invocation is one deterministic, secret-free command disclosure.
type Invocation struct {
	Kind           string
	Command        string
	Args           []string
	EnvNames       []string
	CWDPolicy      string
	TimeoutSeconds int
}

// Disclosure contains the complete command invocation and adapter-owned effect
// disclosure selected for one refresh attempt.
type Disclosure struct {
	Invocation            Invocation
	EffectClasses         []string
	RetainedEffectClasses []string
	NonClaims             []string
}

// Observation is one bounded relation observation summary.
type Observation struct {
	State        observerelation.CorrelationState
	Reason       observerelation.ReasonCode
	Availability observerelation.InventoryAvailability
	Freshness    observerelation.EvidenceFreshness
}

// ProcessOutcome is the bounded mechanical result returned to presentation.
type ProcessOutcome struct {
	Started   bool
	Reason    subprocess.CommandReason
	ExitCode  *int
	TimedOut  bool
	Cancelled bool
	Signaled  bool
	Redacted  bool
}

// AttemptHistory reports whether operation-indexed history was durably stored.
type AttemptHistory struct {
	Persisted bool
}

// CommandResult is the presentation-safe result of planning or execution.
type CommandResult struct {
	Mode           Mode
	ManifestPath   string
	LockfilePath   string
	StatefilePath  string
	Selection      Selection
	Route          Route
	Disclosure     Disclosure
	ResultClass    ResultClass
	ReasonCode     ReasonCode
	Attempted      bool
	ProcessOutcome *ProcessOutcome
	Observation    *Observation
	AttemptHistory AttemptHistory
	Remediation    []string
}

func (result CommandResult) HasErrors() bool {
	switch result.ResultClass {
	case ResultRefused, ResultCancelled, ResultFailed, ResultPartial:
		return true
	default:
		return false
	}
}

// ObservationRequest contains canonical facts needed by a passive relation
// observer. It grants no mutation or ownership authority.
type ObservationRequest struct {
	Paths        daempaths.Paths
	Lockfile     lock.File
	CurrentState durable.Snapshot
	Subject      topology.SubjectID
	Target       target.Target
	Scope        target.Scope
}

// RelationObservation contains one exact correlation and every passive host
// inventory path consumed to produce it.
type RelationObservation struct {
	Result         observerelation.CorrelationResult
	Present        bool
	AuthorityPaths []observerelation.AuthorityPath
}

// RelationObserver returns current evidence for one exact selected subject.
type RelationObserver func(context.Context, ObservationRequest) (RelationObservation, error)

// CommandBuildInput contains one exact locked operation and selected root.
type CommandBuildInput struct {
	Contract  lock.LockedSubjectContract
	Operation lock.OperationKind
	WorkDir   string
}

// CommandBuilder lowers one exact locked operation to a command plus its
// adapter-owned disclosure.
type CommandBuilder func(CommandBuildInput) (CommandSpec, error)

// CommandSpec is a validated command request plus complete effect disclosure.
type CommandSpec struct {
	attempt    subprocess.CommandAttemptRequest
	disclosure executehostroute.Disclosure
}

// NewCommandSpec validates one secret-free command and disclosure pair.
func NewCommandSpec(
	attempt subprocess.CommandAttemptRequest,
	disclosure executehostroute.Disclosure,
) (CommandSpec, error) {
	if strings.TrimSpace(attempt.Command) == "" || strings.TrimSpace(attempt.Command) != attempt.Command {
		return CommandSpec{}, fmt.Errorf("refresh command is required and must be trimmed")
	}
	if strings.TrimSpace(attempt.WorkDir) == "" || strings.TrimSpace(attempt.WorkDir) != attempt.WorkDir {
		return CommandSpec{}, fmt.Errorf("refresh command workdir is required and must be trimmed")
	}
	if attempt.Stdin != "" {
		return CommandSpec{}, fmt.Errorf("refresh command must not provide host stdin")
	}
	for index, argument := range attempt.Args {
		if strings.IndexFunc(argument, func(character rune) bool {
			return character == 0 || character == '\n' || character == '\r'
		}) >= 0 {
			return CommandSpec{}, fmt.Errorf("refresh command argument[%d] contains a forbidden control character", index)
		}
	}
	envNames := make([]string, 0, len(attempt.EnvRefs))
	for _, reference := range attempt.EnvRefs {
		name := reference.Name
		if name == "" {
			name = reference.SourceName
		}
		if strings.TrimSpace(name) == "" || strings.TrimSpace(name) != name {
			return CommandSpec{}, fmt.Errorf("refresh command environment name is invalid")
		}
		envNames = append(envNames, name)
	}
	slices.Sort(envNames)
	for index := 1; index < len(envNames); index++ {
		if envNames[index-1] == envNames[index] {
			return CommandSpec{}, fmt.Errorf("refresh command environment name %q is duplicated", envNames[index])
		}
	}
	if strings.TrimSpace(disclosure.ExecutionSubject()) == "" {
		return CommandSpec{}, fmt.Errorf("refresh command disclosure is required")
	}
	cloned := attempt
	cloned.Args = append([]string(nil), attempt.Args...)
	cloned.EnvRefs = append([]subprocess.CommandEnvRef(nil), attempt.EnvRefs...)
	return CommandSpec{attempt: cloned, disclosure: disclosure}, nil
}

func (spec CommandSpec) attemptRequest() subprocess.CommandAttemptRequest {
	attempt := spec.attempt
	attempt.Args = append([]string(nil), spec.attempt.Args...)
	attempt.EnvRefs = append([]subprocess.CommandEnvRef(nil), spec.attempt.EnvRefs...)
	return attempt
}

func commandResultDisclosure(
	spec CommandSpec,
	timeout HostCommandTimeout,
) Disclosure {
	envNames := make([]string, 0, len(spec.attempt.EnvRefs))
	for _, reference := range spec.attempt.EnvRefs {
		name := reference.Name
		if name == "" {
			name = reference.SourceName
		}
		envNames = append(envNames, name)
	}
	slices.Sort(envNames)
	return Disclosure{
		Invocation: Invocation{
			Kind:           spec.disclosure.InvocationKind(),
			Command:        spec.attempt.Command,
			Args:           append([]string(nil), spec.attempt.Args...),
			EnvNames:       envNames,
			CWDPolicy:      spec.disclosure.CWDPolicy(),
			TimeoutSeconds: timeout.Seconds(),
		},
		EffectClasses:         spec.disclosure.EffectClasses(),
		RetainedEffectClasses: spec.disclosure.RetainedEffectClasses(),
		NonClaims:             spec.disclosure.NonClaims(),
	}
}

type plan struct {
	result            CommandResult
	paths             daempaths.Paths
	lockfile          lock.File
	subject           topology.SubjectID
	contract          lock.LockedSubjectContract
	routeRequest      realizationdelegate.Request
	operationContract lock.OperationContract
	command           CommandSpec
	preObservation    *observerelation.CorrelationResult
	authorityPaths    []observerelation.AuthorityPath
	currentState      durable.Snapshot
	timeout           HostCommandTimeout
	fingerprint       mutation.OperationFingerprint
	authority         authorityEvidence
	revisions         mutation.RevisionSet
}
