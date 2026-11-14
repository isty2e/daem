package pipackage

import (
	"bytes"
	"fmt"
	"slices"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// OrderInput selects one exact Pi package sequence and its locked members.
type OrderInput struct {
	Settings   SettingsInput
	Constraint hostrelation.RelationOrderConstraint
	Relations  []ScopedRelation
}

// OrderObservation owns one immutable baseline, its fixed-slot candidate, and
// exact postcondition evidence for a selected Pi settings sequence.
type OrderObservation struct {
	selection         orderSelection
	inventory         Inventory
	sequence          observerelation.ObservedRelationSequence
	expectedSequence  observerelation.ObservedRelationSequence
	candidate         []byte
	changed           bool
	precedenceChanges []observerelation.PrecedenceChange
}

type orderSelection struct {
	scope      target.Scope
	constraint hostrelation.RelationOrderConstraint
	capability profile.ExtensionOrderCapability
	relations  []ScopedRelation
}

// ReadOrder reads one selected Pi settings layer and derives its exact
// fixed-slot normalization candidate.
func ReadOrder(input OrderInput) (OrderObservation, error) {
	selection, err := newOrderSelection(
		input.Settings.Scope,
		input.Constraint,
		input.Relations,
	)
	if err != nil {
		return OrderObservation{}, err
	}
	inventory, err := ReadSettings(input.Settings)
	if err != nil {
		return OrderObservation{}, err
	}
	return observeOrder(selection, inventory)
}

// Validate rejects a zero or forged observation.
func (observation OrderObservation) Validate() error {
	expected, err := observeOrder(observation.selection, observation.inventory)
	if err != nil {
		return err
	}
	if !equalObservedSequence(observation.sequence, expected.sequence) ||
		!equalObservedSequence(observation.expectedSequence, expected.expectedSequence) ||
		!bytes.Equal(observation.candidate, expected.candidate) ||
		observation.changed != expected.changed ||
		!slices.Equal(observation.precedenceChanges, expected.precedenceChanges) {
		return fmt.Errorf("Pi package order observation is not canonical")
	}
	return nil
}

// Scope returns the independently selected Pi settings layer.
func (observation OrderObservation) Scope() target.Scope {
	return observation.selection.scope
}

// SettingsPath returns the selected path, not proof of a filesystem read.
func (observation OrderObservation) SettingsPath() string {
	return observation.inventory.settingsPath
}

// Sequence returns the immutable observed physical order.
func (observation OrderObservation) Sequence() observerelation.ObservedRelationSequence {
	return observation.sequence
}

// ExpectedSequence returns the exact sequence represented by Candidate.
func (observation OrderObservation) ExpectedSequence() observerelation.ObservedRelationSequence {
	return observation.expectedSequence
}

// Changed reports whether the candidate differs from the baseline bytes.
func (observation OrderObservation) Changed() bool { return observation.changed }

// Candidate returns owned candidate bytes and whether the selected settings
// file existed. Missing input remains a non-creating no-op.
func (observation OrderObservation) Candidate() ([]byte, bool) {
	return bytes.Clone(observation.candidate), observation.inventory.exists
}

// VerifyBaseline checks exact existence and content revision before mutation.
func (observation OrderObservation) VerifyBaseline(content []byte, exists bool) error {
	if exists != observation.inventory.exists {
		return fmt.Errorf(
			"Pi settings baseline existence changed from %t to %t",
			observation.inventory.exists,
			exists,
		)
	}
	if settingsRevision(content) != observation.inventory.revision {
		return fmt.Errorf("Pi settings baseline revision changed")
	}
	return nil
}

// VerifyPostContent reparses exact post-read bytes and requires the expected
// canonical sequence. File-write success alone is not convergence evidence.
func (observation OrderObservation) VerifyPostContent(content []byte, exists bool) error {
	candidate, expectedExists := observation.Candidate()
	if exists != expectedExists || !bytes.Equal(content, candidate) {
		return fmt.Errorf("Pi settings post-observation does not match the exact candidate")
	}
	post, err := observation.postObservation(content, exists)
	if err != nil {
		return err
	}
	if !equalObservedSequence(post.sequence, observation.expectedSequence) {
		return fmt.Errorf("Pi settings post-observation sequence does not match the expected order")
	}
	return nil
}

// VerifyPackageContent checks only the exact package array and canonical order.
// A combined writer must independently verify its final whole-document candidate;
// this check neither verifies the root revision nor authorizes a file write.
func (observation OrderObservation) VerifyPackageContent(content []byte, exists bool) error {
	if err := observation.selection.constraint.Validate(); err != nil {
		return fmt.Errorf("Pi package order observation: %w", err)
	}
	if !exists && len(content) != 0 {
		return fmt.Errorf("absent Pi settings post-observation contains bytes")
	}
	post, err := observation.postObservation(content, exists)
	if err != nil {
		return err
	}

	var expectedDocument piconfig.Document
	if observation.inventory.exists {
		expectedDocument, err = piconfig.Parse(observation.candidate)
		if err != nil {
			return fmt.Errorf("parse Pi settings expected candidate: %w", err)
		}
	}
	if exists != observation.inventory.exists {
		if expectedDocument.HasPackages() || post.inventory.document.HasPackages() {
			return fmt.Errorf("Pi settings package array presence changed")
		}
	} else if exists && !expectedDocument.PackageArrayEqual(post.inventory.document) {
		return fmt.Errorf("Pi settings package array does not match the exact candidate")
	}
	if !post.sequence.SameOrdering(observation.expectedSequence) {
		return fmt.Errorf("Pi settings post-observation sequence does not match the expected order")
	}
	return nil
}

func (observation OrderObservation) postObservation(content []byte, exists bool) (OrderObservation, error) {
	inventory, err := inventoryFromContent(observation.inventory, content, exists)
	if err != nil {
		return OrderObservation{}, fmt.Errorf("parse Pi settings post-observation: %w", err)
	}
	post, err := observeOrder(observation.selection, inventory)
	if err != nil {
		return OrderObservation{}, fmt.Errorf("normalize Pi settings post-observation: %w", err)
	}
	return post, nil
}

func newOrderSelection(
	scope target.Scope,
	constraint hostrelation.RelationOrderConstraint,
	relations []ScopedRelation,
) (orderSelection, error) {
	capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapability(
		target.TargetPi,
		scope,
		desiredextension.CarrierPiPackage,
	)
	if !admitted {
		return orderSelection{}, fmt.Errorf("Pi %s package order is not admitted", scope)
	}
	return newOrderSelectionWithCapability(scope, constraint, relations, capability)
}

func newOrderSelectionWithCapability(
	scope target.Scope,
	constraint hostrelation.RelationOrderConstraint,
	relations []ScopedRelation,
	capability profile.ExtensionOrderCapability,
) (orderSelection, error) {
	if err := constraint.Validate(); err != nil {
		return orderSelection{}, fmt.Errorf("Pi package order constraint: %w", err)
	}
	if err := capability.Validate(); err != nil {
		return orderSelection{}, err
	}
	if capability.Carrier() != desiredextension.CarrierPiPackage || capability.Scope() != scope ||
		capability.SequenceMembership() != profile.CompleteClassMembership {
		return orderSelection{}, fmt.Errorf("Pi package order requires its scoped complete-membership capability")
	}
	if constraint.ClassID() != capability.ClassID() ||
		constraint.MemberIdentityContract() != capability.MemberIdentityContract() ||
		constraint.RuntimeMeaning() != capability.RuntimeMeaning() {
		return orderSelection{}, fmt.Errorf(
			"Pi package order constraint does not match the %s profile capability",
			scope,
		)
	}
	if len(capability.PhysicalSequenceIDs()) != 1 {
		return orderSelection{}, fmt.Errorf("Pi %s package order requires one physical sequence", scope)
	}

	bySubject := make(map[topology.SubjectID]ScopedRelation, len(relations))
	for index, relation := range relations {
		if relation.scope != scope {
			return orderSelection{}, fmt.Errorf(
				"Pi package order relation[%d] scope %q does not match %q",
				index,
				relation.scope,
				scope,
			)
		}
		subject := relation.key.Subject()
		if _, duplicate := bySubject[subject]; duplicate {
			return orderSelection{}, fmt.Errorf(
				"Pi package order relation subject %q appears more than once",
				subject,
			)
		}
		bySubject[subject] = relation
	}

	ordered := make([]ScopedRelation, 0, len(constraint.Members()))
	for index, member := range constraint.Members() {
		relation, present := bySubject[member.Subject()]
		if !present {
			return orderSelection{}, fmt.Errorf(
				"Pi package order member[%d] subject %q has no exact relation",
				index,
				member.Subject(),
			)
		}
		identity, err := sourceIdentityForRelation(relation)
		if err != nil {
			return orderSelection{}, fmt.Errorf(
				"derive Pi package order member[%d] load identity: %w",
				index,
				err,
			)
		}
		loadIdentity := identity.hostLoadIdentity(scope)
		if hostrelation.HostLoadIdentity(loadIdentity) != member.HostLoadIdentity() {
			return orderSelection{}, fmt.Errorf(
				"Pi package order member[%d] host load identity %q does not match relation identity %q",
				index,
				member.HostLoadIdentity(),
				loadIdentity,
			)
		}
		ordered = append(ordered, relation)
		delete(bySubject, member.Subject())
	}
	if len(bySubject) != 0 {
		return orderSelection{}, fmt.Errorf("Pi package order contains relations outside its constraint")
	}
	return orderSelection{
		scope:      scope,
		constraint: constraint,
		capability: capability,
		relations:  ordered,
	}, nil
}

func normalizedOrderRows(
	selection orderSelection,
	inventory Inventory,
) ([]observerelation.ObservedRelationRow, error) {
	if inventory.scope != selection.scope {
		return nil, fmt.Errorf(
			"Pi package order inventory scope %q does not match %q",
			inventory.scope,
			selection.scope,
		)
	}
	exactSubjects := make(map[string]topology.SubjectID, len(selection.relations))
	for index, relation := range selection.relations {
		stored, err := expectedSettingsSource(relation, inventory.settingsBase)
		if err != nil {
			return nil, fmt.Errorf(
				"derive Pi package order relation[%d] stored source: %w",
				index,
				err,
			)
		}
		if previous, duplicate := exactSubjects[stored]; duplicate {
			return nil, fmt.Errorf(
				"Pi package order subjects %q and %q map to exact stored source %q",
				previous,
				relation.key.Subject(),
				stored,
			)
		}
		exactSubjects[stored] = relation.key.Subject()
	}

	entries, err := inventory.Entries()
	if err != nil {
		return nil, err
	}
	rows := make([]observerelation.ObservedRelationRow, 0, len(entries))
	for index, entry := range entries {
		loadIdentity, err := hostrelation.NewHostLoadIdentity(entry.HostLoadIdentity())
		if err != nil {
			return nil, fmt.Errorf(
				"normalize Pi package order row[%d] identity: %w",
				index,
				err,
			)
		}
		subject, exact := exactSubjects[entry.Source()]
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

func observeOrder(selection orderSelection, inventory Inventory) (OrderObservation, error) {
	rows, err := normalizedOrderRows(selection, inventory)
	if err != nil {
		return OrderObservation{}, err
	}
	sequence, err := newObservedOrderSequence(selection, rows, inventory.revision)
	if err != nil {
		return OrderObservation{}, err
	}
	selected, err := observerelation.NewSelectedSequenceEdit(selection.capability, sequence, selection.constraint, nil)
	if err != nil {
		return OrderObservation{}, err
	}

	candidate := bytes.Clone(inventory.content)
	changed := false
	if inventory.exists {
		candidate, changed, err = inventory.document.PermutePackageRows(selected.SurvivorOrder())
		if err != nil {
			return OrderObservation{}, err
		}
	}
	expectedSequence, err := newObservedOrderSequence(
		selection,
		selected.ExpectedRows(),
		settingsRevision(candidate),
	)
	if err != nil {
		return OrderObservation{}, err
	}

	return OrderObservation{
		selection:         selection,
		inventory:         inventory,
		sequence:          sequence,
		expectedSequence:  expectedSequence,
		candidate:         candidate,
		changed:           changed,
		precedenceChanges: selected.PrecedenceChanges(),
	}, nil
}

func newObservedOrderSequence(
	selection orderSelection,
	rows []observerelation.ObservedRelationRow,
	revisionValue string,
) (observerelation.ObservedRelationSequence, error) {
	sequenceIDs := selection.capability.PhysicalSequenceIDs()
	authority, err := observerelation.NewSequenceAuthority(
		"pi:" + string(selection.scope) + ":settings.packages",
	)
	if err != nil {
		return observerelation.ObservedRelationSequence{}, err
	}
	revision, err := observerelation.NewSequenceRevision(revisionValue)
	if err != nil {
		return observerelation.ObservedRelationSequence{}, err
	}
	return observerelation.NewObservedRelationSequence(
		selection.capability.ClassID(),
		sequenceIDs[0],
		authority,
		revision,
		rows,
	)
}

func inventoryFromContent(
	baseline Inventory,
	content []byte,
	exists bool,
) (Inventory, error) {
	if len(content) > MaximumSettingsBytes {
		return Inventory{}, fmt.Errorf("Pi settings exceeds %d bytes", MaximumSettingsBytes)
	}
	if !exists && len(content) != 0 {
		return Inventory{}, fmt.Errorf("absent Pi settings contains bytes")
	}
	var document piconfig.Document
	var err error
	if exists {
		document, err = piconfig.Parse(content)
		if err != nil {
			return Inventory{}, err
		}
	}
	return Inventory{
		scope:         baseline.scope,
		settingsPath:  baseline.settingsPath,
		settingsBase:  baseline.settingsBase,
		revision:      settingsRevision(content),
		exists:        exists,
		content:       bytes.Clone(content),
		document:      document,
		sourceContext: baseline.sourceContext,
	}, nil
}

func equalObservedSequence(
	left observerelation.ObservedRelationSequence,
	right observerelation.ObservedRelationSequence,
) bool {
	return left.SameOrdering(right) && left.Revision() == right.Revision()
}
