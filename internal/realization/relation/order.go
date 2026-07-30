package hostrelation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"slices"
	"strconv"

	"github.com/isty2e/daem/internal/topology"
)

const relationOrderFingerprintVersion = "relation-order:v1"

// OrderClassID identifies one target/scope-relative extension ordering domain.
type OrderClassID string

// PhysicalSequenceID identifies one independently observed host sequence.
type PhysicalSequenceID string

// HostLoadIdentity is the canonical identity a host uses to deduplicate or
// override one extension within an order class.
type HostLoadIdentity string

// RuntimeMeaning classifies the strongest meaning admitted for one order
// class. It does not itself grant observation or mutation authority.
type RuntimeMeaning string

const (
	RuntimePrecedence RuntimeMeaning = "runtime-precedence"
	ConfigOrderOnly   RuntimeMeaning = "config-order-only"
	RuntimeUnknown    RuntimeMeaning = "unknown"
)

// NewOrderClassID validates one canonical order-class identity.
func NewOrderClassID(value string) (OrderClassID, error) {
	if err := validateText("relation order class id", value); err != nil {
		return "", err
	}
	return OrderClassID(value), nil
}

// Validate rejects a zero or malformed order-class identity.
func (id OrderClassID) Validate() error {
	_, err := NewOrderClassID(string(id))
	return err
}

// NewPhysicalSequenceID validates one canonical physical-sequence identity.
func NewPhysicalSequenceID(value string) (PhysicalSequenceID, error) {
	if err := validateText("relation physical sequence id", value); err != nil {
		return "", err
	}
	return PhysicalSequenceID(value), nil
}

// Validate rejects a zero or malformed physical-sequence identity.
func (id PhysicalSequenceID) Validate() error {
	_, err := NewPhysicalSequenceID(string(id))
	return err
}

// NewHostLoadIdentity validates one already-normalized host load identity.
// Host codecs remain responsible for contract-specific normalization.
func NewHostLoadIdentity(value string) (HostLoadIdentity, error) {
	if err := validateText("host load identity", value); err != nil {
		return "", err
	}
	return HostLoadIdentity(value), nil
}

// Validate rejects a zero or malformed host load identity.
func (identity HostLoadIdentity) Validate() error {
	_, err := NewHostLoadIdentity(string(identity))
	return err
}

// Validate rejects an open or zero runtime-order classification.
func (meaning RuntimeMeaning) Validate() error {
	switch meaning {
	case RuntimePrecedence, ConfigOrderOnly, RuntimeUnknown:
		return nil
	default:
		return fmt.Errorf("unsupported relation runtime meaning %q", meaning)
	}
}

// RelationOrderMember pairs one exact extension relation subject with the
// independent identity used by its host order class.
type RelationOrderMember struct {
	subject          topology.SubjectID
	hostLoadIdentity HostLoadIdentity
}

// NewRelationOrderMember constructs one canonical order member.
func NewRelationOrderMember(
	subject topology.SubjectID,
	hostLoadIdentity HostLoadIdentity,
) (RelationOrderMember, error) {
	if err := subject.Validate(); err != nil {
		return RelationOrderMember{}, err
	}
	if subject.Kind() != topology.SubjectHostRelation {
		return RelationOrderMember{}, fmt.Errorf(
			"relation order member subject must be a host relation, got %q",
			subject.Kind(),
		)
	}
	if err := hostLoadIdentity.Validate(); err != nil {
		return RelationOrderMember{}, err
	}
	return RelationOrderMember{
		subject:          subject,
		hostLoadIdentity: hostLoadIdentity,
	}, nil
}

// Validate rejects a zero or forged order member.
func (member RelationOrderMember) Validate() error {
	_, err := NewRelationOrderMember(member.subject, member.hostLoadIdentity)
	return err
}

// Subject returns the exact extension relation subject.
func (member RelationOrderMember) Subject() topology.SubjectID { return member.subject }

// HostLoadIdentity returns the host deduplication or override identity.
func (member RelationOrderMember) HostLoadIdentity() HostLoadIdentity {
	return member.hostLoadIdentity
}

// RelationOrderConstraint is one immutable class-relative desired order. The
// member slice is semantically ordered and is never sorted by declaration ID.
type RelationOrderConstraint struct {
	classID                OrderClassID
	memberIdentityContract string
	runtimeMeaning         RuntimeMeaning
	members                []RelationOrderMember
}

