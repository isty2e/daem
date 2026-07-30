package relation

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

const (
	// MaximumOrderRows bounds one physical host sequence before validation or copying.
	MaximumOrderRows = 4_096
	// MaximumOrderMembers bounds desired and observed managed class membership.
	MaximumOrderMembers = 1_024
	// MaximumOrderPrecedencePairs bounds managed/foreign pairs before enumeration.
	MaximumOrderPrecedencePairs = 16_384
)

// OrderLimitKind identifies one bounded extension-order resource dimension.
type OrderLimitKind string

const (
	OrderLimitObservedRows    OrderLimitKind = "observed_rows"
	OrderLimitManagedMembers  OrderLimitKind = "managed_members"
	OrderLimitPrecedencePairs OrderLimitKind = "precedence_pairs"
)

// ErrOrderLimitExceeded classifies deterministic extension-order budget
// failures.
var ErrOrderLimitExceeded = errors.New("extension order resource limit exceeded")

// OrderLimitError reports the first exhausted extension-order dimension.
type OrderLimitError struct {
	kind     OrderLimitKind
	observed int
	limit    int
}

func (err *OrderLimitError) Error() string {
	if err == nil {
		return ErrOrderLimitExceeded.Error()
	}
	return fmt.Sprintf(
		"%s: %s observed=%d limit=%d",
		ErrOrderLimitExceeded,
		err.kind,
		err.observed,
		err.limit,
	)
}

func (err *OrderLimitError) Unwrap() error { return ErrOrderLimitExceeded }

func (err *OrderLimitError) Kind() OrderLimitKind {
	if err == nil {
		return ""
	}
	return err.kind
}

func (err *OrderLimitError) Observed() int {
	if err == nil {
		return 0
	}
	return err.observed
}

func (err *OrderLimitError) Limit() int {
	if err == nil {
		return 0
	}
	return err.limit
}

// SequenceAuthority is an opaque canonical identity for the exact host
// document or surface that produced one physical sequence. Host adapters own
// conversion from paths or native handles into this path-free evidence.
type SequenceAuthority string

// SequenceRevision is the compare-and-swap witness captured with one sequence.
type SequenceRevision string

// NewSequenceAuthority validates one canonical sequence authority identity.
func NewSequenceAuthority(value string) (SequenceAuthority, error) {
	if err := validateSequenceEvidence("relation sequence authority", value); err != nil {
		return "", err
	}
	return SequenceAuthority(value), nil
}

// Validate rejects zero or malformed sequence authority evidence.
func (authority SequenceAuthority) Validate() error {
	_, err := NewSequenceAuthority(string(authority))
	return err
}

// NewSequenceRevision validates one opaque sequence revision witness.
func NewSequenceRevision(value string) (SequenceRevision, error) {
	if err := validateSequenceEvidence("relation sequence revision", value); err != nil {
		return "", err
	}
	return SequenceRevision(value), nil
}

// Validate rejects zero or malformed sequence revision evidence.
func (revision SequenceRevision) Validate() error {
	_, err := NewSequenceRevision(string(revision))
	return err
}

// ObservedRelationRow is one normalized row in a physical host sequence. An
// exact daem subject correlation is optional so foreign rows remain visible.
type ObservedRelationRow struct {
	hostLoadIdentity  hostrelation.HostLoadIdentity
	correlatedSubject topology.SubjectID
	hasCorrelation    bool
}

// PrecedenceChange records one managed/foreign pair whose relative physical
// order changes under a fixed-slot permutation. It is observation evidence,
// not a policy decision or mutation admission.
type PrecedenceChange struct {
	managedSubject      topology.SubjectID
	foreignIdentity     hostrelation.HostLoadIdentity
	managedWasBefore    bool
	managedWillBeBefore bool
}

// ManagedSubject returns the exact correlated relation being moved.
func (change PrecedenceChange) ManagedSubject() topology.SubjectID {
	return change.managedSubject
}

// ForeignIdentity returns the uncorrelated host load identity being crossed.
func (change PrecedenceChange) ForeignIdentity() hostrelation.HostLoadIdentity {
	return change.foreignIdentity
}

// ManagedWasBefore reports the observed relative order.
func (change PrecedenceChange) ManagedWasBefore() bool {
	return change.managedWasBefore
}

// ManagedWillBeBefore reports the candidate relative order.
func (change PrecedenceChange) ManagedWillBeBefore() bool {
	return change.managedWillBeBefore
}

// NewObservedRelationRow constructs an uncorrelated host sequence row.
func NewObservedRelationRow(
	hostLoadIdentity hostrelation.HostLoadIdentity,
) (ObservedRelationRow, error) {
	if err := hostLoadIdentity.Validate(); err != nil {
		return ObservedRelationRow{}, err
	}
	return ObservedRelationRow{hostLoadIdentity: hostLoadIdentity}, nil
}

