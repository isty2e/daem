package opencodeplugin

import (
	"github.com/isty2e/daem/internal/assurance/observe/relation/opencodeorder"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
)

// OrderInput selects one OpenCode scope, its exact desired order, and the
// relations eligible for correlation in every loaded physical document.
type OrderInput struct {
	Inventory  InventoryInput
	Capability profile.ExtensionOrderCapability
	Constraint hostrelation.RelationOrderConstraint
	Relations  []opencodeorder.ScopedRelation
}

// ReadOrder reads the selected inventory and normalizes its loaded documents.
func ReadOrder(input OrderInput) (opencodeorder.OrderObservation, error) {
	inventory, err := ReadInventory(input.Inventory)
	if err != nil {
		return opencodeorder.OrderObservation{}, err
	}
	return opencodeorder.ReconstructOrder(input.Capability, inventory, input.Constraint, input.Relations)
}
