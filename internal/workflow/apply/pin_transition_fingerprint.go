package apply

import (
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
)

type carrierPinFingerprintFacts struct {
	CarrierSubject  topology.SubjectID
	RelationSubject topology.SubjectID
	SubjectKey      string
	ManagedKey      string
	Request         realizationdelegate.Request
}

type relationPinFingerprintFacts struct {
	Before       carrierClaimFingerprintFacts
	Resuming     bool
	State        observerelation.CorrelationState
	Reason       observerelation.ReasonCode
	Availability observerelation.InventoryAvailability
	Freshness    observerelation.EvidenceFreshness
}

func carrierPinFingerprint(claim durablecarrier.ManagedCarrierClaim) *carrierPinFingerprintFacts {
	transition, present := claim.PendingPinTransition()
	if !present {
		return nil
	}
	identity := transition.Identity()
	return &carrierPinFingerprintFacts{
		CarrierSubject: identity.CarrierSubject(), RelationSubject: identity.RelationSubject(),
		SubjectKey: string(identity.ExpectedRelation().SubjectKey()), ManagedKey: string(identity.ExpectedRelation().ManagedInstanceKey()),
		Request: transition.Request(),
	}
}

func relationPinFingerprint(action reconcile.RelationAction) *relationPinFingerprintFacts {
	transition, present := action.PinTransition()
	if !present {
		return nil
	}
	before, _ := action.PinTransitionBeforeCorrelation()
	return &relationPinFingerprintFacts{
		Before: carrierClaimFingerprintFact(transition.Before()), Resuming: action.ResumesPinTransition(),
		State: before.State(), Reason: before.Reason(), Availability: before.EvidenceAvailability(), Freshness: before.EvidenceFreshness(),
	}
}
