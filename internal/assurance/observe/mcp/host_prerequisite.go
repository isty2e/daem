package mcp

import (
	"fmt"
	"strings"
)

// HostPrerequisiteState is host admission evidence, not configuration or runtime readiness.
type HostPrerequisiteState string

const (
	HostNotApplicable HostPrerequisiteState = "not_applicable"
	HostQualified     HostPrerequisiteState = "qualified"
	HostUnqualified   HostPrerequisiteState = "unqualified"

	ReasonHostVersionUnqualified ReasonCode = "HOST_VERSION_UNQUALIFIED"
	ReasonHostSettingsUnobserved ReasonCode = "HOST_SETTINGS_UNOBSERVED"
	ReasonHostBuiltinDisabled    ReasonCode = "HOST_BUILTIN_DISABLED"
	ReasonHostBuiltinUnobserved  ReasonCode = "HOST_BUILTIN_UNOBSERVED"
	ReasonHostAdapterConfigured  ReasonCode = "HOST_ADAPTER_CONFIGURED"
)

type HostPrerequisiteObservationInput struct {
	State  HostPrerequisiteState
	Reason ReasonCode
	Detail string
}

type HostPrerequisiteObservation struct {
	state  HostPrerequisiteState
	reason ReasonCode
	detail string
}

func NewHostPrerequisiteObservation(input HostPrerequisiteObservationInput) (HostPrerequisiteObservation, error) {
	switch input.State {
	case HostNotApplicable, HostQualified:
		if input.Reason != ReasonNone || input.Detail != "" {
			return HostPrerequisiteObservation{}, fmt.Errorf("non-failing host prerequisite cannot carry failure evidence")
		}
	case HostUnqualified:
		switch input.Reason {
		case ReasonHostVersionUnqualified, ReasonHostSettingsUnobserved, ReasonHostBuiltinDisabled, ReasonHostBuiltinUnobserved, ReasonHostAdapterConfigured:
		default:
			return HostPrerequisiteObservation{}, fmt.Errorf("unsupported host prerequisite reason %q", input.Reason)
		}
		if input.Detail == "" || len(input.Detail) > 2048 || strings.TrimSpace(input.Detail) != input.Detail {
			return HostPrerequisiteObservation{}, fmt.Errorf("unqualified host prerequisite requires bounded causal detail")
		}
	default:
		return HostPrerequisiteObservation{}, fmt.Errorf("unsupported host prerequisite state %q", input.State)
	}
	return HostPrerequisiteObservation{state: input.State, reason: input.Reason, detail: input.Detail}, nil
}

func (observation HostPrerequisiteObservation) State() HostPrerequisiteState {
	return observation.state
}
func (observation HostPrerequisiteObservation) Reason() ReasonCode { return observation.reason }
func (observation HostPrerequisiteObservation) Detail() string     { return observation.detail }