// NewRelationOrderConstraint validates one desired or locked relative order.
func NewRelationOrderConstraint(
	classID OrderClassID,
	memberIdentityContract string,
	runtimeMeaning RuntimeMeaning,
	members []RelationOrderMember,
) (RelationOrderConstraint, error) {
	if err := classID.Validate(); err != nil {
		return RelationOrderConstraint{}, err
	}
	if err := validateText("relation member identity contract", memberIdentityContract); err != nil {
		return RelationOrderConstraint{}, err
	}
	if err := runtimeMeaning.Validate(); err != nil {
		return RelationOrderConstraint{}, err
	}
	if len(members) == 0 {
		return RelationOrderConstraint{}, fmt.Errorf(
			"relation order class %q requires at least one member",
			classID,
		)
	}

	canonical := append([]RelationOrderMember(nil), members...)
	subjects := make(map[topology.SubjectID]struct{}, len(canonical))
	loadIdentities := make(map[HostLoadIdentity]struct{}, len(canonical))
	for index, member := range canonical {
		if err := member.Validate(); err != nil {
			return RelationOrderConstraint{}, fmt.Errorf(
				"relation order member[%d]: %w",
				index,
				err,
			)
		}
		if _, duplicate := subjects[member.subject]; duplicate {
			return RelationOrderConstraint{}, fmt.Errorf(
				"relation order subject %q appears more than once",
				member.subject,
			)
		}
		subjects[member.subject] = struct{}{}
		if _, duplicate := loadIdentities[member.hostLoadIdentity]; duplicate {
			return RelationOrderConstraint{}, fmt.Errorf(
				"host load identity %q appears more than once",
				member.hostLoadIdentity,
			)
		}
		loadIdentities[member.hostLoadIdentity] = struct{}{}
	}

	return RelationOrderConstraint{
		classID:                classID,
		memberIdentityContract: memberIdentityContract,
		runtimeMeaning:         runtimeMeaning,
		members:                canonical,
	}, nil
}

// Validate rejects a zero or forged order constraint.
func (constraint RelationOrderConstraint) Validate() error {
	canonical, err := NewRelationOrderConstraint(
		constraint.classID,
		constraint.memberIdentityContract,
		constraint.runtimeMeaning,
		constraint.members,
	)
	if err != nil {
		return err
	}
	if constraint.classID != canonical.classID ||
		constraint.memberIdentityContract != canonical.memberIdentityContract ||
		constraint.runtimeMeaning != canonical.runtimeMeaning ||
		!slices.Equal(constraint.members, canonical.members) {
		return fmt.Errorf("relation order constraint is not canonical")
	}
	return nil
}

// ClassID returns the target/scope-relative ordering domain.
func (constraint RelationOrderConstraint) ClassID() OrderClassID { return constraint.classID }

// MemberIdentityContract returns the versioned host-load identity contract.
func (constraint RelationOrderConstraint) MemberIdentityContract() string {
	return constraint.memberIdentityContract
}

// RuntimeMeaning returns the admitted meaning of sequence order.
func (constraint RelationOrderConstraint) RuntimeMeaning() RuntimeMeaning {
	return constraint.runtimeMeaning
}

// Members returns the authored member order as a defensive copy.
func (constraint RelationOrderConstraint) Members() []RelationOrderMember {
	return append([]RelationOrderMember(nil), constraint.members...)
}

// MemberCount returns the logical class cardinality without copying members.
func (constraint RelationOrderConstraint) MemberCount() int {
	return len(constraint.members)
}

// Equal reports complete semantic equality, including member order.
func (constraint RelationOrderConstraint) Equal(other RelationOrderConstraint) bool {
	return constraint.classID == other.classID &&
		constraint.memberIdentityContract == other.memberIdentityContract &&
		constraint.runtimeMeaning == other.runtimeMeaning &&
		slices.Equal(constraint.members, other.members)
}

// Fingerprint returns a stable digest over the complete ordered constraint.
func (constraint RelationOrderConstraint) Fingerprint() string {
	if constraint.Validate() != nil {
		return ""
	}
	digest := sha256.New()
	writeOrderFingerprintField(digest, relationOrderFingerprintVersion)
	writeOrderFingerprintField(digest, string(constraint.classID))
	writeOrderFingerprintField(digest, constraint.memberIdentityContract)
	writeOrderFingerprintField(digest, string(constraint.runtimeMeaning))
	for _, member := range constraint.members {
		writeOrderFingerprintField(digest, member.subject.String())
		writeOrderFingerprintField(digest, string(member.hostLoadIdentity))
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func writeOrderFingerprintField(digest hash.Hash, value string) {
	_, _ = digest.Write([]byte(strconv.Itoa(len(value))))
	_, _ = digest.Write([]byte(":"))
	_, _ = digest.Write([]byte(value))
	_, _ = digest.Write([]byte("\n"))
}
