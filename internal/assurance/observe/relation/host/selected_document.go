package relationhost

import (
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/observe/relation/opencodeorder"
	"github.com/isty2e/daem/internal/assurance/observe/relation/piorder"
	"github.com/isty2e/daem/internal/assurance/observe/relation/selecteddoc"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func (observation OrderObservation) SelectedEdits(retiring []durablecarrier.ManagedCarrierClaim) ([]selecteddoc.SelectedDocumentEdit, error) {
	return observation.SelectedHandoffEdits(retiring, observerelation.OrderHandoffs{})
}

func (observation OrderObservation) SelectedHandoffEdits(retiring []durablecarrier.ManagedCarrierClaim, handoffs observerelation.OrderHandoffs) ([]selecteddoc.SelectedDocumentEdit, error) {
	var result []selecteddoc.SelectedDocumentEdit
	if observation.pi != nil {
		original, err := observation.pi.CaptureInput()
		if err != nil {
			return nil, err
		}
		keys, err := selecteddoc.RetiringCorrelations(retiring, target.TargetPi, original.Scope, desiredextension.CarrierPiPackage)
		if err != nil {
			return nil, err
		}
		var relations []piorder.ScopedRelation
		for _, key := range keys {
			if len(original.Relations) == 0 {
				return nil, fmt.Errorf("Pi ordered retirement requires its selected command context")
			}
			context := original.Relations[0].SourceContext()
			relation, err := piorder.NewScopedRelation(key, original.Scope, context)
			if err != nil {
				return nil, err
			}
			relations = append(relations, relation)
		}
		view, err := observation.pi.WithHandoffRetirements(relations, handoffs)
		if err != nil {
			return nil, err
		}
		edit, err := selecteddoc.NewPiDocumentEdit(view)
		if err != nil {
			return nil, err
		}
		result = append(result, edit)
	} else if observation.openCode != nil {
		for _, original := range observation.openCode.Documents() {
			relations, err := openCodeRetiringRelations(retiring, original.Scope())
			if err != nil {
				return nil, err
			}
			view, err := original.WithHandoffRetirements(relations, handoffs)
			if err != nil {
				return nil, err
			}
			edit, err := selecteddoc.NewOpenCodeDocumentEdit(view)
			if err != nil {
				return nil, err
			}
			result = append(result, edit)
		}
	} else {
		return nil, fmt.Errorf("order observation has no admitted document normalizer")
	}
	return result, nil
}

// ObserveOpenCodeRetirementEdits prepares removal-only edits when no locked
// order class remains. It uses the same inventory and exact-source normalizer
// as ordered selected edits, without inventing a desired constraint.
// Still-desired relations are protected without becoming an order class.
func ObserveOpenCodeRetirementEdits(
	paths daempaths.Paths,
	locked lock.File,
	scope target.Scope,
	retiring []durablecarrier.ManagedCarrierClaim,
) ([]selecteddoc.SelectedDocumentEdit, error) {
	return ObserveOpenCodeHandoffRetirementEdits(paths, locked, scope, retiring, observerelation.OrderHandoffs{})
}

func ObserveOpenCodeHandoffRetirementEdits(
	paths daempaths.Paths,
	locked lock.File,
	scope target.Scope,
	retiring []durablecarrier.ManagedCarrierClaim,
	handoffs observerelation.OrderHandoffs,
) ([]selecteddoc.SelectedDocumentEdit, error) {
	relations, err := openCodeRetiringRelations(retiring, scope)
	if err != nil || len(relations) == 0 {
		return nil, err
	}
	capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapability(target.TargetOpenCode, scope, desiredextension.CarrierOpenCodePlugin)
	if !admitted {
		return nil, fmt.Errorf("OpenCode retirement has no admitted scope capability")
	}
	selection, err := targetselection.ForAvailableTargets([]target.Target{target.TargetOpenCode}, nil)
	if err != nil {
		return nil, err
	}
	records, err := selectedCarrierRecords(locked, nil, selection)
	if err != nil {
		return nil, err
	}
	var desired []opencodeorder.ScopedRelation
	for _, record := range records {
		if record.scope != scope || record.carrier != desiredextension.CarrierOpenCodePlugin {
			continue
		}
		relation, err := opencodeorder.NewScopedRelation(record.key, scope)
		if err != nil {
			return nil, err
		}
		desired = append(desired, relation)
	}
	inventory, err := opencodeplugin.ReadInventory(opencodeplugin.InventoryInput{ManifestRoot: paths.ManifestRoot, Scope: scope})
	if err != nil {
		return nil, err
	}
	var edits []selecteddoc.SelectedDocumentEdit
	for _, document := range inventory.Documents() {
		view, err := opencodeorder.ReconstructHandoffRetirement(capability, document, desired, relations, handoffs)
		if err != nil {
			return nil, err
		}
		edit, err := selecteddoc.NewOpenCodeDocumentEdit(view)
		if err != nil {
			return nil, err
		}
		edits = append(edits, edit)
	}
	return edits, nil
}

func openCodeRetiringRelations(retiring []durablecarrier.ManagedCarrierClaim, scope target.Scope) ([]opencodeorder.ScopedRelation, error) {
	keys, err := selecteddoc.RetiringCorrelations(retiring, target.TargetOpenCode, scope, desiredextension.CarrierOpenCodePlugin)
	if err != nil {
		return nil, err
	}
	var relations []opencodeorder.ScopedRelation
	for _, key := range keys {
		relation, err := opencodeorder.NewScopedRelation(key, scope)
		if err != nil {
			return nil, err
		}
		relations = append(relations, relation)
	}
	return relations, nil
}
