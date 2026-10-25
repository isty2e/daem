package pipackage

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

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
		Scope: observation.selection.scope, SettingsContext: *observation.inventory.sourceContext,
		Document: document, Constraint: observation.selection.constraint,
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
