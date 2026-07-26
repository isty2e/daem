package reconcile

import (
	"fmt"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func classify(
	state observerelation.CorrelationState,
	admission RelationRouteAdmissionDecision,
	pendingInstall bool,
	managedClaim bool,
	evidenceClass extensiontopology.RelationEvidenceClass,
) (RelationActionKind, RelationExecutionClass, RelationReasonCode, error) {
	switch state {
	case observerelation.StateExactCorrelation:
		if evidenceClass != extensiontopology.RelationEvidenceSourceExact {
			return "", "", "", fmt.Errorf(
				"exact relation correlation is incompatible with evidence class %q",
				evidenceClass,
			)
		}
		if !pendingInstall && !managedClaim {
			return ActionBlock, ExecutionBlocked, ReasonPresentUnclaimed, nil
		}
		return ActionNoOp, ExecutionNoMutation, ReasonNone, nil
	case observerelation.StateMissing:
		return classifyMissing(admission)
	case observerelation.StateUnkeyedSameSubject:
		if evidenceClass == extensiontopology.RelationEvidenceBoundedSameSubject &&
			(pendingInstall || managedClaim) {
			return ActionNoOp, ExecutionNoMutation, ReasonNone, nil
		}
		return ActionBlock, ExecutionBlocked, ReasonUnkeyedSameSubject, nil
	case observerelation.StateSameSubjectShadow:
		return ActionBlock, ExecutionBlocked, ReasonSameSubjectShadow, nil
	case observerelation.StateManagedKeyDrift:
		return ActionBlock, ExecutionBlocked, ReasonManagedKeyDrift, nil
	case observerelation.StateAmbiguous:
		return ActionBlock, ExecutionBlocked, ReasonAmbiguousRelation, nil
	case observerelation.StateStaleEvidence:
		return ActionBlock, ExecutionBlocked, ReasonStaleEvidence, nil
	case observerelation.StateUnsupported:
		return classifyUnsupported(admission)
	case observerelation.StateUnavailableEvidence:
		return ActionObserveOnly, ExecutionObserveOnly, ReasonRelationEvidenceUnavailable, nil
	default:
		return "", "", "", fmt.Errorf("relation correlation state %q is unsupported", state)
	}
}

func classifyUnsupported(
	admission RelationRouteAdmissionDecision,
) (RelationActionKind, RelationExecutionClass, RelationReasonCode, error) {
	switch admission.ObservationPolicy() {
	case ObservationRequireCurrent:
		return ActionObserveOnly, ExecutionObserveOnly, ReasonUnsupportedPassiveInventory, nil
	case ObservationAttemptWhenUnsupported:
		if !admission.AllowsHostRouteInvocation() {
			return "", "", "", fmt.Errorf(
				"observation policy %q requires a host-route admission outcome",
				admission.ObservationPolicy(),
			)
		}
		return ActionAttempt, ExecutionHostRoute, ReasonUnsupportedPassiveInventory, nil
	default:
		return "", "", "", fmt.Errorf(
			"observation policy %q is unsupported",
			admission.ObservationPolicy(),
		)
	}
}

func classifyMissing(
	admission RelationRouteAdmissionDecision,
) (RelationActionKind, RelationExecutionClass, RelationReasonCode, error) {
	switch admission.SelectedOutcome() {
	case AdmissionOutcomeOrdinaryMutation, AdmissionOutcomeHostDelegated:
		return ActionCreate, ExecutionHostRoute, ReasonNone, nil
	case AdmissionOutcomeAssisted, AdmissionOutcomeExplicitAttempt:
		return ActionAssistCandidate, ExecutionAssisted, ReasonRouteRequiresAssistance, nil
	case AdmissionOutcomeObserveOnly:
		return ActionObserveOnly, ExecutionObserveOnly, ReasonUnsupportedPassiveInventory, nil
	case AdmissionOutcomeBlocked:
		return ActionCreate, ExecutionBlocked, ReasonRouteNotAdmitted, nil
	default:
		return "", "", "", fmt.Errorf(
			"selected outcome %q is unsupported",
			admission.SelectedOutcome(),
		)
	}
}
