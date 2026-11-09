package relation

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

// SelectedSequenceEdit retains an original CAS observation and a positional edit.
// Source contexts and class-wide membership admission stay with the normalizer;
// this value contains no parser, filesystem path or publication authority.
type SelectedSequenceEdit struct {
	kind             selectedSequenceKind
	original         ObservedRelationSequence
	constraint       hostrelation.RelationOrderConstraint
	capability       profile.ExtensionOrderCapability
	projection       profile.NativeSequenceProjection
	identityContract string
	removed          []int
	order            []int
	changes          []PrecedenceChange
}

type selectedSequenceKind uint8

const (
	selectedSequenceOrdered selectedSequenceKind = iota + 1
	selectedSequenceRetirementOnly
)

// NewSelectedSequenceEdit computes order and precedence after selected removals.
// It does not require every logical class member in each physical sequence.
// Removals admit only correlated subjects outside the desired constraint.
// Moving still-desired members between documents is not supported by retirement.
func NewSelectedSequenceEdit(capability profile.ExtensionOrderCapability, original ObservedRelationSequence, constraint hostrelation.RelationOrderConstraint, removed []int) (SelectedSequenceEdit, error) {
	selected, err := selectedSequenceOriginal(capability, original, removed)
	if err != nil {
		return SelectedSequenceEdit{}, err
	}
	if err := ValidateOrderConstraintBudget(constraint); err != nil {
		return SelectedSequenceEdit{}, err
	}
	if err := constraint.Validate(); err != nil {
		return SelectedSequenceEdit{}, err
	}
	if constraint.ClassID() != selected.original.classID ||
		constraint.MemberIdentityContract() != capability.MemberIdentityContract() || constraint.RuntimeMeaning() != capability.RuntimeMeaning() {
		return SelectedSequenceEdit{}, fmt.Errorf("selected sequence constraint does not match its profile class")
	}
	var desired map[topology.SubjectID]struct{}
	if len(removed) != 0 {
		desired = make(map[topology.SubjectID]struct{}, constraint.MemberCount())
		for _, member := range constraint.Members() {
			desired[member.Subject()] = struct{}{}
		}
	}
	for _, index := range removed {
		subject, _ := selected.original.orderedRows[index].CorrelatedSubject()
		if _, retained := desired[subject]; retained {
			return SelectedSequenceEdit{}, fmt.Errorf("selected sequence removal cannot retire a still-desired subject")
		}
	}
	selected.kind, selected.constraint = selectedSequenceOrdered, constraint
	selected.order, selected.changes, err = FixedSlotPermutation(constraint, selected.SurvivingRows())
	if err != nil {
		return SelectedSequenceEdit{}, err
	}
	return selected, nil
}

// NewRetirementSequenceEdit selects pure removal when no desired order class
// remains. Correlated survivors would be an unfulfilled retirement obligation.
func NewRetirementSequenceEdit(capability profile.ExtensionOrderCapability, original ObservedRelationSequence, removed []int) (SelectedSequenceEdit, error) {
	selected, err := selectedSequenceOriginal(capability, original, removed)
	if err != nil {
		return SelectedSequenceEdit{}, err
	}
	selected.kind = selectedSequenceRetirementOnly
	surviving := selected.SurvivingRows()
	selected.order = make([]int, len(surviving))
	for index, row := range surviving {
		if _, correlated := row.CorrelatedSubject(); correlated {
			return SelectedSequenceEdit{}, fmt.Errorf("retirement-only sequence cannot retain a correlated subject")
		}
		selected.order[index] = index
	}
	return selected, nil
}

