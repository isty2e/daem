package lock

import (
	"fmt"
	"strings"
)

// ReplayClass describes whether a replay axis is covered by a lock.
type ReplayClass string

const (
	ReplayExact         ReplayClass = "exact"
	ReplayPartial       ReplayClass = "partial"
	ReplayUnavailable   ReplayClass = "unavailable"
	ReplayNotApplicable ReplayClass = "not_applicable"
)

// ReplayExclusionReason is a stable reason for replay closure gaps.
type ReplayExclusionReason string

const (
	ReplayExclusionHostSelectedArtifact ReplayExclusionReason = "HOST_SELECTED_ARTIFACT"
	ReplayExclusionRuntimeDependency    ReplayExclusionReason = "RUNTIME_DEPENDENCY"
	ReplayExclusionOAuthSession         ReplayExclusionReason = "OAUTH_SESSION"
	ReplayExclusionHostMarketplace      ReplayExclusionReason = "HOST_MARKETPLACE"
	ReplayExclusionHostSource           ReplayExclusionReason = "HOST_SOURCE"
	ReplayExclusionHostApproval         ReplayExclusionReason = "HOST_APPROVAL"
	ReplayExclusionRuntimeReadiness     ReplayExclusionReason = "RUNTIME_READINESS"
	ReplayExclusionToolInventory        ReplayExclusionReason = "TOOL_INVENTORY"
	ReplayExclusionPluginCarrier        ReplayExclusionReason = "PLUGIN_CARRIER"
)

// ReplayExclusion names one component outside the lock replay closure.
type ReplayExclusion struct {
	Component string
	Reason    ReplayExclusionReason
}

// ReplayCoverage records invocation, outcome, and derivation replay independently.
type ReplayCoverage struct {
	invocation ReplayClass
	outcome    ReplayClass
	derivation ReplayClass
	exclusions []ReplayExclusion
}

// NewReplayCoverage constructs replay coverage without collapsing it to a global reproducibility flag.
func NewReplayCoverage(
	invocation ReplayClass,
	outcome ReplayClass,
	derivation ReplayClass,
	exclusions []ReplayExclusion,
) (ReplayCoverage, error) {
	coverage := ReplayCoverage{
		invocation: invocation,
		outcome:    outcome,
		derivation: derivation,
		exclusions: cloneReplayExclusions(exclusions),
	}
	if err := coverage.validate(); err != nil {
		return ReplayCoverage{}, err
	}
	return coverage, nil
}

// Invocation returns invocation replay coverage.
func (coverage ReplayCoverage) Invocation() ReplayClass {
	return coverage.invocation
}

// Outcome returns outcome replay coverage.
func (coverage ReplayCoverage) Outcome() ReplayClass {
	return coverage.outcome
}

// Derivation returns derivation replay coverage.
func (coverage ReplayCoverage) Derivation() ReplayClass {
	return coverage.derivation
}

// Exclusions returns replay closure gaps.
func (coverage ReplayCoverage) Exclusions() []ReplayExclusion {
	return cloneReplayExclusions(coverage.exclusions)
}

// OutcomeReplayable reports whether the concrete outcome identity is replayable.
func (coverage ReplayCoverage) OutcomeReplayable() bool {
	return coverage.outcome == ReplayExact
}

func (coverage ReplayCoverage) validate() error {
	if err := validateReplayClass("invocation replay", coverage.invocation); err != nil {
		return err
	}
	if err := validateReplayClass("outcome replay", coverage.outcome); err != nil {
		return err
	}
	if err := validateReplayClass("derivation replay", coverage.derivation); err != nil {
		return err
	}
	for _, exclusion := range coverage.exclusions {
		if strings.TrimSpace(exclusion.Component) == "" {
			return fmt.Errorf("replay exclusion component is required")
		}
		if err := validateReplayExclusionReason(exclusion.Reason); err != nil {
			return err
		}
	}
	return nil
}

func validateReplayClass(context string, class ReplayClass) error {
	switch class {
	case ReplayExact, ReplayPartial, ReplayUnavailable, ReplayNotApplicable:
		return nil
	default:
		return fmt.Errorf("%s class %q is unsupported", context, class)
	}
}

func validateReplayExclusionReason(reason ReplayExclusionReason) error {
	switch reason {
	case ReplayExclusionHostSelectedArtifact,
		ReplayExclusionRuntimeDependency,
		ReplayExclusionOAuthSession,
		ReplayExclusionHostMarketplace,
		ReplayExclusionHostSource,
		ReplayExclusionHostApproval,
		ReplayExclusionRuntimeReadiness,
		ReplayExclusionToolInventory,
		ReplayExclusionPluginCarrier:
		return nil
	default:
		return fmt.Errorf("replay exclusion reason %q is unsupported", reason)
	}
}

func cloneReplayExclusions(exclusions []ReplayExclusion) []ReplayExclusion {
	if len(exclusions) == 0 {
		return nil
	}
	cloned := make([]ReplayExclusion, 0, len(exclusions))
	for _, exclusion := range exclusions {
		cloned = append(cloned, ReplayExclusion{
			Component: strings.TrimSpace(exclusion.Component),
			Reason:    exclusion.Reason,
		})
	}
	return cloned
}

func cloneReplayCoverage(coverage ReplayCoverage) ReplayCoverage {
	return ReplayCoverage{
		invocation: coverage.invocation,
		outcome:    coverage.outcome,
		derivation: coverage.derivation,
		exclusions: cloneReplayExclusions(coverage.exclusions),
	}
}
