package opencodeorder

import (
	"bytes"
	"fmt"
	"slices"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

// DocumentOrderObservation owns one loaded candidate baseline, fixed-slot
// candidate, and exact postcondition.
type DocumentOrderObservation struct {
	selection         orderSelection
	document          Document
	sequence          observerelation.ObservedRelationSequence
	expectedSequence  observerelation.ObservedRelationSequence
	candidate         []byte
	changed           bool
	precedenceChanges []observerelation.PrecedenceChange
}

// Kind returns the selected server or TUI config family.
func (observation DocumentOrderObservation) Kind() opencodeconfig.ConfigKind {
	return observation.document.kind
}

// Path returns the exact selected config authority path.
func (observation DocumentOrderObservation) Path() string {
	return observation.document.path
}

// Scope returns the independently selected OpenCode config layer.
func (observation DocumentOrderObservation) Scope() target.Scope {
	return observation.selection.scope
}

// Sequence returns the immutable observed physical order.
func (observation DocumentOrderObservation) Sequence() observerelation.ObservedRelationSequence {
	return observation.sequence
}

// ExpectedSequence returns the exact sequence represented by Candidate.
func (observation DocumentOrderObservation) ExpectedSequence() observerelation.ObservedRelationSequence {
	return observation.expectedSequence
}

// Changed reports whether the exact candidate differs from the baseline bytes.
func (observation DocumentOrderObservation) Changed() bool { return observation.changed }

// Candidate returns owned candidate bytes and whether the selected file existed.
// Missing input remains a non-creating no-op.
func (observation DocumentOrderObservation) Candidate() ([]byte, bool) {
	return bytes.Clone(observation.candidate), observation.document.exists
}

// Validate rejects a zero or forged document observation.
func (observation DocumentOrderObservation) Validate() error {
	expected, err := observeDocument(observation.selection, observation.document)
	if err != nil {
		return err
	}
	if !equalObservedSequence(observation.sequence, expected.sequence) ||
		!equalObservedSequence(observation.expectedSequence, expected.expectedSequence) ||
		!bytes.Equal(observation.candidate, expected.candidate) ||
		observation.changed != expected.changed ||
		!slices.Equal(observation.precedenceChanges, expected.precedenceChanges) {
		return fmt.Errorf("OpenCode %s plugin order observation is not canonical", observation.Kind())
	}
	return nil
}

// OrderObservation owns every independently mutable loaded OpenCode sequence.
type OrderObservation struct {
	selection orderSelection
	inventory Inventory
	documents []DocumentOrderObservation
}

// ReconstructOrder normalizes the loaded inventory under the supplied order contract.
func ReconstructOrder(
	capability profile.ExtensionOrderCapability,
	inventory Inventory,
	constraint hostrelation.RelationOrderConstraint,
	relations []ScopedRelation,
) (OrderObservation, error) {
	selection, err := newOrderSelection(capability, inventory.scope, inventory.directory, constraint, relations)
	if err != nil {
		return OrderObservation{}, err
	}
	return observeOrder(selection, inventory)
}

// Validate rejects a zero or forged aggregate observation.
func (observation OrderObservation) Validate() error {
	expected, err := observeOrder(observation.selection, observation.inventory)
	if err != nil {
		return err
	}
	if len(observation.documents) != len(expected.documents) {
		return fmt.Errorf("OpenCode plugin order observation document count is not canonical")
	}
	for index := range observation.documents {
		if err := observation.documents[index].Validate(); err != nil {
			return fmt.Errorf("OpenCode plugin order document[%d]: %w", index, err)
		}
		if observation.documents[index].Kind() != expected.documents[index].Kind() ||
			observation.documents[index].Path() != expected.documents[index].Path() {
			return fmt.Errorf("OpenCode plugin order document[%d] selection is not canonical", index)
		}
	}
	return nil
}

// Scope returns the selected OpenCode config layer.
func (observation OrderObservation) Scope() target.Scope {
	return observation.selection.scope
}

// Documents returns server then TUI candidate observations in stable order.
func (observation OrderObservation) Documents() []DocumentOrderObservation {
	return append([]DocumentOrderObservation(nil), observation.documents...)
}

func observeOrder(
	selection orderSelection,
	inventory Inventory,
) (OrderObservation, error) {
	if inventory.scope != selection.scope || inventory.directory != selection.directory {
		return OrderObservation{}, fmt.Errorf(
			"OpenCode plugin order inventory does not match selected scope and directory",
		)
	}
	if err := validateOrderInventory(inventory); err != nil {
		return OrderObservation{}, err
	}
	documents := make([]DocumentOrderObservation, 0, len(inventory.documents))
	for _, document := range inventory.documents {
		observation, err := observeDocument(selection, document)
		if err != nil {
			return OrderObservation{}, err
		}
		documents = append(documents, observation)
	}
	return OrderObservation{
		selection: selection,
		inventory: inventory,
		documents: documents,
	}, nil
}

func normalizedDocumentRows(
	selection orderSelection,
	document Document,
) ([]observerelation.ObservedRelationRow, error) {
	exactCounts := make(map[string]int, len(selection.exactSubjects))
	entries := document.Entries()
	rows := make([]observerelation.ObservedRelationRow, 0, len(entries))
	for index, entry := range entries {
		loadIdentity, err := hostrelation.NewHostLoadIdentity(entry.HostLoadIdentity())
		if err != nil {
			return nil, fmt.Errorf(
				"normalize OpenCode %s plugin row[%d] identity: %w",
				document.kind,
				index,
				err,
			)
		}
		subject, exact := selection.exactSubjects[entry.Source()]
		if exact {
			exactCounts[entry.Source()]++
			if exactCounts[entry.Source()] > 1 {
				return nil, fmt.Errorf(
					"OpenCode %s config contains %d exact plugin rows for source %q",
					document.kind,
					exactCounts[entry.Source()],
					entry.Source(),
				)
			}
		}
		var row observerelation.ObservedRelationRow
		if exact {
			row, err = observerelation.NewCorrelatedObservedRelationRow(loadIdentity, subject)
		} else {
			row, err = observerelation.NewObservedRelationRow(loadIdentity)
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func observeDocument(selection orderSelection, document Document) (DocumentOrderObservation, error) {
	rows, err := normalizedDocumentRows(selection, document)
	if err != nil {
		return DocumentOrderObservation{}, err
	}
	sequence, err := newObservedOrderSequence(selection, document, document.revision, rows)
	if err != nil {
		return DocumentOrderObservation{}, err
	}
	selected, err := observerelation.NewSelectedSequenceEdit(selection.capability, sequence, selection.constraint, nil)
	if err != nil {
		return DocumentOrderObservation{}, fmt.Errorf(
			"normalize OpenCode %s plugin order: %w",
			document.kind,
			err,
		)
	}

	var candidate []byte
	changed := false
	if document.exists {
		candidate, changed, err = document.parsed.PermutePluginRows(selected.SurvivorOrder())
		if err != nil {
			return DocumentOrderObservation{}, err
		}
	}
	expectedSequence, err := newObservedOrderSequence(
		selection,
		document,
		contentRevision(candidate),
		selected.ExpectedRows(),
	)
	if err != nil {
		return DocumentOrderObservation{}, err
	}
	return DocumentOrderObservation{
		selection:         selection,
		document:          document,
		sequence:          sequence,
		expectedSequence:  expectedSequence,
		candidate:         candidate,
		changed:           changed,
		precedenceChanges: selected.PrecedenceChanges(),
	}, nil
}

func newObservedOrderSequence(
	selection orderSelection,
	document Document,
	contentRevision string,
	rows []observerelation.ObservedRelationRow,
) (observerelation.ObservedRelationSequence, error) {
	sequenceID, err := opencodeconfig.PhysicalSequenceID(
		selection.scope,
		document.kind,
		document.path,
	)
	if err != nil {
		return observerelation.ObservedRelationSequence{}, err
	}
	if !selection.capability.AdmitsPhysicalSequenceID(sequenceID) {
		return observerelation.ObservedRelationSequence{}, fmt.Errorf(
			"OpenCode plugin order capability does not admit physical sequence %q",
			sequenceID,
		)
	}
	authority, err := observerelation.NewSequenceAuthority(string(sequenceID))
	if err != nil {
		return observerelation.ObservedRelationSequence{}, err
	}
	revision, err := observerelation.NewSequenceRevision(contentRevision)
	if err != nil {
		return observerelation.ObservedRelationSequence{}, err
	}
	return observerelation.NewObservedRelationSequence(
		selection.capability.ClassID(),
		sequenceID,
		authority,
		revision,
		rows,
	)
}

func equalObservedSequence(
	left observerelation.ObservedRelationSequence,
	right observerelation.ObservedRelationSequence,
) bool {
	return left.SameOrdering(right) && left.Revision() == right.Revision()
}
