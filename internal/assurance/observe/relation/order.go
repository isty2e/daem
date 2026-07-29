package relation

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

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
