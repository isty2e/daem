package runtimeprobe

import "fmt"

// FoldFacts folds already-sanitized runtime probe facts into one canonical
// operation-local observation. Missing dimensions remain not_probed.
func FoldFacts(facts []Fact) (Observation, error) {
	observation := notProbedObservation()
	seen := make(map[Dimension]struct{}, len(facts))
	for _, fact := range facts {
		if err := validateFact(fact); err != nil {
			return Observation{}, err
		}
		if _, exists := seen[fact.Dimension]; exists {
			return Observation{}, fmt.Errorf("duplicate runtime readiness fact for %s", fact.Dimension)
		}
		seen[fact.Dimension] = struct{}{}

		readiness := ReadinessObservation{
			state:           fact.State,
			reason:          fact.Reason,
			source:          fact.Source,
			freshness:       fact.Freshness,
			sanitizedDetail: fact.SanitizedDetail,
		}
		switch fact.Dimension {
		case DimensionLauncher:
			observation.launcher = readiness
		case DimensionProtocolInitialize:
			observation.protocolInitialize = readiness
		case DimensionAuthentication:
			observation.authentication = readiness
		case DimensionEndpointHealth:
			observation.endpointHealth = readiness
		case DimensionToolInventory:
			observation.toolInventory = readiness
		}
	}
	return observation, nil
}

func notProbedObservation() Observation {
	notProbed := notProbedReadiness()
	return Observation{
		launcher:           notProbed,
		protocolInitialize: notProbed,
		authentication:     notProbed,
		endpointHealth:     notProbed,
		toolInventory:      notProbed,
	}
}

func notProbedReadiness() ReadinessObservation {
	return ReadinessObservation{
		state:  NotProbed,
		reason: ReasonNotProbed,
	}
}

func validateFact(fact Fact) error {
	if err := validateDimension(fact.Dimension); err != nil {
		return err
	}
	if err := validateReadiness(fact.State); err != nil {
		return err
	}
	if err := validateSource(fact.Source); err != nil {
		return err
	}
	if err := validateFreshness(fact.Freshness); err != nil {
		return err
	}
	if fact.State == NotProbed {
		return fmt.Errorf("runtime probe fact for %s must not use %q; omit the fact to report not_probed", fact.Dimension, fact.State)
	}
	if err := validateReason(fact.State, fact.Reason); err != nil {
		return fmt.Errorf("runtime probe fact for %s: %w", fact.Dimension, err)
	}
	if fact.State == ObservedOK && fact.SanitizedDetail != "" {
		return fmt.Errorf("runtime probe fact for %s state %q must not carry sanitized detail", fact.Dimension, fact.State)
	}
	if fact.State == Stale && fact.Freshness != FreshnessStale {
		return fmt.Errorf("runtime probe fact for %s is stale but freshness is %q", fact.Dimension, fact.Freshness)
	}
	if fact.State != Stale && fact.Freshness == FreshnessStale {
		return fmt.Errorf("runtime probe fact for %s has stale freshness with non-stale state %q", fact.Dimension, fact.State)
	}
	return nil
}

func validateReason(state Readiness, reason ReasonCode) error {
	switch state {
	case ObservedOK:
		if reason != ReasonNone {
			return fmt.Errorf("state %q requires empty reason, got %q", state, reason)
		}
	case NotApplicable:
		if reason != ReasonNotApplicable {
			return fmt.Errorf("state %q requires reason %q, got %q", state, ReasonNotApplicable, reason)
		}
	case Unsupported:
		if reason != ReasonUnsupported {
			return fmt.Errorf("state %q requires reason %q, got %q", state, ReasonUnsupported, reason)
		}
	case ObservedFailed:
		if reason != ReasonObservedFailed {
			return fmt.Errorf("state %q requires reason %q, got %q", state, ReasonObservedFailed, reason)
		}
	case Blocked:
		if reason != ReasonBlocked {
			return fmt.Errorf("state %q requires reason %q, got %q", state, ReasonBlocked, reason)
		}
	case Stale:
		if reason != ReasonStale {
			return fmt.Errorf("state %q requires reason %q, got %q", state, ReasonStale, reason)
		}
	}
	return nil
}

func validateDimension(dimension Dimension) error {
	switch dimension {
	case DimensionLauncher,
		DimensionProtocolInitialize,
		DimensionAuthentication,
		DimensionEndpointHealth,
		DimensionToolInventory:
		return nil
	default:
		return fmt.Errorf("unsupported runtime readiness dimension %q", dimension)
	}
}

func validateReadiness(state Readiness) error {
	switch state {
	case NotProbed,
		NotApplicable,
		Unsupported,
		ObservedOK,
		ObservedFailed,
		Blocked,
		Stale:
		return nil
	default:
		return fmt.Errorf("unsupported runtime readiness state %q", state)
	}
}

func validateSource(source Source) error {
	switch source {
	case SourceExplicit, SourceAssisted:
		return nil
	default:
		return fmt.Errorf("unsupported runtime probe source %q", source)
	}
}

func validateFreshness(freshness Freshness) error {
	switch freshness {
	case FreshnessCurrent, FreshnessStale:
		return nil
	default:
		return fmt.Errorf("unsupported runtime freshness %q", freshness)
	}
}
