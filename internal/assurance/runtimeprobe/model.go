// Package runtimeprobe models operation-local active runtime evidence.
package runtimeprobe

// ReasonCode is a stable runtime-probe reason.
type ReasonCode string

const (
	ReasonNone           ReasonCode = ""
	ReasonNotProbed      ReasonCode = "RUNTIME_NOT_PROBED"
	ReasonNotApplicable  ReasonCode = "RUNTIME_NOT_APPLICABLE"
	ReasonUnsupported    ReasonCode = "RUNTIME_UNSUPPORTED"
	ReasonObservedFailed ReasonCode = "RUNTIME_OBSERVED_FAILED"
	ReasonBlocked        ReasonCode = "RUNTIME_PROBE_BLOCKED"
	ReasonStale          ReasonCode = "RUNTIME_PROBE_STALE"
)

// Readiness records one runtime-readiness observation state.
type Readiness string

const (
	NotProbed      Readiness = "not_probed"
	NotApplicable  Readiness = "not_applicable"
	Unsupported    Readiness = "unsupported"
	ObservedOK     Readiness = "observed_ok"
	ObservedFailed Readiness = "observed_failed"
	Blocked        Readiness = "blocked"
	Stale          Readiness = "stale"
)

// Dimension identifies one runtime-readiness evidence axis.
type Dimension string

const (
	DimensionLauncher           Dimension = "runtime_launcher"
	DimensionProtocolInitialize Dimension = "protocol_initialize"
	DimensionAuthentication     Dimension = "runtime_authentication"
	DimensionEndpointHealth     Dimension = "endpoint_health"
	DimensionToolInventory      Dimension = "tool_inventory"
)

// Source records how a runtime-readiness fact was obtained.
type Source string

const (
	SourceExplicit Source = "explicit_probe"
	SourceAssisted Source = "assisted_evidence"
)

// Freshness records whether a fact matches the current probe identity.
type Freshness string

const (
	FreshnessCurrent Freshness = "current"
	FreshnessStale   Freshness = "stale"
)

// Observation reports runtime readiness axes without performing a probe.
type Observation struct {
	launcher           ReadinessObservation
	protocolInitialize ReadinessObservation
	authentication     ReadinessObservation
	endpointHealth     ReadinessObservation
	toolInventory      ReadinessObservation
}

// ReadinessObservation reports one runtime-readiness axis.
type ReadinessObservation struct {
	state           Readiness
	reason          ReasonCode
	source          Source
	freshness       Freshness
	sanitizedDetail string
}

// Fact is an already-sanitized active or assisted runtime-readiness fact.
// It is pre-normalized boundary input until FoldFacts constructs an Observation.
type Fact struct {
	Dimension       Dimension
	State           Readiness
	Reason          ReasonCode
	Source          Source
	Freshness       Freshness
	SanitizedDetail string
}

// Launcher returns launcher readiness. A zero Observation reports not_probed.
func (observation Observation) Launcher() ReadinessObservation {
	return normalizedReadiness(observation.launcher)
}

// ProtocolInitialize returns protocol initialize readiness. A zero Observation reports not_probed.
func (observation Observation) ProtocolInitialize() ReadinessObservation {
	return normalizedReadiness(observation.protocolInitialize)
}

// Authentication returns runtime authentication readiness. A zero Observation reports not_probed.
func (observation Observation) Authentication() ReadinessObservation {
	return normalizedReadiness(observation.authentication)
}

// EndpointHealth returns endpoint health readiness. A zero Observation reports not_probed.
func (observation Observation) EndpointHealth() ReadinessObservation {
	return normalizedReadiness(observation.endpointHealth)
}

// ToolInventory returns tool inventory readiness. A zero Observation reports not_probed.
func (observation Observation) ToolInventory() ReadinessObservation {
	return normalizedReadiness(observation.toolInventory)
}

// State returns the closed readiness state.
func (observation ReadinessObservation) State() Readiness {
	return normalizedReadiness(observation).state
}

// Reason returns the reason required by the readiness state.
func (observation ReadinessObservation) Reason() ReasonCode {
	return normalizedReadiness(observation).reason
}

// Source returns how the runtime fact was obtained.
func (observation ReadinessObservation) Source() Source {
	return observation.source
}

// Freshness returns whether the runtime fact matches the current probe identity.
func (observation ReadinessObservation) Freshness() Freshness {
	return observation.freshness
}

// SanitizedDetail returns bounded, redacted diagnostic detail.
func (observation ReadinessObservation) SanitizedDetail() string {
	return observation.sanitizedDetail
}

// IsFailure reports whether the dimension should make the explicit probe fail.
func (observation ReadinessObservation) IsFailure() bool {
	switch observation.State() {
	case ObservedOK, NotProbed, NotApplicable:
		return false
	default:
		return true
	}
}

func normalizedReadiness(observation ReadinessObservation) ReadinessObservation {
	if observation.state != "" {
		return observation
	}
	return notProbedReadiness()
}
