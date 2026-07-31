package carrieradoption

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type rowFingerprint struct {
	SubjectKey         string
	ManagedInstanceKey string
	HasManagedKey      bool
}

type operationFingerprint struct {
	Operation            lock.OperationKind
	Actuation            lock.ActuationKind
	Authority            lock.AuthorityKind
	Route                lock.RouteContractRef
	HostCompatibility    lock.HostCompatibilityConstraint
	Preconditions        []string
	EffectEnvelope       lock.EffectEnvelopeClass
	EffectPostconditions []effectpostcondition.Requirement
	Idempotency          lock.IdempotencyContract
	Verification         lock.VerificationContract
	TrustActivation      lock.TrustActivationRequirement
	Recovery             lock.OperationRecoveryClass
}

type pathAuthorityFingerprint struct {
	Key              string
	SemanticsWitness string
}

type claimFingerprint struct {
	OwnerStatefileAuthority pathAuthorityFingerprint
	OwnerManifestPath       string
	CarrierSubject          topology.SubjectID
	RelationSubject         topology.SubjectID
	RelationSubjectKey      string
	ManagedInstanceKey      string
	SourceNamespace         string
	InstallRequest          realizationdelegate.Request
	Provenance              durablecarrier.ClaimProvenance
}

type planFingerprint struct {
	OwnerStatefileAuthority pathAuthorityFingerprint
	OwnerManifestPath       string
	CarrierSubject          topology.SubjectID
	RelationSubject         topology.SubjectID
	Target                  target.Target
	Scope                   target.Scope
	SourceNamespace         string
	RelationSubjectKey      string
	ManagedInstanceKey      string
	AcquisitionRequest      realizationdelegate.Request
	ObservationState        observerelation.CorrelationState
	ObservationReason       observerelation.ReasonCode
	EvidenceAvailability    observerelation.InventoryAvailability
	EvidenceFreshness       observerelation.EvidenceFreshness
	Rows                    []rowFingerprint
	Watchpoints             []observerelation.Watchpoint
	Claims                  []claimFingerprint
	Install                 operationFingerprint
	InstallRoute            InstallRouteStatus
	RemovalStatus           string
	Removal                 operationFingerprint
	RemovalRequest          realizationdelegate.Request
	PreservesShared         bool
	RemovedEffects          []string
	RetainedEffects         []string
	RemovalNonClaims        []string
	ClaimStore              ClaimStore
	StoreAvailable          bool
	LifecycleBlocker        LifecycleBlocker
	ManageExisting          bool
	Result                  Result
	ProposedClaim           *claimFingerprint
}

