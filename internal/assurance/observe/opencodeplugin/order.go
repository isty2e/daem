package opencodeplugin

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

// OrderInput selects one OpenCode scope, its exact desired order, and the
// relations eligible for correlation in every loaded physical document.
type OrderInput struct {
	Inventory  InventoryInput
	Constraint hostrelation.RelationOrderConstraint
	Relations  []ScopedRelation
}

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

// VerifyBaseline checks exact existence and content revision before mutation.
func (observation DocumentOrderObservation) VerifyBaseline(content []byte, exists bool) error {
	if exists != observation.document.exists {
		return fmt.Errorf(
			"OpenCode %s config baseline existence changed from %t to %t",
			observation.Kind(),
			observation.document.exists,
			exists,
		)
	}
	if contentRevision(content) != observation.document.revision {
		return fmt.Errorf("OpenCode %s config baseline revision changed", observation.Kind())
	}
	return nil
}

// VerifyPostContent reparses exact post-read bytes and requires the expected
// canonical sequence. File-write success alone is not convergence evidence.
func (observation DocumentOrderObservation) VerifyPostContent(
	content []byte,
	exists bool,
) error {
	candidate, expectedExists := observation.Candidate()
	if exists != expectedExists || !bytes.Equal(content, candidate) {
		return fmt.Errorf(
			"OpenCode %s config post-observation does not match the exact candidate",
			observation.Kind(),
		)
	}
	document, err := documentFromContent(observation.document, content, exists)
	if err != nil {
		return fmt.Errorf(
			"parse OpenCode %s config post-observation: %w",
			observation.Kind(),
			err,
		)
	}
	post, err := observeDocument(observation.selection, document)
	if err != nil {
		return fmt.Errorf(
			"normalize OpenCode %s config post-observation: %w",
			observation.Kind(),
			err,
		)
	}
	if !equalObservedSequence(post.sequence, observation.expectedSequence) {
		return fmt.Errorf(
			"OpenCode %s config post-observation sequence does not match expected order",
			observation.Kind(),
		)
	}
	return nil
}

// OrderObservation owns every independently mutable loaded OpenCode sequence.
type OrderObservation struct {
	selection orderSelection
	inventory Inventory
	documents []DocumentOrderObservation
}

