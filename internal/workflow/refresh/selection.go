package refresh

import (
	"fmt"
	"strings"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
)

func observationRefusal(
	state observerelation.CorrelationState,
) (ReasonCode, string) {
	switch state {
	case observerelation.StateMissing:
		return ReasonRelationMissing,
			"run daem apply to install the missing relation before refreshing"
	case observerelation.StateUnkeyedSameSubject,
		observerelation.StateSameSubjectShadow,
		observerelation.StateManagedKeyDrift,
		observerelation.StateAmbiguous:
		return ReasonRelationAmbiguous,
			"inspect daem status and resolve the relation conflict before refreshing"
	default:
		return ReasonObservationUnavailable,
			"restore fresh passive host inventory evidence before refreshing"
	}
}

func selectExtension(
	extensions []desiredextension.Extension,
	input CommandInput,
) (desiredextension.Extension, error) {
	if input.ExtensionID == "" || strings.TrimSpace(input.ExtensionID) != input.ExtensionID {
		return desiredextension.Extension{}, fmt.Errorf("exact extension id is required")
	}
	var selected desiredextension.Extension
	found := false
	for _, extension := range extensions {
		if extension.ID().Name() != input.ExtensionID {
			continue
		}
		if found {
			return desiredextension.Extension{}, fmt.Errorf("extension id %q is ambiguous", input.ExtensionID)
		}
		selected = extension
		found = true
	}
	if !found {
		return desiredextension.Extension{}, fmt.Errorf("extension id %q is not declared", input.ExtensionID)
	}
	if input.TargetValue != "" {
		selectedTarget, err := target.ParseTarget(input.TargetValue)
		if err != nil {
			return desiredextension.Extension{}, fmt.Errorf("target selector: %w", err)
		}
		if selected.Target() != selectedTarget {
			return desiredextension.Extension{}, fmt.Errorf(
				"extension %q target is %q, not %q",
				input.ExtensionID,
				selected.Target(),
				selectedTarget,
			)
		}
	}
	if input.ScopeValue != "" {
		selectedScope, err := target.ParseScope(input.ScopeValue)
		if err != nil {
			return desiredextension.Extension{}, fmt.Errorf("scope selector: %w", err)
		}
		if selected.Scope() != selectedScope {
			return desiredextension.Extension{}, fmt.Errorf(
				"extension %q scope is %q, not %q",
				input.ExtensionID,
				selected.Scope(),
				selectedScope,
			)
		}
	}
	return selected, nil
}

func refreshPosture(contract lock.OperationContract) (ObservationPosture, error) {
	if contract.Operation() != lock.OperationRefresh {
		return "", fmt.Errorf("refresh workflow received operation %q", contract.Operation())
	}
	if contract.Actuation() != lock.ActuationDelegatedHostRoute {
		return "", fmt.Errorf("refresh operation must use delegated host-route actuation")
	}
	if contract.Authority() != lock.AuthorityNone {
		return "", fmt.Errorf("refresh operation must not grant durable relation authority")
	}
	switch contract.Verification() {
	case lock.VerificationHostRelation:
		return PostureRequireCurrent, nil
	case lock.VerificationInsufficient:
		return PostureAttemptWhenUnsupported, nil
	default:
		return "", fmt.Errorf("refresh verification contract %q is unsupported", contract.Verification())
	}
}