func planIdentityFor(action Action) (PlanIdentity, error) {
	expected := action.identity.ExpectedRelation()
	removal := action.lifecycle.RemovalRoute()
	claims := make([]claimFingerprint, 0, len(action.claims))
	for _, claim := range action.claims {
		claims = append(claims, claimFingerprintFor(claim))
	}
	var proposed *claimFingerprint
	if action.hasProposedClaim {
		value := claimFingerprintFor(action.proposedClaim)
		proposed = &value
	}
	canonical, err := json.Marshal(planFingerprint{
		OwnerStatefileAuthority: statefileAuthorityFingerprintFor(action.owner),
		OwnerManifestPath:       action.owner.ManifestPath(),
		CarrierSubject:          action.identity.CarrierSubject(),
		RelationSubject:         action.identity.RelationSubject(),
		Target:                  action.identity.Target(),
		Scope:                   action.identity.Scope(),
		SourceNamespace:         action.identity.SourceNamespace(),
		RelationSubjectKey:      string(expected.SubjectKey()),
		ManagedInstanceKey:      string(expected.ManagedInstanceKey()),
		AcquisitionRequest:      action.acquisition,
		ObservationState:        action.observation.State(),
		ObservationReason:       action.observation.Reason(),
		EvidenceAvailability:    action.observation.EvidenceAvailability(),
		EvidenceFreshness:       action.observation.EvidenceFreshness(),
		Rows:                    rowFingerprints(action.observation.Rows()),
		Watchpoints:             action.observation.Watchpoints(),
		Claims:                  claims,
		Install:                 operationFingerprintFor(action.lifecycle.InstallOperation()),
		InstallRoute:            action.lifecycle.InstallRouteStatus(),
		RemovalStatus:           string(removal.Status()),
		Removal:                 operationFingerprintFor(removal.Operation()),
		RemovalRequest:          removal.Request(),
		PreservesShared:         removal.PreservesSharedCarrier(),
		RemovedEffects:          removal.RemovedEffects(),
		RetainedEffects:         removal.RetainedEffects(),
		RemovalNonClaims:        removal.NonClaims(),
		ClaimStore:              action.lifecycle.ClaimStore(),
		StoreAvailable:          action.lifecycle.StoreAvailable(),
		LifecycleBlocker:        action.lifecycle.Blocker(),
		ManageExisting:          action.manageExisting,
		Result:                  action.result,
		ProposedClaim:           proposed,
	})
	if err != nil {
		return "", fmt.Errorf("encode carrier adoption plan identity: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return PlanIdentity("sha256:" + hex.EncodeToString(digest[:])), nil
}

func operationFingerprintFor(operation lock.OperationContract) operationFingerprint {
	return operationFingerprint{
		Operation:            operation.Operation(),
		Actuation:            operation.Actuation(),
		Authority:            operation.Authority(),
		Route:                operation.Route(),
		HostCompatibility:    operation.HostCompatibility(),
		Preconditions:        operation.Preconditions(),
		EffectEnvelope:       operation.EffectEnvelope(),
		EffectPostconditions: operation.EffectPostconditions().Requirements(),
		Idempotency:          operation.Idempotency(),
		Verification:         operation.Verification(),
		TrustActivation:      operation.TrustActivation(),
		Recovery:             operation.Recovery(),
	}
}

func statefileAuthorityFingerprintFor(authority stateauthority.Authority) pathAuthorityFingerprint {
	exact := authority.StatefileAuthority()
	return pathAuthorityFingerprint{
		Key:              exact.Key(),
		SemanticsWitness: exact.Witness(),
	}
}

func claimFingerprintFor(claim durablecarrier.ManagedCarrierClaim) claimFingerprint {
	identity := claim.Identity()
	expected := identity.ExpectedRelation()
	return claimFingerprint{
		OwnerStatefileAuthority: statefileAuthorityFingerprintFor(claim.Owner()),
		OwnerManifestPath:       claim.Owner().ManifestPath(),
		CarrierSubject:          identity.CarrierSubject(),
		RelationSubject:         identity.RelationSubject(),
		RelationSubjectKey:      string(expected.SubjectKey()),
		ManagedInstanceKey:      string(expected.ManagedInstanceKey()),
		SourceNamespace:         identity.SourceNamespace(),
		InstallRequest:          claim.InstallRequest(),
		Provenance:              claim.Provenance(),
	}
}

func rowFingerprints(rows []observerelation.Row) []rowFingerprint {
	result := make([]rowFingerprint, 0, len(rows))
	for _, row := range rows {
		managedKey, hasManagedKey := row.ManagedInstanceKey()
		result = append(result, rowFingerprint{
			SubjectKey:         string(row.SubjectKey()),
			ManagedInstanceKey: string(managedKey),
			HasManagedKey:      hasManagedKey,
		})
	}
	sort.Slice(result, func(left int, right int) bool {
		if result[left].SubjectKey != result[right].SubjectKey {
			return result[left].SubjectKey < result[right].SubjectKey
		}
		if result[left].HasManagedKey != result[right].HasManagedKey {
			return !result[left].HasManagedKey
		}
		return result[left].ManagedInstanceKey < result[right].ManagedInstanceKey
	})
	return result
}
