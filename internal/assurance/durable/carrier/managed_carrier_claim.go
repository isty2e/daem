package carrier

import (
	"cmp"
	"fmt"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// ClaimProvenance identifies the evidence-backed transition that created a claim.
type ClaimProvenance string

const (
	// ClaimProvenanceInstalledObserved records admitted absent-before and
	// route-specific present-after evidence around one install.
	ClaimProvenanceInstalledObserved ClaimProvenance = "installed_observed_transition"
	// ClaimProvenanceExplicitlyAdoptedObserved records an explicit state-only
	// adoption of one fresh source-exact external relation.
	ClaimProvenanceExplicitlyAdoptedObserved ClaimProvenance = "explicitly_adopted_observed"
)

// ManagedCarrierClaim is durable delete-authority provenance for one exact
// daem-managed carrier relation. It is not current host evidence.
type ManagedCarrierClaim struct {
	owner          stateauthority.Authority
	identity       ManagedCarrierIdentity
	installRequest realizationdelegate.Request
	provenance     ClaimProvenance
}

// NewManagedCarrierClaim reconstructs one exact durable claim.
func NewManagedCarrierClaim(
	owner stateauthority.Authority,
	identity ManagedCarrierIdentity,
	installRequest realizationdelegate.Request,
	provenance ClaimProvenance,
) (ManagedCarrierClaim, error) {
	claim := ManagedCarrierClaim{
		owner:          owner,
		identity:       identity,
		installRequest: installRequest,
		provenance:     provenance,
	}
	if err := claim.Validate(); err != nil {
		return ManagedCarrierClaim{}, err
	}
	return claim, nil
}

// ClaimAfterObservedInstall validates one completed install and preserves an
// exact retained claim's first-acquisition provenance. A different retained
// claim for the same owner relation is a contradiction.
func ClaimAfterObservedInstall(
	pending PendingCarrierInstall,
	observation observerelation.CorrelationResult,
	retained []ManagedCarrierClaim,
) (ManagedCarrierClaim, error) {
	if err := pending.Validate(); err != nil {
		return ManagedCarrierClaim{}, err
	}
	if err := validateObservedInstallEvidence(pending.Identity(), observation); err != nil {
		return ManagedCarrierClaim{}, err
	}
	key := pending.FactKey()
	var matched ManagedCarrierClaim
	hasMatched := false
	for index, claim := range retained {
		if err := claim.Validate(); err != nil {
			return ManagedCarrierClaim{}, fmt.Errorf(
				"retained managed carrier claim[%d]: %w",
				index,
				err,
			)
		}
		if claim.FactKey() != key {
			continue
		}
		if hasMatched {
			return ManagedCarrierClaim{}, fmt.Errorf(
				"retained managed carrier claims duplicate one owner relation",
			)
		}
		if claim.Owner().ExactEqual(pending.Owner()) &&
			claim.Identity().ExactEqual(pending.Identity()) &&
			claim.InstallRequest().Equal(pending.InstallRequest()) {
			matched = claim
			hasMatched = true
			continue
		}
		return ManagedCarrierClaim{}, fmt.Errorf(
			"observed install conflicts with retained managed carrier claim",
		)
	}
	if hasMatched {
		return matched, nil
	}
	return NewManagedCarrierClaim(
		pending.Owner(),
		pending.Identity(),
		pending.InstallRequest(),
		ClaimProvenanceInstalledObserved,
	)
}

// ClaimFromObservedAdoption creates state-only management authority for one
// fresh source-exact external relation already declared by the current lock.
func ClaimFromObservedAdoption(
	owner stateauthority.Authority,
	locked lock.LockedSubjectContract,
	observation observerelation.CorrelationResult,
) (ManagedCarrierClaim, error) {
	if err := owner.Validate(); err != nil {
		return ManagedCarrierClaim{}, fmt.Errorf("managed carrier adoption owner: %w", err)
	}
	identity, admitted, err := ManagedCarrierIdentityFromLockedRecord(locked)
	if err != nil {
		return ManagedCarrierClaim{}, fmt.Errorf("managed carrier adoption identity: %w", err)
	}
	if !admitted {
		return ManagedCarrierClaim{}, fmt.Errorf("managed carrier adoption requires a locked carrier relation")
	}
	installRequest, err := lock.DelegatedOperationRequest(locked, lock.OperationInstall)
	if err != nil {
		return ManagedCarrierClaim{}, fmt.Errorf("managed carrier adoption acquisition request: %w", err)
	}
	evidenceClass, err := identity.Carrier().RelationEvidence()
	if err != nil {
		return ManagedCarrierClaim{}, fmt.Errorf("managed carrier adoption relation evidence: %w", err)
	}
	if evidenceClass != extensiontopology.RelationEvidenceSourceExact {
		return ManagedCarrierClaim{}, fmt.Errorf(
			"managed carrier adoption requires source-exact relation evidence, got %q",
			evidenceClass,
		)
	}
	if err := validateFreshExactRelationEvidence(identity, observation); err != nil {
		return ManagedCarrierClaim{}, fmt.Errorf("managed carrier adoption: %w", err)
	}
	return NewManagedCarrierClaim(
		owner,
		identity,
		installRequest,
		ClaimProvenanceExplicitlyAdoptedObserved,
	)
}

func validateObservedInstallEvidence(
	identity ManagedCarrierIdentity,
	observation observerelation.CorrelationResult,
) error {
	evidenceClass, err := identity.Carrier().RelationEvidence()
	if err != nil {
		return fmt.Errorf("managed carrier relation evidence: %w", err)
	}
	switch evidenceClass {
	case extensiontopology.RelationEvidenceSourceExact:
		return validateSourceExactInstallEvidence(identity, observation)
	case extensiontopology.RelationEvidenceBoundedSameSubject:
		return validateBoundedInstallEvidence(identity, observation)
	case extensiontopology.RelationEvidenceUnavailable:
		return fmt.Errorf("managed carrier claim requires admitted relation evidence")
	default:
		return fmt.Errorf("managed carrier relation evidence %q is unsupported", evidenceClass)
	}
}

func validateSourceExactInstallEvidence(
	identity ManagedCarrierIdentity,
	observation observerelation.CorrelationResult,
) error {
	if err := validateFreshExactRelationEvidence(identity, observation); err != nil {
		return fmt.Errorf("managed carrier claim post-install evidence: %w", err)
	}
	return nil
}

func validateFreshExactRelationEvidence(
	identity ManagedCarrierIdentity,
	observation observerelation.CorrelationResult,
) error {
	if observation.State() != observerelation.StateExactCorrelation ||
		observation.EvidenceAvailability() != observerelation.InventorySupported ||
		observation.EvidenceFreshness() != observerelation.EvidenceFresh {
		return fmt.Errorf("fresh exact relation correlation is required")
	}
	sameSubject := observation.SameSubjectRows()
	managedKey := observation.ManagedKeyRows()
	expected := identity.ExpectedRelation()
	if len(sameSubject) != 1 || sameSubject[0].SubjectKey() != expected.SubjectKey() ||
		len(managedKey) != 1 {
		return fmt.Errorf("fresh exact relation correlation does not match expected relation")
	}
	observedManagedKey, present := managedKey[0].ManagedInstanceKey()
	if !present || observedManagedKey != expected.ManagedInstanceKey() {
		return fmt.Errorf("fresh exact relation correlation does not match managed instance key")
	}
	return nil
}

func validateBoundedInstallEvidence(
	identity ManagedCarrierIdentity,
	observation observerelation.CorrelationResult,
) error {
	if observation.State() != observerelation.StateUnkeyedSameSubject ||
		observation.EvidenceAvailability() != observerelation.InventorySupported ||
		observation.EvidenceFreshness() != observerelation.EvidenceFresh {
		return fmt.Errorf("managed carrier claim requires fresh bounded post-install evidence")
	}
	sameSubject := observation.SameSubjectRows()
	if len(sameSubject) != 1 ||
		sameSubject[0].SubjectKey() != identity.ExpectedRelation().SubjectKey() ||
		len(observation.ManagedKeyRows()) != 0 {
		return fmt.Errorf("managed carrier claim bounded evidence does not match expected relation subject")
	}
	if _, hasManagedKey := sameSubject[0].ManagedInstanceKey(); hasManagedKey {
		return fmt.Errorf("managed carrier claim bounded evidence must remain unkeyed")
	}
	return nil
}

// Validate rejects a forged claim or unsupported provenance.
func (claim ManagedCarrierClaim) Validate() error {
	if err := claim.owner.Validate(); err != nil {
		return fmt.Errorf("managed carrier claim owner: %w", err)
	}
	if err := claim.identity.Validate(); err != nil {
		return fmt.Errorf("managed carrier claim identity: %w", err)
	}
	if err := claim.installRequest.Validate(); err != nil {
		return fmt.Errorf("managed carrier claim install request: %w", err)
	}
	switch claim.provenance {
	case ClaimProvenanceInstalledObserved,
		ClaimProvenanceExplicitlyAdoptedObserved:
	default:
		return fmt.Errorf("unsupported managed carrier claim provenance %q", claim.provenance)
	}
	return nil
}

// Owner returns the manifest state authority that owns this claim.
func (claim ManagedCarrierClaim) Owner() stateauthority.Authority { return claim.owner }

// Identity returns the exact carrier and managed relation identity.
func (claim ManagedCarrierClaim) Identity() ManagedCarrierIdentity {
	return claim.identity
}

// InstallRequest returns the acquisition contract identity, not a removal route.
func (claim ManagedCarrierClaim) InstallRequest() realizationdelegate.Request {
	return claim.installRequest
}

// Provenance returns the evidence-backed transition that created this claim.
func (claim ManagedCarrierClaim) Provenance() ClaimProvenance { return claim.provenance }

// FactKey returns the owner-relation key used for durable collection identity.
func (claim ManagedCarrierClaim) FactKey() CarrierFactKey {
	return carrierFactKey(claim.owner, claim.identity)
}

// Compare returns the canonical persisted order between managed claims.
func (claim ManagedCarrierClaim) Compare(other ManagedCarrierClaim) int {
	return cmp.Or(
		cmp.Compare(claim.owner.StatefileKey(), other.owner.StatefileKey()),
		cmp.Compare(claim.identity.RelationSubject().String(), other.identity.RelationSubject().String()),
		cmp.Compare(claim.identity.CarrierSubject().String(), other.identity.CarrierSubject().String()),
		cmp.Compare(claim.installRequest.RouteID(), other.installRequest.RouteID()),
		cmp.Compare(claim.installRequest.ContractVersion(), other.installRequest.ContractVersion()),
		cmp.Compare(claim.installRequest.CanonicalRequestHash(), other.installRequest.CanonicalRequestHash()),
		cmp.Compare(claim.provenance, other.provenance),
	)
}

// ExactEqual reports complete persisted claim equality.
func (claim ManagedCarrierClaim) ExactEqual(other ManagedCarrierClaim) bool {
	return claim.owner.ExactEqual(other.owner) &&
		claim.identity.ExactEqual(other.identity) &&
		claim.installRequest.Equal(other.installRequest) &&
		claim.provenance == other.provenance
}

// SameAcquisition reports whether two claims describe the same owner, carrier
// relation, and install request while intentionally ignoring first-acquisition
// provenance.
func (claim ManagedCarrierClaim) SameAcquisition(other ManagedCarrierClaim) bool {
	return claim.owner.ExactEqual(other.owner) &&
		claim.identity.ExactEqual(other.identity) &&
		claim.installRequest.Equal(other.installRequest)
}

// MatchesLockedRecord checks exact current lock identity without treating it as
// current observation or removal eligibility.
func (claim ManagedCarrierClaim) MatchesLockedRecord(locked lock.LockedSubjectContract) bool {
	return claim.Validate() == nil &&
		claim.identity.MatchesLockedRecord(locked, claim.installRequest)
}
