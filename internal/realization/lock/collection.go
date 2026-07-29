package lock

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/desired/entity"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

const currentVersion = 4

// CurrentVersion is the supported canonical lock snapshot version.
const CurrentVersion = currentVersion

// File is the canonical reproducible lock snapshot.
type File struct {
	Version int
	Locked  LockedSection
}

// LockedSection is the one canonical, deterministically ordered locked-subject collection.
type LockedSection struct {
	subjects         []LockedSubjectContract
	orderConstraints []hostrelation.RelationOrderConstraint
	bySubject        map[topology.SubjectID]int
	byEntity         map[entity.ID][]int
	byOrderClass     map[hostrelation.OrderClassID]int
}

// NewLockedSection validates, sorts, and defensively copies one complete
// subject and cross-subject order-constraint collection.
func NewLockedSection(
	subjects []LockedSubjectContract,
	orderConstraints []hostrelation.RelationOrderConstraint,
) (LockedSection, error) {
	canonical := append([]LockedSubjectContract(nil), subjects...)
	for index, subject := range canonical {
		if err := subject.validate(); err != nil {
			return LockedSection{}, fmt.Errorf("locked subject[%d]: %w", index, err)
		}
	}
	sort.Slice(canonical, func(left int, right int) bool {
		return canonical[left].CompareIdentity(canonical[right]) < 0
	})

	collectionIndex, err := validateLockedCollection(canonical)
	if err != nil {
		return LockedSection{}, err
	}
	for _, subject := range canonical {
		if err := validateAdmittedLockedSubject(subject); err != nil {
			return LockedSection{}, fmt.Errorf("locked subject %q refinement: %w", subject.SubjectID(), err)
		}
	}
	if err := validateLockedCollectionAdmission(collectionIndex); err != nil {
		return LockedSection{}, err
	}
	bySubject := make(map[topology.SubjectID]int, len(canonical))
	byEntity := make(map[entity.ID][]int)
	for index, subject := range canonical {
		bySubject[subject.SubjectID()] = index
		byEntity[subject.EntityID()] = append(byEntity[subject.EntityID()], index)
	}
	canonicalOrder, byOrderClass, err := validateLockedOrderConstraints(
		canonical,
		bySubject,
		orderConstraints,
	)
	if err != nil {
		return LockedSection{}, err
	}
	return LockedSection{
		subjects:         canonical,
		orderConstraints: canonicalOrder,
		bySubject:        bySubject,
		byEntity:         byEntity,
		byOrderClass:     byOrderClass,
	}, nil
}

// Subjects returns a stable defensive copy of all locked subjects.
func (section LockedSection) Subjects() []LockedSubjectContract {
	return append([]LockedSubjectContract(nil), section.subjects...)
}

// OrderConstraints returns a stable defensive copy sorted by order class.
func (section LockedSection) OrderConstraints() []hostrelation.RelationOrderConstraint {
	return append([]hostrelation.RelationOrderConstraint(nil), section.orderConstraints...)
}

// OrderConstraint returns the one locked constraint for classID when present.
func (section LockedSection) OrderConstraint(
	classID hostrelation.OrderClassID,
) (hostrelation.RelationOrderConstraint, bool) {
	index, ok := section.byOrderClass[classID]
	if !ok {
		return hostrelation.RelationOrderConstraint{}, false
	}
	return section.orderConstraints[index], true
}

// Subject returns the one contract for id when present.
func (section LockedSection) Subject(id topology.SubjectID) (LockedSubjectContract, bool) {
	index, ok := section.bySubject[id]
	if !ok {
		return LockedSubjectContract{}, false
	}
	return section.subjects[index], true
}

// ExactSupplySubject returns the canonical exact-Supply resource subject for one Desired entity.
func (section LockedSection) ExactSupplySubject(id entity.ID) (LockedSubjectContract, bool) {
	if id.Validate() != nil {
		return LockedSubjectContract{}, false
	}
	for _, index := range section.byEntity[id] {
		contract := section.subjects[index]
		if contract.SubjectID().Kind() != topology.SubjectResource {
			continue
		}
		if _, hasSupply := contract.ExactSupply(); hasSupply {
			return contract, true
		}
	}
	return LockedSubjectContract{}, false
}

// Len returns the number of canonical locked subjects.
func (section LockedSection) Len() int { return len(section.subjects) }

// OrderLen returns the number of locked relative-order constraints.
func (section LockedSection) OrderLen() int { return len(section.orderConstraints) }

// Validate rejects malformed or non-canonical lock snapshots.
func Validate(file File) error {
	if file.Version != currentVersion {
		return fmt.Errorf("unsupported lockfile version %d", file.Version)
	}
	canonical, err := NewLockedSection(
		file.Locked.subjects,
		file.Locked.orderConstraints,
	)
	if err != nil {
		return err
	}
	if canonical.Len() != file.Locked.Len() {
		return fmt.Errorf("locked subject collection length changed during validation")
	}
	for index, subject := range canonical.subjects {
		if !subject.Equal(file.Locked.subjects[index]) {
			return fmt.Errorf("locked subject collection is not in canonical order at index %d", index)
		}
	}
	if canonical.OrderLen() != file.Locked.OrderLen() {
		return fmt.Errorf("locked order-constraint collection length changed during validation")
	}
	for index, constraint := range canonical.orderConstraints {
		if !constraint.Equal(file.Locked.orderConstraints[index]) {
			return fmt.Errorf(
				"locked order-constraint collection is not in canonical order at index %d",
				index,
			)
		}
	}
	return nil
}

