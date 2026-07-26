package hostroute

import (
	"fmt"

	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/topology"
)

// CurrentVerificationInput contains current passive evidence for one exact
// route postcondition. It deliberately contains no command-attempt fact.
type CurrentVerificationInput struct {
	Subject               topology.SubjectID
	RouteRequest          realizationdelegate.Request
	Observation           ObservationFact
	RequiredPostcondition PostconditionRequirement
}

// CurrentVerification is an observation-only postcondition result. It may
// settle an already-pending effect but never proves that a command ran.
type CurrentVerification struct {
	satisfied            bool
	reason               ResultReasonCode
	observation          observerelation.ObservationSummary
	postcondition        observerelation.PostconditionSummary
	effectPostconditions observepostcondition.Assessment
}

// VerifyCurrentPostconditions evaluates fresh current relation and effect
// evidence without manufacturing a host-route attempt.
func VerifyCurrentPostconditions(
	input CurrentVerificationInput,
) (CurrentVerification, error) {
	if err := input.Subject.Validate(); err != nil {
		return CurrentVerification{}, fmt.Errorf(
			"%s: host route subject identity: %w",
			ResultReasonUnsupportedObservation,
			err,
		)
	}
	if err := input.RouteRequest.Validate(); err != nil {
		return CurrentVerification{}, fmt.Errorf(
			"%s: host route request identity: %w",
			ResultReasonUnsupportedObservation,
			err,
		)
	}
	if err := input.RequiredPostcondition.validate(); err != nil {
		return CurrentVerification{}, fmt.Errorf(
			"%s: %w",
			ResultReasonUnsupportedObservation,
			err,
		)
	}
	if input.Observation.observed && input.Observation.result.State() == "" {
		return CurrentVerification{}, fmt.Errorf(
			"%s: current relation observation state is required",
			ResultReasonUnsupportedObservation,
		)
	}
	effects, err := assessEffectPostconditions(
		input.Subject,
		input.RouteRequest,
		input.Observation,
		input.RequiredPostcondition,
	)
	if err != nil {
		return CurrentVerification{}, err
	}
	relation := input.RequiredPostcondition.relationPostcondition()
	verification := CurrentVerification{
		observation:          observationSummary(input.Observation, relation),
		postcondition:        postconditionSummary(input.Observation, relation),
		effectPostconditions: effects,
	}
	verification.satisfied = verification.postcondition == observerelation.PostconditionObserved &&
		effects.Satisfied()
	verification.reason = currentVerificationReason(
		input.Observation,
		relation,
		effects,
	)
	return verification, nil
}

// Satisfied reports whether the exact current relation and every coupled
// effect postcondition are freshly satisfied.
func (verification CurrentVerification) Satisfied() bool {
	return verification.satisfied
}

// Reason returns the bounded explanation for a non-satisfied observation.
func (verification CurrentVerification) Reason() ResultReasonCode {
	return verification.reason
}

// ObservationSummary returns the history-safe current observation class.
func (verification CurrentVerification) ObservationSummary() observerelation.ObservationSummary {
	return verification.observation
}

// PostconditionSummary returns the history-safe current relation result.
func (verification CurrentVerification) PostconditionSummary() observerelation.PostconditionSummary {
	return verification.postcondition
}

// EffectPostconditionAssessment returns the bounded coupled-effect result.
func (verification CurrentVerification) EffectPostconditionAssessment() observepostcondition.Assessment {
	return verification.effectPostconditions
}

func assessEffectPostconditions(
	subject topology.SubjectID,
	routeRequest realizationdelegate.Request,
	observation ObservationFact,
	required PostconditionRequirement,
) (observepostcondition.Assessment, error) {
	effects, err := observepostcondition.Assess(observepostcondition.AssessmentInput{
		Subject:         subject,
		RouteRequest:    routeRequest,
		Requirements:    required.effectPostconditions(),
		Evidence:        observation.effectEvidence,
		EvidencePresent: observation.effectEvidencePresent,
	})
	if err != nil {
		return observepostcondition.Assessment{}, fmt.Errorf(
			"%s: %w",
			ResultReasonUnsupportedObservation,
			err,
		)
	}
	return effects, nil
}

func currentVerificationReason(
	observation ObservationFact,
	required RelationPostcondition,
	effects observepostcondition.Assessment,
) ResultReasonCode {
	if !observation.observed {
		if observation.reason != "" {
			return observation.reason
		}
		return ResultReasonObservationUnavailable
	}
	if required.Accepts(observation.result.State()) {
		if !effects.Satisfied() {
			return effectAssessmentReason(effects.Reason())
		}
		_, reason, _ := required.satisfiedResult()
		return reason
	}
	switch observation.result.State() {
	case observerelation.StateExactCorrelation:
		return ResultReasonObservedPresent
	case observerelation.StateMissing:
		return ResultReasonObservedAbsent
	case observerelation.StateUnkeyedSameSubject:
		return ResultReasonUnkeyedSameSubject
	case observerelation.StateSameSubjectShadow:
		return ResultReasonSameSubjectShadow
	case observerelation.StateManagedKeyDrift:
		return ResultReasonManagedKeyDrift
	case observerelation.StateAmbiguous:
		return ResultReasonAmbiguousRelation
	case observerelation.StateStaleEvidence:
		return ResultReasonObservationStale
	case observerelation.StateUnsupported:
		return ResultReasonObservationUnsupported
	case observerelation.StateUnavailableEvidence:
		return ResultReasonObservationUnavailable
	default:
		return ResultReasonUnsupportedObservation
	}
}