func selectedSequenceOriginal(capability profile.ExtensionOrderCapability, original ObservedRelationSequence, removed []int) (SelectedSequenceEdit, error) {
	checked, err := NewObservedRelationSequence(original.classID, original.sequenceID, original.authority, original.revision, original.orderedRows)
	if err != nil {
		return SelectedSequenceEdit{}, err
	}
	if err := capability.Validate(); err != nil {
		return SelectedSequenceEdit{}, err
	}
	if checked.classID != capability.ClassID() {
		return SelectedSequenceEdit{}, fmt.Errorf("selected sequence does not match its profile class")
	}
	projection, present := capability.NativeSequence(checked.sequenceID)
	if !present {
		return SelectedSequenceEdit{}, fmt.Errorf("selected physical sequence has no admitted native projection")
	}
	if err := projection.Validate(); err != nil {
		return SelectedSequenceEdit{}, err
	}
	previous := -1
	for _, index := range removed {
		if index <= previous || index < 0 || index >= len(checked.orderedRows) {
			return SelectedSequenceEdit{}, fmt.Errorf("selected original removal slots must be sorted, unique and in range")
		}
		if _, correlated := checked.orderedRows[index].CorrelatedSubject(); !correlated {
			return SelectedSequenceEdit{}, fmt.Errorf("selected sequence removal cannot retire a foreign row")
		}
		previous = index
	}
	return SelectedSequenceEdit{original: checked, projection: projection, capability: capability, identityContract: capability.MemberIdentityContract(), removed: slices.Clone(removed)}, nil
}

func (selected SelectedSequenceEdit) Original() ObservedRelationSequence { return selected.original }

func (selected SelectedSequenceEdit) Constraint() (hostrelation.RelationOrderConstraint, bool) {
	return selected.constraint, selected.kind == selectedSequenceOrdered
}

func (selected SelectedSequenceEdit) SameIntent(other SelectedSequenceEdit) bool {
	return selected.kind != 0 && selected.kind == other.kind && selected.original.ClassID() == other.original.ClassID() && selected.original.SequenceID() == other.original.SequenceID() &&
		selected.original.Authority() == other.original.Authority() && selected.capability.Equal(other.capability) &&
		(selected.kind == selectedSequenceRetirementOnly || selected.constraint.Equal(other.constraint))
}

func (selected SelectedSequenceEdit) SameMutation(other SelectedSequenceEdit) bool {
	return selected.SameIntent(other) && selected.original.Revision() == other.original.Revision() && selected.original.SameOrdering(other.original) &&
		slices.Equal(selected.removed, other.removed) && slices.Equal(selected.order, other.order)
}

func (selected SelectedSequenceEdit) Projection() profile.NativeSequenceProjection {
	return selected.projection
}

func (selected SelectedSequenceEdit) MemberIdentityContract() string {
	return selected.identityContract
}

func (selected SelectedSequenceEdit) RemovedSlots() []int  { return slices.Clone(selected.removed) }
func (selected SelectedSequenceEdit) SurvivorOrder() []int { return slices.Clone(selected.order) }

func (selected SelectedSequenceEdit) PrecedenceChanges() []PrecedenceChange {
	return slices.Clone(selected.changes)
}

// SurvivingRows is a planning baseline, never a new disk revision or recovery phase.
func (selected SelectedSequenceEdit) SurvivingRows() []ObservedRelationRow {
	rows := make([]ObservedRelationRow, 0, len(selected.original.orderedRows)-len(selected.removed))
	removedIndex := 0
	for index, row := range selected.original.orderedRows {
		if removedIndex < len(selected.removed) && selected.removed[removedIndex] == index {
			removedIndex++
		} else {
			rows = append(rows, row)
		}
	}
	return rows
}

func (selected SelectedSequenceEdit) ExpectedRows() []ObservedRelationRow {
	surviving := selected.SurvivingRows()
	rows := make([]ObservedRelationRow, len(selected.order))
	for slot, source := range selected.order {
		rows[slot] = surviving[source]
	}
	return rows
}

func (selected SelectedSequenceEdit) Changed() bool {
	if len(selected.removed) != 0 {
		return true
	}
	for slot, source := range selected.order {
		if slot != source {
			return true
		}
	}
	return false
}