// DeltaStatus identifies how a locked subject changed between two snapshots.
type DeltaStatus string

const (
	DeltaStatusAdded     DeltaStatus = "added"
	DeltaStatusRemoved   DeltaStatus = "removed"
	DeltaStatusChanged   DeltaStatus = "changed"
	DeltaStatusUnchanged DeltaStatus = "unchanged"
)

// DeltaEntry describes one changed or unchanged locked subject.
type DeltaEntry struct {
	Status DeltaStatus
	Key    topology.SubjectID
	Before LockedSubjectContract
	After  LockedSubjectContract
}

// DeltaCounts summarizes locked-subject statuses.
type DeltaCounts struct {
	Added     int
	Changed   int
	Removed   int
	Unchanged int
}

// Delta compares two reproducible lock snapshots.
type Delta struct {
	entries      []DeltaEntry
	orderEntries []OrderDeltaEntry
}

// BuildDelta compares canonical lock snapshots by SubjectID.
func BuildDelta(before File, after File) Delta {
	beforeEntries := subjectMap(before)
	afterEntries := subjectMap(after)
	keys := make([]topology.SubjectID, 0, len(beforeEntries)+len(afterEntries))
	seen := make(map[topology.SubjectID]struct{}, len(beforeEntries)+len(afterEntries))
	for key := range beforeEntries {
		keys = appendKey(keys, seen, key)
	}
	for key := range afterEntries {
		keys = appendKey(keys, seen, key)
	}
	sort.Slice(keys, func(left int, right int) bool {
		return topology.CompareSubjectID(keys[left], keys[right]) < 0
	})

	entries := make([]DeltaEntry, 0, len(keys))
	for _, key := range keys {
		beforeSubject, hadBefore := beforeEntries[key]
		afterSubject, hasAfter := afterEntries[key]
		switch {
		case !hadBefore && hasAfter:
			entries = append(entries, DeltaEntry{Status: DeltaStatusAdded, Key: key, After: afterSubject})
		case hadBefore && !hasAfter:
			entries = append(entries, DeltaEntry{Status: DeltaStatusRemoved, Key: key, Before: beforeSubject})
		case beforeSubject.Equal(afterSubject):
			entries = append(entries, DeltaEntry{Status: DeltaStatusUnchanged, Key: key, Before: beforeSubject, After: afterSubject})
		default:
			entries = append(entries, DeltaEntry{Status: DeltaStatusChanged, Key: key, Before: beforeSubject, After: afterSubject})
		}
	}
	return Delta{
		entries:      entries,
		orderEntries: buildOrderDelta(before, after),
	}
}

// Entries returns a stable copy of delta entries.
func (delta Delta) Entries() []DeltaEntry {
	return append([]DeltaEntry(nil), delta.entries...)
}

// EntriesWithStatus returns stable entries with the selected status.
func (delta Delta) EntriesWithStatus(status DeltaStatus) []DeltaEntry {
	entries := make([]DeltaEntry, 0)
	for _, entry := range delta.entries {
		if entry.Status == status {
			entries = append(entries, entry)
		}
	}
	return entries
}

// Counts returns locked-subject status counts.
func (delta Delta) Counts() DeltaCounts {
	counts := DeltaCounts{}
	for _, entry := range delta.entries {
		switch entry.Status {
		case DeltaStatusAdded:
			counts.Added++
		case DeltaStatusRemoved:
			counts.Removed++
		case DeltaStatusChanged:
			counts.Changed++
		case DeltaStatusUnchanged:
			counts.Unchanged++
		}
	}
	return counts
}

// HasChanges reports whether any subject or relative-order constraint changed.
func (delta Delta) HasChanges() bool {
	counts := delta.Counts()
	orderCounts := delta.OrderCounts()
	return counts.Added != 0 ||
		counts.Removed != 0 ||
		counts.Changed != 0 ||
		orderCounts.Added != 0 ||
		orderCounts.Removed != 0 ||
		orderCounts.Changed != 0
}

func subjectMap(file File) map[topology.SubjectID]LockedSubjectContract {
	subjects := file.Locked.Subjects()
	entries := make(map[topology.SubjectID]LockedSubjectContract, len(subjects))
	for _, subject := range subjects {
		entries[subject.SubjectID()] = subject
	}
	return entries
}

func appendKey(
	keys []topology.SubjectID,
	seen map[topology.SubjectID]struct{},
	key topology.SubjectID,
) []topology.SubjectID {
	if _, exists := seen[key]; exists {
		return keys
	}
	seen[key] = struct{}{}
	return append(keys, key)
}