// NewCorrelatedObservedRelationRow constructs a row correlated to one exact
// extension relation subject.
func NewCorrelatedObservedRelationRow(
	hostLoadIdentity hostrelation.HostLoadIdentity,
	subject topology.SubjectID,
) (ObservedRelationRow, error) {
	row, err := NewObservedRelationRow(hostLoadIdentity)
	if err != nil {
		return ObservedRelationRow{}, err
	}
	if err := subject.Validate(); err != nil {
		return ObservedRelationRow{}, fmt.Errorf("observed relation subject: %w", err)
	}
	if subject.Kind() != topology.SubjectHostRelation {
		return ObservedRelationRow{}, fmt.Errorf(
			"observed relation subject must be a host relation, got %q",
			subject.Kind(),
		)
	}
	row.correlatedSubject = subject
	row.hasCorrelation = true
	return row, nil
}

// Validate rejects a zero or forged observed row.
func (row ObservedRelationRow) Validate() error {
	if err := row.hostLoadIdentity.Validate(); err != nil {
		return err
	}
	if !row.hasCorrelation {
		if !row.correlatedSubject.IsZero() {
			return fmt.Errorf("uncorrelated observed relation row carries a subject")
		}
		return nil
	}
	_, err := NewCorrelatedObservedRelationRow(row.hostLoadIdentity, row.correlatedSubject)
	return err
}

// HostLoadIdentity returns the host deduplication or override identity.
func (row ObservedRelationRow) HostLoadIdentity() hostrelation.HostLoadIdentity {
	return row.hostLoadIdentity
}

// CorrelatedSubject returns the exact daem relation subject when known.
func (row ObservedRelationRow) CorrelatedSubject() (topology.SubjectID, bool) {
	return row.correlatedSubject, row.hasCorrelation
}

// ObservedRelationSequence is one immutable current physical sequence. Its
// authority and revision are observation evidence, never desired identity.
type ObservedRelationSequence struct {
	classID     hostrelation.OrderClassID
	sequenceID  hostrelation.PhysicalSequenceID
	authority   SequenceAuthority
	revision    SequenceRevision
	orderedRows []ObservedRelationRow
}

// NewObservedRelationSequence validates one current physical host sequence.
func NewObservedRelationSequence(
	classID hostrelation.OrderClassID,
	sequenceID hostrelation.PhysicalSequenceID,
	authority SequenceAuthority,
	revision SequenceRevision,
	orderedRows []ObservedRelationRow,
) (ObservedRelationSequence, error) {
	if err := classID.Validate(); err != nil {
		return ObservedRelationSequence{}, err
	}
	if err := sequenceID.Validate(); err != nil {
		return ObservedRelationSequence{}, err
	}
	if err := authority.Validate(); err != nil {
		return ObservedRelationSequence{}, err
	}
	if err := revision.Validate(); err != nil {
		return ObservedRelationSequence{}, err
	}
	if err := checkOrderLimit(
		OrderLimitObservedRows,
		len(orderedRows),
		MaximumOrderRows,
	); err != nil {
		return ObservedRelationSequence{}, err
	}

	rows := append([]ObservedRelationRow(nil), orderedRows...)
	loadIdentities := make(map[hostrelation.HostLoadIdentity]struct{}, len(rows))
	correlatedSubjects := make(map[topology.SubjectID]struct{}, len(rows))
	for index, row := range rows {
		if err := row.Validate(); err != nil {
			return ObservedRelationSequence{}, fmt.Errorf(
				"observed relation sequence row[%d]: %w",
				index,
				err,
			)
		}
		if _, duplicate := loadIdentities[row.hostLoadIdentity]; duplicate {
			return ObservedRelationSequence{}, fmt.Errorf(
				"host load identity %q appears more than once in physical sequence %q",
				row.hostLoadIdentity,
				sequenceID,
			)
		}
		loadIdentities[row.hostLoadIdentity] = struct{}{}
		if row.hasCorrelation {
			if _, duplicate := correlatedSubjects[row.correlatedSubject]; duplicate {
				return ObservedRelationSequence{}, fmt.Errorf(
					"relation subject %q appears more than once in physical sequence %q",
					row.correlatedSubject,
					sequenceID,
				)
			}
			correlatedSubjects[row.correlatedSubject] = struct{}{}
		}
	}

	return ObservedRelationSequence{
		classID:     classID,
		sequenceID:  sequenceID,
		authority:   authority,
		revision:    revision,
		orderedRows: rows,
	}, nil
}

// ClassID returns the logical order class observed by this sequence.
func (sequence ObservedRelationSequence) ClassID() hostrelation.OrderClassID {
	return sequence.classID
}

// SequenceID returns the independently mutable physical sequence identity.
func (sequence ObservedRelationSequence) SequenceID() hostrelation.PhysicalSequenceID {
	return sequence.sequenceID
}

// Authority returns the selected host document or surface identity.
func (sequence ObservedRelationSequence) Authority() SequenceAuthority {
	return sequence.authority
}

