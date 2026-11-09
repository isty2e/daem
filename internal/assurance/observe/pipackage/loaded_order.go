package pipackage

import (
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type LoadedOrderInput struct {
	Scope           target.Scope
	Capability      profile.ExtensionOrderCapability
	SettingsContext extensiontopology.LocalSourceContext
	Document        aggregate.Document
	Constraint      hostrelation.RelationOrderConstraint
	Relations       []ScopedRelation
}

// ReconstructOrder uses the supplied contract and explicit lexical contexts,
// without selecting a capability from the current catalog.
// It performs no filesystem observation and supplies no publication authority.
func ReconstructOrder(input LoadedOrderInput) (OrderObservation, error) {
	if err := input.SettingsContext.Validate(); err != nil {
		return OrderObservation{}, err
	}
	if err := input.Document.Validate(); err != nil {
		return OrderObservation{}, err
	}
	for index, relation := range input.Relations {
		if relation.sourceContext == nil {
			return OrderObservation{}, fmt.Errorf("Pi loaded order relation[%d] requires explicit source context", index)
		}
		if err := relation.sourceContext.Validate(); err != nil {
			return OrderObservation{}, err
		}
	}
	selection, err := newOrderSelectionWithCapability(input.Scope, input.Constraint, input.Relations, input.Capability)
	if err != nil {
		return OrderObservation{}, err
	}
	if input.Document.ContentSize() > MaximumSettingsBytes {
		return OrderObservation{}, fmt.Errorf("Pi settings exceeds %d bytes", MaximumSettingsBytes)
	}
	context := input.SettingsContext
	inventory, err := inventoryFromContent(Inventory{
		scope: input.Scope, settingsBase: context.BaseRoot(),
		settingsPath: filepath.Join(context.BaseRoot(), "settings.json"), sourceContext: &context,
	}, input.Document.Content(), input.Document.Exists())
	if err != nil {
		return OrderObservation{}, err
	}
	return observeOrder(selection, inventory)
}
