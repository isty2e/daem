package carrier

import (
	"cmp"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// ManagedCarrierIdentity ties one structural carrier to one exact managed host
// relation. It grants no current-state or mutation authority.
type ManagedCarrierIdentity struct {
	carrier  extensiontopology.Carrier
	subject  topology.SubjectID
	relation hostrelation.ExpectedRelation
}

// NewManagedCarrierIdentity validates the complete structural and correlation identity.
func NewManagedCarrierIdentity(
	carrier extensiontopology.Carrier,
	subject topology.SubjectID,
	relation hostrelation.ExpectedRelation,
) (ManagedCarrierIdentity, error) {
	if err := carrier.Validate(); err != nil {
		return ManagedCarrierIdentity{}, fmt.Errorf("managed carrier identity: %w", err)
	}
	if err := subject.Validate(); err != nil {
		return ManagedCarrierIdentity{}, fmt.Errorf("managed carrier relation subject: %w", err)
	}
	if subject.Kind() != topology.SubjectHostRelation {
		return ManagedCarrierIdentity{}, fmt.Errorf("managed carrier identity requires host_relation subject")
	}
	if !extensiontopology.IsCarrierRelation(carrier.Family(), subject) {
		return ManagedCarrierIdentity{}, fmt.Errorf(
			"managed carrier relation subject is outside carrier family %q",
			carrier.Family(),
		)
	}
	if err := relation.Validate(); err != nil {
		return ManagedCarrierIdentity{}, fmt.Errorf("managed carrier expected relation: %w", err)
	}
	expected, err := hostrelation.Derive(carrier.Key(), subject, relation.SubjectKey())
	if err != nil {
		return ManagedCarrierIdentity{}, err
	}
	if !relation.Equal(expected) {
		return ManagedCarrierIdentity{}, fmt.Errorf(
			"managed carrier relation key does not match carrier and subject identity",
		)
	}
	return ManagedCarrierIdentity{carrier: carrier, subject: subject, relation: relation}, nil
}

// Validate rejects a zero or forged identity.
func (identity ManagedCarrierIdentity) Validate() error {
	expected, err := NewManagedCarrierIdentity(identity.carrier, identity.subject, identity.relation)
	if err != nil {
		return err
	}
	if !identity.ExactEqual(expected) {
		return fmt.Errorf("managed carrier identity is not canonical")
	}
	return nil
}

// Carrier returns the declaration-independent structural carrier identity.
func (identity ManagedCarrierIdentity) Carrier() extensiontopology.Carrier {
	return identity.carrier
}

// CarrierSubject returns the canonical structural carrier subject.
func (identity ManagedCarrierIdentity) CarrierSubject() topology.SubjectID {
	return identity.carrier.SubjectID()
}

// RelationSubject returns the exact declaration-local host relation subject.
func (identity ManagedCarrierIdentity) RelationSubject() topology.SubjectID {
	return identity.subject
}

// ExpectedRelation returns the host-visible subject and structural correlation key.
func (identity ManagedCarrierIdentity) ExpectedRelation() hostrelation.ExpectedRelation {
	return identity.relation
}

// Target returns the carrier host target.
func (identity ManagedCarrierIdentity) Target() target.Target {
	return identity.carrier.Key().Target()
}

// Scope returns the carrier locality.
func (identity ManagedCarrierIdentity) Scope() target.Scope {
	return identity.carrier.Key().Scope()
}

// SourceNamespace returns the exact unresolved host-native source identity.
func (identity ManagedCarrierIdentity) SourceNamespace() string {
	return identity.carrier.Source().String()
}

// ExactEqual reports complete structural and relation identity equality.
func (identity ManagedCarrierIdentity) ExactEqual(other ManagedCarrierIdentity) bool {
	return identity.carrier == other.carrier &&
		identity.subject == other.subject &&
		identity.relation.Equal(other.relation)
}

// ManagedCarrierIdentityFromLockedRecord reconstructs the complete structural
// identity from one admitted locked carrier relation.
func ManagedCarrierIdentityFromLockedRecord(
	locked lock.LockedSubjectContract,
) (ManagedCarrierIdentity, bool, error) {
	carrierKey, admitted, err := lock.DelegatedRelationCarrierKey(locked)
	if err != nil || !admitted {
		return ManagedCarrierIdentity{}, admitted, err
	}
	realizationSpec, ok := locked.Realization()
	if !ok {
		return ManagedCarrierIdentity{}, true, fmt.Errorf(
			"managed carrier identity requires delegated relation realization",
		)
	}
	relationSpec, ok := realizationSpec.DelegatedRelation()
	if !ok {
		return ManagedCarrierIdentity{}, true, fmt.Errorf(
			"managed carrier identity requires delegated relation realization",
		)
	}
	carrier, err := extensiontopology.NewCarrier(carrierKey)
	if err != nil {
		return ManagedCarrierIdentity{}, true, err
	}
	identity, err := NewManagedCarrierIdentity(
		carrier,
		locked.SubjectID(),
		relationSpec.ExpectedRelation(),
	)
	if err != nil {
		return ManagedCarrierIdentity{}, true, err
	}
	return identity, true, nil
}

// MatchesLockedRecord checks the full structural relation and install-request identity.
func (identity ManagedCarrierIdentity) MatchesLockedRecord(
	locked lock.LockedSubjectContract,
	install realizationdelegate.Request,
) bool {
	if identity.Validate() != nil || install.Validate() != nil {
		return false
	}
	lockedIdentity, admitted, err := ManagedCarrierIdentityFromLockedRecord(locked)
	if err != nil || !admitted || !identity.ExactEqual(lockedIdentity) {
		return false
	}
	request, err := lock.DelegatedOperationRequest(locked, lock.OperationInstall)
	return err == nil && request.Equal(install)
}

// PendingCarrierInstall is write-ahead correlation eligibility for one exact
// observed-absent install. It is not a claim or evidence of current presence.
type PendingCarrierInstall struct {
	owner          stateauthority.Authority
	identity       ManagedCarrierIdentity
	installRequest realizationdelegate.Request
}

// NewPendingCarrierInstall constructs exact pre-effect correlation state.
func NewPendingCarrierInstall(
	owner stateauthority.Authority,
	identity ManagedCarrierIdentity,
	installRequest realizationdelegate.Request,
) (PendingCarrierInstall, error) {
	pending := PendingCarrierInstall{
		owner:          owner,
		identity:       identity,
		installRequest: installRequest,
	}
	if err := pending.Validate(); err != nil {
		return PendingCarrierInstall{}, err
	}
	return pending, nil
}

// Validate rejects incomplete or mixed pending-install facts.
func (pending PendingCarrierInstall) Validate() error {
	if err := pending.owner.Validate(); err != nil {
		return fmt.Errorf("pending carrier install owner: %w", err)
	}
	if err := pending.identity.Validate(); err != nil {
		return fmt.Errorf("pending carrier install identity: %w", err)
	}
	if err := pending.installRequest.Validate(); err != nil {
		return fmt.Errorf("pending carrier install request: %w", err)
	}
	return nil
}

// Owner returns the state authority that observed absence and requested install.
func (pending PendingCarrierInstall) Owner() stateauthority.Authority { return pending.owner }

// Identity returns the exact carrier and relation identity.
func (pending PendingCarrierInstall) Identity() ManagedCarrierIdentity {
	return pending.identity
}

// InstallRequest returns the acquisition route identity.
func (pending PendingCarrierInstall) InstallRequest() realizationdelegate.Request {
	return pending.installRequest
}

// FactKey returns the owner-relation key used for durable collection identity.
func (pending PendingCarrierInstall) FactKey() CarrierFactKey {
	return carrierFactKey(pending.owner, pending.identity)
}

// Compare returns the canonical persisted order between pending installs.
func (pending PendingCarrierInstall) Compare(other PendingCarrierInstall) int {
	return cmp.Or(
		pending.owner.Key().Compare(other.owner.Key()),
		cmp.Compare(pending.identity.RelationSubject().String(), other.identity.RelationSubject().String()),
		cmp.Compare(pending.identity.CarrierSubject().String(), other.identity.CarrierSubject().String()),
		cmp.Compare(pending.installRequest.RouteID(), other.installRequest.RouteID()),
		cmp.Compare(pending.installRequest.ContractVersion(), other.installRequest.ContractVersion()),
		cmp.Compare(pending.installRequest.CanonicalRequestHash(), other.installRequest.CanonicalRequestHash()),
	)
}

// ExactEqual reports complete persisted pending-state equality.
func (pending PendingCarrierInstall) ExactEqual(other PendingCarrierInstall) bool {
	return pending.owner.ExactEqual(other.owner) &&
		pending.identity.ExactEqual(other.identity) &&
		pending.installRequest.Equal(other.installRequest)
}

// MatchesLockedRecord checks the exact acquisition identity without treating
// the pending fact as current host evidence.
func (pending PendingCarrierInstall) MatchesLockedRecord(
	locked lock.LockedSubjectContract,
) bool {
	return pending.Validate() == nil &&
		pending.identity.MatchesLockedRecord(locked, pending.installRequest)
}
