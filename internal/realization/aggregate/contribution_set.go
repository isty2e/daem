package aggregate

import (
	"fmt"
	"slices"
	"sort"

	"github.com/isty2e/daem/internal/topology"
)

// SubjectContribution pairs the sole canonical subject identity with its realization.
type SubjectContribution struct {
	subject      topology.SubjectID
	contribution ManagedContribution
}

// NewSubjectContribution correlates one subject with one contribution body.
func NewSubjectContribution(subject topology.SubjectID, contribution ManagedContribution) (SubjectContribution, error) {
	if err := subject.Validate(); err != nil {
		return SubjectContribution{}, fmt.Errorf("aggregate contribution subject: %w", err)
	}
	if err := contribution.Validate(); err != nil {
		return SubjectContribution{}, err
	}
	return SubjectContribution{subject: subject, contribution: contribution.Clone()}, nil
}

func (item SubjectContribution) SubjectID() topology.SubjectID     { return item.subject }
func (item SubjectContribution) Contribution() ManagedContribution { return item.contribution.Clone() }

// ContributionSet is one canonical set of subjects sharing a projection address.
type ContributionSet struct {
	items []SubjectContribution
}

// NewContributionSet validates and canonicalizes one shared projection set.
func NewContributionSet(values []SubjectContribution) (ContributionSet, error) {
	if len(values) == 0 {
		return ContributionSet{}, fmt.Errorf("aggregate contribution set is required")
	}
	items := append([]SubjectContribution(nil), values...)
	for index := range items {
		canonical, err := NewSubjectContribution(items[index].subject, items[index].contribution)
		if err != nil {
			return ContributionSet{}, fmt.Errorf("aggregate contribution set[%d]: %w", index, err)
		}
		items[index] = canonical
	}
	sort.Slice(items, func(left int, right int) bool {
		return topology.CompareSubjectID(items[left].subject, items[right].subject) < 0
	})
	first := items[0].contribution
	for index, item := range items {
		if index > 0 && item.subject == items[index-1].subject {
			return ContributionSet{}, fmt.Errorf("aggregate contribution set repeats subject %q", item.subject)
		}
		if item.contribution.address != first.address {
			return ContributionSet{}, fmt.Errorf("aggregate contribution set mixes projection addresses")
		}
		if !item.contribution.sameStaticContract(first) {
			return ContributionSet{}, fmt.Errorf("aggregate contribution set mixes codec or preservation contracts")
		}
	}
	if first.cardinality == ContributionExclusive && len(items) != 1 {
		return ContributionSet{}, fmt.Errorf("exclusive aggregate projection accepts exactly one subject")
	}
	return ContributionSet{items: items}, nil
}

// PartitionContributionSets groups canonical subject contributions by their
// complete projection address while preserving first-address order.
func PartitionContributionSets(values []SubjectContribution) ([]ContributionSet, error) {
	if len(values) == 0 {
		return nil, nil
	}
	groups := make([][]SubjectContribution, 0)
	groupIndexByAddress := make(map[ProjectionAddress]int)
	seenSubjects := make(map[topology.SubjectID]struct{}, len(values))
	for index, value := range values {
		canonical, err := NewSubjectContribution(value.subject, value.contribution)
		if err != nil {
			return nil, fmt.Errorf("aggregate contribution[%d]: %w", index, err)
		}
		if _, duplicate := seenSubjects[canonical.subject]; duplicate {
			return nil, fmt.Errorf("aggregate contribution[%d] repeats subject %q", index, canonical.subject)
		}
		seenSubjects[canonical.subject] = struct{}{}
		address := canonical.contribution.address
		groupIndex, exists := groupIndexByAddress[address]
		if !exists {
			groupIndex = len(groups)
			groupIndexByAddress[address] = groupIndex
			groups = append(groups, nil)
		}
		groups[groupIndex] = append(groups[groupIndex], canonical)
	}

	sets := make([]ContributionSet, 0, len(groups))
	for index, group := range groups {
		set, err := NewContributionSet(group)
		if err != nil {
			return nil, fmt.Errorf("aggregate contribution set[%d]: %w", index, err)
		}
		sets = append(sets, set)
	}
	return sets, nil
}

func (contribution ManagedContribution) sameStaticContract(other ManagedContribution) bool {
	return contribution.address == other.address &&
		contribution.cardinality == other.cardinality &&
		contribution.siblingRetention == other.siblingRetention &&
		contribution.siblingPreservation == other.siblingPreservation &&
		contribution.equivalence == other.equivalence &&
		contribution.codecContractID == other.codecContractID &&
		slices.Equal(contribution.comparedFields, other.comparedFields)
}

func (set ContributionSet) Address() ProjectionAddress {
	if len(set.items) == 0 {
		return ProjectionAddress{}
	}
	return set.items[0].contribution.address
}

func (set ContributionSet) Contract() ProjectionContract {
	if len(set.items) == 0 {
		return ProjectionContract{}
	}
	return set.items[0].contribution.Contract()
}

func (set ContributionSet) Contributions() []SubjectContribution {
	result := make([]SubjectContribution, len(set.items))
	for index, item := range set.items {
		result[index] = SubjectContribution{subject: item.subject, contribution: item.contribution.Clone()}
	}
	return result
}

// Equal compares subject membership and every canonical contribution fact.
func (set ContributionSet) Equal(other ContributionSet) bool {
	if len(set.items) == 0 || len(other.items) == 0 || len(set.items) != len(other.items) {
		return false
	}
	for index, item := range set.items {
		if item.subject != other.items[index].subject || !item.contribution.Equal(other.items[index].contribution) {
			return false
		}
	}
	return true
}
