package carrieradoption

import (
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func classifyObservation(
	identity durablecarrier.ManagedCarrierIdentity,
	observation observerelation.CorrelationResult,
) (Result, bool, error) {
	switch observation.State() {
	case observerelation.StateExactCorrelation:
		evidenceClass, err := identity.Carrier().RelationEvidence()
		if err != nil {
			return "", false, err
		}
		if evidenceClass != extensiontopology.RelationEvidenceSourceExact {
			return ResultInexactRelation, true, nil
		}
		return "", false, nil
	case observerelation.StateMissing:
		return ResultMissingRelation, true, nil
	case observerelation.StateUnkeyedSameSubject,
		observerelation.StateSameSubjectShadow,
		observerelation.StateManagedKeyDrift:
		return ResultInexactRelation, true, nil
	case observerelation.StateAmbiguous,
		observerelation.StateStaleEvidence,
		observerelation.StateUnsupported,
		observerelation.StateUnavailableEvidence:
		return ResultObservationBlocked, true, nil
	default:
		return "", false, fmt.Errorf(
			"carrier adoption correlation state %q is unsupported",
			observation.State(),
		)
	}
}

func validateCorrelation(
	identity durablecarrier.ManagedCarrierIdentity,
	observation observerelation.CorrelationResult,
) error {
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observation.EvidenceAvailability(),
		Freshness:    observation.EvidenceFreshness(),
		Rows:         observation.Rows(),
	})
	if err != nil {
		return err
	}
	expected := observerelation.Correlate(identity.ExpectedRelation(), inventory)
	if !equalCorrelation(observation, expected) {
		return fmt.Errorf("correlation result does not match locked relation and passive rows")
	}
	return nil
}

func equalCorrelation(
	left observerelation.CorrelationResult,
	right observerelation.CorrelationResult,
) bool {
	return left.State() == right.State() &&
		left.Reason() == right.Reason() &&
		left.EvidenceAvailability() == right.EvidenceAvailability() &&
		left.EvidenceFreshness() == right.EvidenceFreshness() &&
		equalRows(left.Rows(), right.Rows()) &&
		equalRows(left.SameSubjectRows(), right.SameSubjectRows()) &&
		equalRows(left.ManagedKeyRows(), right.ManagedKeyRows()) &&
		equalWatchpoints(left.Watchpoints(), right.Watchpoints())
}

func equalRows(left []observerelation.Row, right []observerelation.Row) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		leftKey, leftHasKey := left[index].ManagedInstanceKey()
		rightKey, rightHasKey := right[index].ManagedInstanceKey()
		if left[index].SubjectKey() != right[index].SubjectKey() ||
			leftHasKey != rightHasKey ||
			leftKey != rightKey {
			return false
		}
	}
	return true
}

func equalWatchpoints(left []observerelation.Watchpoint, right []observerelation.Watchpoint) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