// Revision returns the compare-and-swap baseline captured with this sequence.
func (sequence ObservedRelationSequence) Revision() SequenceRevision {
	return sequence.revision
}

// OrderedRows returns a defensive copy in observed physical order.
func (sequence ObservedRelationSequence) OrderedRows() []ObservedRelationRow {
	return append([]ObservedRelationRow(nil), sequence.orderedRows...)
}

// ValidateOrderConstraintBudget rejects desired membership that cannot be
// safely projected onto host-controlled physical sequences.
func ValidateOrderConstraintBudget(
	constraint hostrelation.RelationOrderConstraint,
) error {
	return checkOrderLimit(
		OrderLimitManagedMembers,
		constraint.MemberCount(),
		MaximumOrderMembers,
	)
}

// FixedSlotPermutation projects the managed rows present in one physical
// sequence into constraint order while keeping every foreign row in place.
func FixedSlotPermutation(
	constraint hostrelation.RelationOrderConstraint,
	rows []ObservedRelationRow,
) ([]int, []PrecedenceChange, error) {
	if err := checkOrderLimit(
		OrderLimitObservedRows,
		len(rows),
		MaximumOrderRows,
	); err != nil {
		return nil, nil, err
	}
	if err := ValidateOrderConstraintBudget(constraint); err != nil {
		return nil, nil, err
	}
	if err := constraint.Validate(); err != nil {
		return nil, nil, fmt.Errorf("fixed-slot relation order constraint: %w", err)
	}

	sourceBySubject := make(map[topology.SubjectID]int)
	loadIdentities := make(map[hostrelation.HostLoadIdentity]struct{}, len(rows))
	managedSlots := make([]int, 0)
	for index, row := range rows {
		if err := row.Validate(); err != nil {
			return nil, nil, fmt.Errorf("fixed-slot relation row[%d]: %w", index, err)
		}
		if _, duplicate := loadIdentities[row.HostLoadIdentity()]; duplicate {
			return nil, nil, fmt.Errorf(
				"fixed-slot host load identity %q appears more than once",
				row.HostLoadIdentity(),
			)
		}
		loadIdentities[row.HostLoadIdentity()] = struct{}{}
		subject, correlated := row.CorrelatedSubject()
		if !correlated {
			continue
		}
		if _, duplicate := sourceBySubject[subject]; duplicate {
			return nil, nil, fmt.Errorf(
				"fixed-slot relation subject %q appears more than once",
				subject,
			)
		}
		sourceBySubject[subject] = index
		managedSlots = append(managedSlots, index)
	}
	if err := checkOrderLimit(
		OrderLimitManagedMembers,
		len(managedSlots),
		MaximumOrderMembers,
	); err != nil {
		return nil, nil, err
	}
	foreignRows := len(rows) - len(managedSlots)
	precedencePairs := len(managedSlots) * foreignRows
	if err := checkOrderLimit(
		OrderLimitPrecedencePairs,
		precedencePairs,
		MaximumOrderPrecedencePairs,
	); err != nil {
		return nil, nil, err
	}

	desiredSources := make([]int, 0, len(managedSlots))
	for _, member := range constraint.Members() {
		if source, present := sourceBySubject[member.Subject()]; present {
			desiredSources = append(desiredSources, source)
		}
	}
	if len(desiredSources) != len(managedSlots) {
		return nil, nil, fmt.Errorf(
			"fixed-slot relation order contains a correlated subject outside its constraint",
		)
	}

	order := make([]int, len(rows))
	for index := range order {
		order[index] = index
	}
	for index, slot := range managedSlots {
		order[slot] = desiredSources[index]
	}
	inverse := make([]int, len(order))
	for destination, source := range order {
		inverse[source] = destination
	}

	changes := make([]PrecedenceChange, 0)
	for managedIndex, row := range rows {
		subject, correlated := row.CorrelatedSubject()
		if !correlated {
			continue
		}
		for foreignIndex, foreign := range rows {
			if _, foreignCorrelated := foreign.CorrelatedSubject(); foreignCorrelated {
				continue
			}
			wasBefore := managedIndex < foreignIndex
			willBeBefore := inverse[managedIndex] < inverse[foreignIndex]
			if wasBefore == willBeBefore {
				continue
			}
			changes = append(changes, PrecedenceChange{
				managedSubject:      subject,
				foreignIdentity:     foreign.HostLoadIdentity(),
				managedWasBefore:    wasBefore,
				managedWillBeBefore: willBeBefore,
			})
		}
	}
	return order, changes, nil
}

func checkOrderLimit(kind OrderLimitKind, observed int, limit int) error {
	if observed <= limit {
		return nil
	}
	return &OrderLimitError{kind: kind, observed: observed, limit: limit}
}

func validateSequenceEvidence(label string, value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be non-empty and trimmed", label)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", label)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("%s must not contain control or bidirectional formatting characters", label)
	}
	return nil
}
