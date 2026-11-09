package pipackage

import (
	"fmt"
	"slices"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// SettingsDocumentAddress identifies the independently selected Pi settings layer.
// It grants no filesystem observation or publication authority.
func SettingsDocumentAddress(scope target.Scope) (aggregate.DocumentAddress, error) {
	if scope != target.ScopeProject && scope != target.ScopeGlobal {
		return aggregate.DocumentAddress{}, fmt.Errorf("Pi settings scope is invalid")
	}
	capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapability(target.TargetPi, scope, desiredextension.CarrierPiPackage)
	projections := capability.NativeSequences()
	if !admitted || len(projections) != 1 {
		return aggregate.DocumentAddress{}, fmt.Errorf("Pi settings requires one admitted native sequence projection")
	}
	return aggregate.NewDocumentAddress(target.TargetPi, scope, projections[0].Destination())
}

func (observation OrderObservation) SelectedSequenceEdit() (observerelation.SelectedSequenceEdit, error) {
	return observerelation.NewSelectedSequenceEdit(observation.selection.capability, observation.sequence, observation.selection.constraint, nil)
}

// SameSelection compares replayable order intent, not observed bytes or candidates.
func (observation OrderObservation) SameSelection(other OrderObservation) bool {
	left, err := observation.CaptureInput()
	if err != nil {
		return false
	}
	right, err := other.CaptureInput()
	if err != nil || left.Scope != right.Scope || !left.Capability.Equal(right.Capability) ||
		left.SettingsContext != right.SettingsContext || !left.Constraint.Equal(right.Constraint) {
		return false
	}
	return slices.EqualFunc(left.Relations, right.Relations, func(a, b ScopedRelation) bool {
		return a.key.Subject() == b.key.Subject() && a.key.ExpectedRelation().Equal(b.key.ExpectedRelation()) &&
			a.scope == b.scope && a.sourceContext != nil && b.sourceContext != nil && *a.sourceContext == *b.sourceContext
	})
}

// CaptureInput projects the actual immutable observation, including its original
// document and explicit lexical contexts. Ambient observations are not replayable.
func (observation OrderObservation) CaptureInput() (LoadedOrderInput, error) {
	if observation.inventory.sourceContext == nil {
		return LoadedOrderInput{}, fmt.Errorf("Pi order capture requires an explicit settings context")
	}
	for _, relation := range observation.selection.relations {
		if relation.sourceContext == nil {
			return LoadedOrderInput{}, fmt.Errorf("Pi order capture requires explicit relation contexts")
		}
	}
	if err := observation.Validate(); err != nil {
		return LoadedOrderInput{}, err
	}
	document := aggregate.AbsentDocument()
	if observation.inventory.exists {
		document = aggregate.ExistingDocument(observation.inventory.content)
	}
	return LoadedOrderInput{
		Scope: observation.selection.scope, Capability: observation.selection.capability,
		SettingsContext: *observation.inventory.sourceContext,
		Document:        document, Constraint: observation.selection.constraint,
		Relations: slices.Clone(observation.selection.relations),
	}, nil
}

// Project applies the observed fixed-slot order to an otherwise edited document.
// Its package array must still be the exact original; other content is retained.
func (observation OrderObservation) Project(document aggregate.Document) (aggregate.Document, error) {
	if _, err := observation.CaptureInput(); err != nil {
		return aggregate.Document{}, err
	}
	if err := document.Validate(); err != nil {
		return aggregate.Document{}, err
	}
	if document.ContentSize() > MaximumSettingsBytes {
		return aggregate.Document{}, fmt.Errorf("Pi settings exceeds %d bytes", MaximumSettingsBytes)
	}
	inventory, err := inventoryFromContent(observation.inventory, document.Content(), document.Exists())
	if err != nil {
		return aggregate.Document{}, err
	}
	if inventory.exists && observation.inventory.exists {
		if !inventory.document.PackageArrayEqual(observation.inventory.document) {
			return aggregate.Document{}, fmt.Errorf("Pi order projection package baseline changed")
		}
	} else if inventory.document.HasPackages() || observation.inventory.document.HasPackages() {
		return aggregate.Document{}, fmt.Errorf("Pi order projection package baseline changed")
	}
	projected, err := observeOrder(observation.selection, inventory)
	if err != nil {
		return aggregate.Document{}, err
	}
	if err := observation.VerifyPackageContent(projected.candidate, inventory.exists); err != nil {
		return aggregate.Document{}, err
	}
	if !inventory.exists {
		return aggregate.AbsentDocument(), nil
	}
	return aggregate.ExistingDocument(projected.candidate), nil
}
