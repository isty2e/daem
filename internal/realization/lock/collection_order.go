package lock

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

// ExtensionOrderIdentityResolver derives one host's canonical load identity
// from a selected-manifest carrier key.
type ExtensionOrderIdentityResolver func(
	desiredextension.CarrierKey,
) (hostrelation.HostLoadIdentity, error)

type lockedOrderClass struct {
	capability profile.ExtensionOrderCapability
	subjects   map[topology.SubjectID]struct{}
}

// ValidateExtensionOrderIdentities verifies the context-dependent identity in
// every order member against its locked carrier relation.
func ValidateExtensionOrderIdentities(
	file File,
	resolveIdentity ExtensionOrderIdentityResolver,
) error {
	if file.Version != CurrentVersion {
		return fmt.Errorf("unsupported lockfile version %d", file.Version)
	}
	if file.Locked.OrderLen() == 0 {
		return nil
	}
	if resolveIdentity == nil {
		return fmt.Errorf("locked extension order requires a host-load identity resolver")
	}
	for _, constraint := range file.Locked.orderConstraints {
		for index, member := range constraint.Members() {
			subject, present := file.Locked.Subject(member.Subject())
			if !present {
				return fmt.Errorf(
					"locked order class %q member[%d] references missing subject %q",
					constraint.ClassID(),
					index,
					member.Subject(),
				)
			}
			carrier, admitted, err := DelegatedRelationCarrierKey(subject)
			if err != nil {
				return fmt.Errorf(
					"locked order class %q member[%d] carrier: %w",
					constraint.ClassID(),
					index,
					err,
				)
			}
			if !admitted {
				return fmt.Errorf(
					"locked order class %q member[%d] is not an extension relation",
					constraint.ClassID(),
					index,
				)
			}
			expected, err := resolveIdentity(carrier)
			if err != nil {
				return fmt.Errorf(
					"locked order class %q member[%d] host-load identity: %w",
					constraint.ClassID(),
					index,
					err,
				)
			}
			if member.HostLoadIdentity() != expected {
				return fmt.Errorf(
					"locked order class %q member[%d] host-load identity %q does not match derived identity %q",
					constraint.ClassID(),
					index,
					member.HostLoadIdentity(),
					expected,
				)
			}
		}
	}
	return nil
}

func validateLockedOrderConstraints(
	subjects []LockedSubjectContract,
	bySubject map[topology.SubjectID]int,
	constraints []hostrelation.RelationOrderConstraint,
) (
	[]hostrelation.RelationOrderConstraint,
	map[hostrelation.OrderClassID]int,
	error,
) {
	classes, err := lockedOrderClasses(subjects)
	if err != nil {
		return nil, nil, err
	}

	canonical := append([]hostrelation.RelationOrderConstraint(nil), constraints...)
	for index, constraint := range canonical {
		if err := constraint.Validate(); err != nil {
			return nil, nil, fmt.Errorf("locked order_constraint[%d]: %w", index, err)
		}
	}
	sort.Slice(canonical, func(left int, right int) bool {
		return canonical[left].ClassID() < canonical[right].ClassID()
	})

	byClass := make(map[hostrelation.OrderClassID]int, len(canonical))
	for index, constraint := range canonical {
		classID := constraint.ClassID()
		if _, duplicate := byClass[classID]; duplicate {
			return nil, nil, fmt.Errorf(
				"locked order class %q appears more than once",
				classID,
			)
		}
		class, admitted := classes[classID]
		if !admitted {
			return nil, nil, fmt.Errorf(
				"locked order class %q has no admitted locked extension members",
				classID,
			)
		}
		if len(class.subjects) < 2 {
			return nil, nil, fmt.Errorf(
				"locked order class %q requires at least two locked members",
				classID,
			)
		}
		if constraint.MemberIdentityContract() != class.capability.MemberIdentityContract() {
			return nil, nil, fmt.Errorf(
				"locked order class %q member identity contract %q does not match profile %q",
				classID,
				constraint.MemberIdentityContract(),
				class.capability.MemberIdentityContract(),
			)
		}
		if constraint.RuntimeMeaning() != class.capability.RuntimeMeaning() {
			return nil, nil, fmt.Errorf(
				"locked order class %q runtime meaning %q does not match profile %q",
				classID,
				constraint.RuntimeMeaning(),
				class.capability.RuntimeMeaning(),
			)
		}
		if err := validateLockedOrderMembers(constraint, class.subjects, bySubject); err != nil {
			return nil, nil, err
		}
		byClass[classID] = index
	}

	for classID, class := range classes {
		if len(class.subjects) < 2 {
			continue
		}
		if _, present := byClass[classID]; !present {
			return nil, nil, fmt.Errorf(
				"locked order class %q with %d members requires an order constraint",
				classID,
				len(class.subjects),
			)
		}
	}
	return canonical, byClass, nil
}

func lockedOrderClasses(
	subjects []LockedSubjectContract,
) (map[hostrelation.OrderClassID]lockedOrderClass, error) {
	classes := make(map[hostrelation.OrderClassID]lockedOrderClass)
	for _, subject := range subjects {
		if subject.EntityID().Kind() != entity.KindExtension {
			continue
		}
		carrier, admitted, err := DelegatedRelationCarrierKey(subject)
		if err != nil {
			return nil, fmt.Errorf(
				"locked order member subject %q: %w",
				subject.SubjectID(),
				err,
			)
		}
		if !admitted {
			continue
		}
		capability, ordered := profile.Profile(carrier.Target()).ExtensionOrder(
			carrier.Carrier(),
			carrier.Scope(),
		)
		if !ordered {
			continue
		}
		class := classes[capability.ClassID()]
		if class.subjects == nil {
			class = lockedOrderClass{
				capability: capability,
				subjects:   make(map[topology.SubjectID]struct{}),
			}
		}
		class.subjects[subject.SubjectID()] = struct{}{}
		classes[capability.ClassID()] = class
	}
	return classes, nil
}

func validateLockedOrderMembers(
	constraint hostrelation.RelationOrderConstraint,
	classSubjects map[topology.SubjectID]struct{},
	bySubject map[topology.SubjectID]int,
) error {
	members := constraint.Members()
	if len(members) != len(classSubjects) {
		return fmt.Errorf(
			"locked order class %q has %d members, want exactly %d admitted locked subjects",
			constraint.ClassID(),
			len(members),
			len(classSubjects),
		)
	}
	for index, member := range members {
		subjectID := member.Subject()
		if _, present := bySubject[subjectID]; !present {
			return fmt.Errorf(
				"locked order class %q member[%d] references missing subject %q",
				constraint.ClassID(),
				index,
				subjectID,
			)
		}
		if _, admitted := classSubjects[subjectID]; !admitted {
			return fmt.Errorf(
				"locked order class %q member[%d] subject %q belongs to another order class",
				constraint.ClassID(),
				index,
				subjectID,
			)
		}
	}
	return nil
}
