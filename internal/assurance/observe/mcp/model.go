package mcp

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/topology"
)

// ReasonCode is a stable first-slice MCP observation reason.
type ReasonCode string

const (
	ReasonNone                           ReasonCode = ""
	ReasonSecretLiteralForbidden         ReasonCode = "SECRET_LITERAL_FORBIDDEN"
	ReasonConfigShadowed                 ReasonCode = "CONFIG_SHADOWED"
	ReasonRoutePreexistingUnowned        ReasonCode = "ROUTE_PREEXISTING_UNOWNED"
	ReasonOwnershipStateUnobserved       ReasonCode = "OWNERSHIP_STATE_UNOBSERVED"
	ReasonEffectiveStateUnobserved       ReasonCode = "EFFECTIVE_STATE_UNOBSERVED"
	ReasonProjectionEquivalenceUndefined ReasonCode = "PROJECTION_EQUIVALENCE_UNDEFINED"
	ReasonConfigMalformed                ReasonCode = "CONFIG_MALFORMED"
	ReasonUnsupportedTransport           ReasonCode = "UNSUPPORTED_TRANSPORT"
	ReasonUnsupportedManagedField        ReasonCode = "UNSUPPORTED_MANAGED_FIELD"
	ReasonUnsupportedAlternateConfig     ReasonCode = "UNSUPPORTED_ALTERNATE_CONFIG"
	ReasonStaleAdapterContract           ReasonCode = "STALE_ADAPTER_CONTRACT"
	ReasonRemovalEffectUnknown           ReasonCode = "REMOVAL_EFFECT_UNKNOWN"
	ReasonLastDelegateAttemptUnobserved  ReasonCode = "LAST_DELEGATE_ATTEMPT_UNOBSERVED"
	ReasonLastDelegateAttemptStale       ReasonCode = "LAST_DELEGATE_ATTEMPT_STALE"
	ReasonDelegatePolicyBlocked          ReasonCode = "DELEGATE_POLICY_BLOCKED"
	ReasonDelegateMissingEnvRef          ReasonCode = "DELEGATE_MISSING_ENV_REF"
	ReasonDelegateMissingRunner          ReasonCode = "DELEGATE_MISSING_RUNNER"
	ReasonDelegateNonZeroExit            ReasonCode = "DELEGATE_NONZERO_EXIT"
	ReasonDelegateTimeout                ReasonCode = "DELEGATE_TIMEOUT"
	ReasonDelegateRunnerError            ReasonCode = "DELEGATE_RUNNER_ERROR"
	ReasonDelegateWorkDirAuthority       ReasonCode = "DELEGATE_WORKDIR_AUTHORITY"
	ReasonProviderRelationInstall        ReasonCode = "PROVIDER_RELATION_INSTALL_REQUIRED"
	ReasonProviderPackageAbsent          ReasonCode = "PROVIDER_PACKAGE_ABSENT"
	ReasonProviderVersionUnobserved      ReasonCode = "PROVIDER_VERSION_UNOBSERVED"
	ReasonProviderVersionIncompatible    ReasonCode = "PROVIDER_VERSION_INCOMPATIBLE"
	ReasonProviderCodecMismatch          ReasonCode = "PROVIDER_CODEC_MISMATCH"
)

// ProjectionState classifies the passive project .mcp.json projection state.
type ProjectionState string

const (
	ProjectionMissing           ProjectionState = "missing"
	ProjectionProjected         ProjectionState = "projected"
	ProjectionDrifted           ProjectionState = "drifted"
	ProjectionMalformed         ProjectionState = "malformed"
	ProjectionUnsupported       ProjectionState = "unsupported"
	ProjectionUnmanagedSameName ProjectionState = "unmanaged_same_name"
)

// OwnershipState records whether a same-name project entry is known to be owned.
type OwnershipState string

const (
	OwnershipManaged           OwnershipState = "managed"
	OwnershipUnmanagedSameName OwnershipState = "unmanaged_same_name"
	OwnershipAdopted           OwnershipState = "adopted"
	OwnershipUnknown           OwnershipState = "unknown"
)

// ShadowState records passive evidence that another relation may affect the configured entry.
type ShadowState string

const (
	ShadowUnshadowed                  ShadowState = "unshadowed"
	ShadowShadowedByLocal             ShadowState = "shadowed_by_local"
	ShadowLowerPrecedenceUserConflict ShadowState = "lower_precedence_user_conflict"
	ShadowCarrierCollision            ShadowState = "carrier_collision"
	ShadowUnknown                     ShadowState = "unknown"
)

// DelegateAttemptState records last attempt diagnostics without implying runtime readiness.
type DelegateAttemptState string

const (
	DelegateAttemptNotObserved DelegateAttemptState = "not_observed"
	DelegateAttemptStale       DelegateAttemptState = "stale"
	DelegateAttemptSucceeded   DelegateAttemptState = "succeeded"
	DelegateAttemptFailed      DelegateAttemptState = "failed"
	DelegateAttemptBlocked     DelegateAttemptState = "blocked"
)

// ResidueState records adoption/orphan residue visibility for this slice.
type ResidueState string

const (
	ResidueNotApplicable      ResidueState = "not_applicable"
	ResidueAdoptableUnmanaged ResidueState = "adoptable_unmanaged"
	ResidueManagedEntryAbsent ResidueState = "managed_entry_absent"
	ResidueUnobserved         ResidueState = "residue_unobserved"
	ResidueDeferred           ResidueState = "deferred"
)

