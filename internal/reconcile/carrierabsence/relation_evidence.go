package carrierabsence

import (
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// ObservationAdmitsRouteResolution reports whether current evidence can be
// combined with an exact durable claim to select a removal route. The
// carrier's source interpretation determines whether exact or bounded
// same-subject evidence is admissible; neither form grants authority alone.
func ObservationAdmitsRouteResolution(
	identity durablecarrier.ManagedCarrierIdentity,
	correlation observerelation.CorrelationResult,
) bool {
	evidenceClass, err := identity.Carrier().RelationEvidence()
	if err != nil {
		return false
	}
	switch evidenceClass {
	case extensiontopology.RelationEvidenceSourceExact:
		return correlation.State() == observerelation.StateExactCorrelation
	case extensiontopology.RelationEvidenceBoundedSameSubject:
		return boundedSameSubjectEvidence(identity, correlation)
	default:
		return false
	}
}

func boundedSameSubjectEvidence(
	identity durablecarrier.ManagedCarrierIdentity,
	correlation observerelation.CorrelationResult,
) bool {
	if correlation.State() != observerelation.StateUnkeyedSameSubject ||
		correlation.EvidenceAvailability() != observerelation.InventorySupported ||
		correlation.EvidenceFreshness() != observerelation.EvidenceFresh {
		return false
	}
	sameSubject := correlation.SameSubjectRows()
	if len(sameSubject) != 1 ||
		sameSubject[0].SubjectKey() != identity.ExpectedRelation().SubjectKey() ||
		len(correlation.ManagedKeyRows()) != 0 {
		return false
	}
	_, hasManagedKey := sameSubject[0].ManagedInstanceKey()
	return !hasManagedKey
}
