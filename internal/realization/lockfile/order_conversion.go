package lockfile

import (
	"fmt"

	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

func orderConstraintsFromDTO(
	values []lockedOrderConstraintDTO,
) ([]hostrelation.RelationOrderConstraint, error) {
	if len(values) == 0 {
		return nil, nil
	}
	constraints := make([]hostrelation.RelationOrderConstraint, 0, len(values))
	var previousClassID hostrelation.OrderClassID
	for index, value := range values {
		classID, err := hostrelation.NewOrderClassID(value.ClassID)
		if err != nil {
			return nil, fmt.Errorf("locked order_constraint[%d] class_id: %w", index, err)
		}
		if index > 0 && classID <= previousClassID {
			return nil, fmt.Errorf(
				"locked order_constraint[%d] class_id %q is not in canonical order after %q",
				index,
				classID,
				previousClassID,
			)
		}
		previousClassID = classID
		runtimeMeaning := hostrelation.RuntimeMeaning(value.RuntimeMeaning)
		if err := runtimeMeaning.Validate(); err != nil {
			return nil, fmt.Errorf("locked order_constraint[%d] runtime_meaning: %w", index, err)
		}
		members := make([]hostrelation.RelationOrderMember, 0, len(value.Members))
		for memberIndex, memberValue := range value.Members {
			subjectID, err := topology.ParseSubjectID(memberValue.SubjectID)
			if err != nil {
				return nil, fmt.Errorf(
					"locked order_constraint[%d] member[%d] subject_id: %w",
					index,
					memberIndex,
					err,
				)
			}
			hostLoadIdentity, err := hostrelation.NewHostLoadIdentity(
				memberValue.HostLoadIdentity,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"locked order_constraint[%d] member[%d] host_load_identity: %w",
					index,
					memberIndex,
					err,
				)
			}
			member, err := hostrelation.NewRelationOrderMember(subjectID, hostLoadIdentity)
			if err != nil {
				return nil, fmt.Errorf(
					"locked order_constraint[%d] member[%d]: %w",
					index,
					memberIndex,
					err,
				)
			}
			members = append(members, member)
		}
		constraint, err := hostrelation.NewRelationOrderConstraint(
			classID,
			value.ContractVersion,
			runtimeMeaning,
			members,
		)
		if err != nil {
			return nil, fmt.Errorf("locked order_constraint[%d]: %w", index, err)
		}
		constraints = append(constraints, constraint)
	}
	return constraints, nil
}

func orderConstraintsToDTO(
	values []hostrelation.RelationOrderConstraint,
) ([]lockedOrderConstraintDTO, error) {
	if len(values) == 0 {
		return nil, nil
	}
	constraints := make([]lockedOrderConstraintDTO, 0, len(values))
	for index, value := range values {
		if err := value.Validate(); err != nil {
			return nil, fmt.Errorf("locked order_constraint[%d]: %w", index, err)
		}
		members := value.Members()
		memberDTOs := make([]lockedOrderMemberDTO, 0, len(members))
		for _, member := range members {
			memberDTOs = append(memberDTOs, lockedOrderMemberDTO{
				SubjectID:        member.Subject().String(),
				HostLoadIdentity: string(member.HostLoadIdentity()),
			})
		}
		constraints = append(constraints, lockedOrderConstraintDTO{
			ClassID:         string(value.ClassID()),
			ContractVersion: value.MemberIdentityContract(),
			RuntimeMeaning:  string(value.RuntimeMeaning()),
			Members:         memberDTOs,
		})
	}
	return constraints, nil
}
