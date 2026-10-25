package pipackage

import (
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/realization/aggregate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type LoadedOrderInput struct {
	Scope           target.Scope
	SettingsContext extensiontopology.LocalSourceContext
	Document        aggregate.Document
	Constraint      hostrelation.RelationOrderConstraint
	Relations       []ScopedRelation
}

// ReconstructOrder interprets loaded settings using explicit lexical contexts.
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
	selection, err := newOrderSelection(input.Scope, input.Constraint, input.Relations)
	if err != nil {
		return OrderObservation{}, err
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