// ProviderPrerequisiteState records current provider-package readiness
// independently from managed config and runtime readiness.
type ProviderPrerequisiteState string

const (
	ProviderNotApplicable  ProviderPrerequisiteState = "not_applicable"
	ProviderCurrent        ProviderPrerequisiteState = "current"
	ProviderInstallPending ProviderPrerequisiteState = "install_required"
	ProviderBlocked        ProviderPrerequisiteState = "blocked"
)

// ProviderPrerequisiteObservationInput contains one already-normalized
// provider prerequisite classification.
type ProviderPrerequisiteObservationInput struct {
	State   ProviderPrerequisiteState
	Reason  ReasonCode
	Version string
}

// ProviderPrerequisiteObservation is current provider-package evidence. An
// exact version is reported only when it was freshly observed.
type ProviderPrerequisiteObservation struct {
	state   ProviderPrerequisiteState
	reason  ReasonCode
	version string
}

// NewProviderPrerequisiteObservation validates provider evidence without
// importing provider-planning policy into the observation model.
func NewProviderPrerequisiteObservation(
	input ProviderPrerequisiteObservationInput,
) (ProviderPrerequisiteObservation, error) {
	switch input.State {
	case ProviderNotApplicable:
		if input.Reason != ReasonNone || input.Version != "" {
			return ProviderPrerequisiteObservation{}, fmt.Errorf(
				"non-provider MCP projection cannot carry provider evidence",
			)
		}
	case ProviderCurrent:
		if input.Reason != ReasonNone || input.Version == "" {
			return ProviderPrerequisiteObservation{}, fmt.Errorf(
				"current MCP provider requires only an exact version",
			)
		}
	case ProviderInstallPending:
		if input.Reason != ReasonProviderRelationInstall &&
			input.Reason != ReasonProviderPackageAbsent {
			return ProviderPrerequisiteObservation{}, fmt.Errorf(
				"pending MCP provider has unsupported reason %q",
				input.Reason,
			)
		}
		if input.Version != "" {
			return ProviderPrerequisiteObservation{}, fmt.Errorf(
				"pending MCP provider cannot carry a version",
			)
		}
	case ProviderBlocked:
		if input.Reason != ReasonProviderVersionUnobserved &&
			input.Reason != ReasonProviderVersionIncompatible &&
			input.Reason != ReasonProviderCodecMismatch {
			return ProviderPrerequisiteObservation{}, fmt.Errorf(
				"blocked MCP provider has unsupported reason %q",
				input.Reason,
			)
		}
		if input.Reason == ReasonProviderVersionUnobserved && input.Version != "" {
			return ProviderPrerequisiteObservation{}, fmt.Errorf(
				"unobserved MCP provider cannot carry a version",
			)
		}
		if input.Reason != ReasonProviderVersionUnobserved && input.Version == "" {
			return ProviderPrerequisiteObservation{}, fmt.Errorf(
				"incompatible MCP provider requires its exact observed version",
			)
		}
	default:
		return ProviderPrerequisiteObservation{}, fmt.Errorf(
			"MCP provider prerequisite state %q is unsupported",
			input.State,
		)
	}
	return ProviderPrerequisiteObservation{
		state:   input.State,
		reason:  input.Reason,
		version: input.Version,
	}, nil
}

func (observation ProviderPrerequisiteObservation) State() ProviderPrerequisiteState {
	return observation.state
}

func (observation ProviderPrerequisiteObservation) Reason() ReasonCode {
	return observation.reason
}

func (observation ProviderPrerequisiteObservation) Version() string {
	return observation.version
}

// AggregateProjectionObservationInput classifies one MCP status row from the
// generic aggregate observer without rereading or reparsing host config.
type AggregateProjectionObservationInput struct {
	Contract                   lock.LockedSubjectContract
	Projection                 aggregate.ProjectionState
	Observed                   bool
	FailureReason              aggregate.CodecFailureReason
	UnsupportedAlternateConfig bool
	Ownership                  OwnershipState
	Shadowing                  ShadowState
}

// AggregateProjectionObservation is current passive evidence for one locked
// MCP aggregate projection. It carries no history, runtime, residue, or
// presentation state.
type AggregateProjectionObservation struct {
	Subject    topology.SubjectID
	Projection ProjectionObservation
	Ownership  OwnershipObservation
	Shadowing  ShadowObservation
}

// ProjectionObservation reports configured project .mcp.json state only.
type ProjectionObservation struct {
	State       ProjectionState
	Reason      ReasonCode
	ConfigPath  output.Destination
	ContentPath string
	Present     bool
	Equivalent  bool
}

// OwnershipObservation reports same-scope ownership evidence separately from projection equality.
type OwnershipObservation struct {
	State  OwnershipState
	Reason ReasonCode
}

// ShadowObservation reports passive effective-conflict evidence separately from configured projection.
type ShadowObservation struct {
	State  ShadowState
	Reason ReasonCode
}

// LastDelegateAttemptInput is an already-sanitized historical attempt fact read from the statefile.
type LastDelegateAttemptInput struct {
	Observed            bool
	MatchesPlanIdentity bool
	Status              DelegateAttemptState
	Reason              ReasonCode
}

// LastDelegateAttemptObservation reports attempt diagnostics separately from projection.
type LastDelegateAttemptObservation struct {
	State  DelegateAttemptState
	Reason ReasonCode
}

// ResidueObservation reports orphan/adoption cleanup separately from projection state.
type ResidueObservation struct {
	State  ResidueState
	Reason ReasonCode
}