// ReadOrder observes all loaded OpenCode documents once and derives independent
// fixed-slot normalization candidates.
func ReadOrder(input OrderInput) (OrderObservation, error) {
	inventory, err := ReadInventory(input.Inventory)
	if err != nil {
		return OrderObservation{}, err
	}
	selection, err := newOrderSelection(
		inventory.scope,
		inventory.directory,
		input.Constraint,
		input.Relations,
	)
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

func validateOrderInventory(inventory Inventory) error {
	index := 0
	for _, kind := range []opencodeconfig.ConfigKind{
		opencodeconfig.ConfigServer,
		opencodeconfig.ConfigTUI,
	} {
		names, err := opencodeconfig.CandidateNames(kind)
		if err != nil {
			return err
		}
		seen := 0
		for index < len(inventory.documents) && inventory.documents[index].kind == kind {
			document := inventory.documents[index]
			if seen >= len(names) ||
				document.path != filepath.Join(inventory.directory, names[seen]) {
				if seen == 0 && len(names) > 1 &&
					document.path == filepath.Join(inventory.directory, names[1]) {
					seen = 1
				} else {
					return fmt.Errorf(
						"OpenCode plugin order inventory has non-canonical %s candidate order",
						kind,
					)
				}
			}
			if !document.exists && (seen != 0 || index+1 < len(inventory.documents) &&
				inventory.documents[index+1].kind == kind) {
				return fmt.Errorf(
					"OpenCode plugin order inventory has non-canonical absent %s candidate",
					kind,
				)
			}
			seen++
			index++
		}
		if seen == 0 {
			return fmt.Errorf("OpenCode plugin order inventory has no %s candidate", kind)
		}
	}
	if index != len(inventory.documents) {
		return fmt.Errorf("OpenCode plugin order inventory has unexpected candidate documents")
	}
	return nil
}

func observeDocument(
	selection orderSelection,
	document Document,
) (DocumentOrderObservation, error) {
	exactCounts := make(map[string]int, len(selection.exactSubjects))
	rows := make([]observerelation.ObservedRelationRow, 0, len(document.entries))
	for index, entry := range document.entries {
		loadIdentity, err := hostrelation.NewHostLoadIdentity(entry.hostLoadIdentity)
		if err != nil {
			return DocumentOrderObservation{}, fmt.Errorf(
				"normalize OpenCode %s plugin row[%d] identity: %w",
				document.kind,
				index,
				err,
			)
		}
		subject, exact := selection.exactSubjects[entry.source]
		if exact {
			exactCounts[entry.source]++
			if exactCounts[entry.source] > 1 {
				return DocumentOrderObservation{}, fmt.Errorf(
					"OpenCode %s config contains %d exact plugin rows for source %q",
					document.kind,
					exactCounts[entry.source],
					entry.source,
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
			return DocumentOrderObservation{}, err
		}
		rows = append(rows, row)
	}
	sequence, err := newObservedOrderSequence(selection, document, rows)
	if err != nil {
		return DocumentOrderObservation{}, err
	}
	order, changes, err := observerelation.FixedSlotPermutation(selection.constraint, rows)
	if err != nil {
		return DocumentOrderObservation{}, fmt.Errorf(
			"normalize OpenCode %s plugin order: %w",
			document.kind,
			err,
		)
	}

	candidate := bytes.Clone(document.content)
	changed := false
	if document.exists {
		candidate, changed, err = document.parsed.PermutePluginRows(order)
		if err != nil {
			return DocumentOrderObservation{}, err
		}
	}
	expectedRows := make([]observerelation.ObservedRelationRow, len(rows))
	for destination, source := range order {
		expectedRows[destination] = rows[source]
	}
	expectedDocument := document
	expectedDocument.revision = contentRevision(candidate)
	expectedSequence, err := newObservedOrderSequence(
		selection,
		expectedDocument,
		expectedRows,
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
		precedenceChanges: changes,
	}, nil
}

func newObservedOrderSequence(
	selection orderSelection,
	document Document,
	rows []observerelation.ObservedRelationRow,
) (observerelation.ObservedRelationSequence, error) {
	sequenceID, err := sequenceIDForDocument(selection.capability, document)
	if err != nil {
		return observerelation.ObservedRelationSequence{}, err
	}
	authority, err := observerelation.NewSequenceAuthority(
		"opencode:" + string(selection.scope) + ":" +
			string(document.kind) + "." + configVariant(document.path) + ".plugins",
	)
	if err != nil {
		return observerelation.ObservedRelationSequence{}, err
	}
	revision, err := observerelation.NewSequenceRevision(document.revision)
	if err != nil {
		return observerelation.ObservedRelationSequence{}, err
	}
	return observerelation.NewObservedRelationSequence(
		selection.constraint.ClassID(),
		sequenceID,
		authority,
		revision,
		rows,
	)
}

func sequenceIDForDocument(
	capability profile.ExtensionOrderCapability,
	document Document,
) (hostrelation.PhysicalSequenceID, error) {
	variant := configVariant(document.path)
	if variant == "" {
		return "", fmt.Errorf("OpenCode plugin order document has unsupported config variant")
	}
	suffix := ":" + string(document.kind) + "." + variant + ".plugins"
	for _, sequenceID := range capability.PhysicalSequenceIDs() {
		if strings.HasSuffix(string(sequenceID), suffix) {
			return sequenceID, nil
		}
	}
	return "", fmt.Errorf(
		"OpenCode plugin order capability has no %s %s physical sequence",
		document.kind,
		variant,
	)
}

func configVariant(path string) string {
	switch filepath.Ext(path) {
	case ".json":
		return "json"
	case ".jsonc":
		return "jsonc"
	default:
		return ""
	}
}

func documentFromContent(
	baseline Document,
	content []byte,
	exists bool,
) (Document, error) {
	return documentFromSnapshot(
		baseline.kind,
		baseline.path,
		bytes.Clone(content),
		exists,
	)
}

func equalObservedSequence(
	left observerelation.ObservedRelationSequence,
	right observerelation.ObservedRelationSequence,
) bool {
	return left.ClassID() == right.ClassID() &&
		left.SequenceID() == right.SequenceID() &&
		left.Authority() == right.Authority() &&
		left.Revision() == right.Revision() &&
		slices.Equal(left.OrderedRows(), right.OrderedRows())
}
