package pipackage

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	localcontext "github.com/isty2e/daem/internal/topology/extension"
)

// SelectedOrderView cannot be passed to a historical16 order encoder. Its base
// observation remains unchanged while retired correlations select original slots.
type SelectedOrderView struct {
	selection orderSelection
	inventory Inventory
	retiring  []ScopedRelation
	selected  observerelation.SelectedSequenceEdit
}

type LoadedRetirementInput struct {
	Capability      profile.ExtensionOrderCapability
	SettingsContext localcontext.LocalSourceContext
	CommandContext  localcontext.LocalSourceContext
	Document        aggregate.Document
	Relations       []ScopedRelation
}

// ReconstructRetirement uses actual loaded bytes and current explicit lexical
// contexts, not an empty desired constraint or facts reconstructed from a journal.
func ReconstructRetirement(input LoadedRetirementInput) (SelectedOrderView, error) {
	if err := input.Capability.Validate(); err != nil {
		return SelectedOrderView{}, err
	}
	if input.Capability.Carrier() != desiredextension.CarrierPiPackage {
		return SelectedOrderView{}, fmt.Errorf("Pi retirement requires its package capability")
	}
	if err := input.SettingsContext.Validate(); err != nil {
		return SelectedOrderView{}, err
	}
	if err := input.CommandContext.Validate(); err != nil {
		return SelectedOrderView{}, err
	}
	if err := input.Document.Validate(); err != nil {
		return SelectedOrderView{}, err
	}
	if input.Document.ContentSize() > MaximumSettingsBytes {
		return SelectedOrderView{}, fmt.Errorf("Pi retirement image exceeds the settings byte limit")
	}
	for _, relation := range input.Relations {
		if relation.sourceContext == nil || *relation.sourceContext != input.CommandContext {
			return SelectedOrderView{}, fmt.Errorf("Pi retirement differs from the selected command context")
		}
	}
	context := input.SettingsContext
	inventory, err := inventoryFromContent(Inventory{scope: input.Capability.Scope(), settingsPath: filepath.Join(context.BaseRoot(), "settings.json"), settingsBase: context.BaseRoot(), sourceContext: &context}, input.Document.Content(), input.Document.Exists())
	if err != nil {
		return SelectedOrderView{}, err
	}
	return newSelectedOrderView(orderSelection{scope: input.Capability.Scope(), capability: input.Capability}, inventory, input.Relations)
}

func (observation OrderObservation) WithRetiringRelations(retiring []ScopedRelation) (SelectedOrderView, error) {
	if _, err := observation.CaptureInput(); err != nil {
		return SelectedOrderView{}, err
	}
	return newSelectedOrderView(observation.selection, observation.inventory, retiring)
}

func newSelectedOrderView(selection orderSelection, inventory Inventory, retiring []ScopedRelation) (SelectedOrderView, error) {
	desiredSources := make(map[string]bool, len(selection.relations))
	desiredIdentities := make(map[hostrelation.HostLoadIdentity]bool, len(selection.relations))
	for _, relation := range selection.relations {
		source, err := expectedSettingsSource(relation, inventory.settingsBase)
		if err != nil {
			return SelectedOrderView{}, err
		}
		desiredSources[source] = true
	}
	for _, member := range selection.constraint.Members() {
		desiredIdentities[member.HostLoadIdentity()] = true
	}
	retained := slices.Clone(retiring)
	slices.SortFunc(retained, func(left, right ScopedRelation) int {
		return strings.Compare(left.key.Subject().String(), right.key.Subject().String())
	})
	sources := make(map[string]topology.SubjectID, len(retained))
	identities := make(map[hostrelation.HostLoadIdentity]bool, len(retained))
	for index, relation := range retained {
		if err := relation.key.Validate(); err != nil {
			return SelectedOrderView{}, err
		}
		if relation.scope != selection.scope || relation.sourceContext == nil || index != 0 && retained[index-1].key.Subject() == relation.key.Subject() {
			return SelectedOrderView{}, fmt.Errorf("Pi retirement requires unique, scope-matched explicit correlations")
		}
		for _, desired := range selection.relations {
			if desired.sourceContext == nil || *relation.sourceContext != *desired.sourceContext {
				return SelectedOrderView{}, fmt.Errorf("Pi retirement differs from the selected command context")
			}
		}
		source, err := expectedSettingsSource(relation, inventory.settingsBase)
		if err != nil {
			return SelectedOrderView{}, err
		}
		identity, err := sourceIdentityForRelation(relation)
		if err != nil {
			return SelectedOrderView{}, err
		}
		load := hostrelation.HostLoadIdentity(identity.hostLoadIdentity(selection.scope))
		_, repeatedSource := sources[source]
		if desiredSources[source] || desiredIdentities[load] || repeatedSource || identities[load] {
			return SelectedOrderView{}, fmt.Errorf("Pi retirement overlaps desired or ambiguous source identity")
		}
		sources[source], identities[load] = relation.key.Subject(), true
	}
	entries, err := inventory.Entries()
	if err != nil {
		return SelectedOrderView{}, err
	}
	rows, err := normalizedOrderRows(selection, inventory)
	if err != nil {
		return SelectedOrderView{}, err
	}
	var removed []int
	for index, entry := range entries {
		subject, retiring := sources[entry.Source()]
		if !retiring {
			continue
		}
		rows[index], err = observerelation.NewCorrelatedObservedRelationRow(rows[index].HostLoadIdentity(), subject)
		if err != nil {
			return SelectedOrderView{}, err
		}
		removed = append(removed, index)
	}
	sequence, err := newObservedOrderSequence(selection, rows, inventory.revision)
	if err != nil {
		return SelectedOrderView{}, err
	}
	var selected observerelation.SelectedSequenceEdit
	if selection.constraint.MemberCount() == 0 {
		selected, err = observerelation.NewRetirementSequenceEdit(selection.capability, sequence, removed)
	} else {
		selected, err = observerelation.NewSelectedSequenceEdit(selection.capability, sequence, selection.constraint, removed)
	}
	if err != nil {
		return SelectedOrderView{}, err
	}
	return SelectedOrderView{selection: selection, inventory: inventory, retiring: retained, selected: selected}, nil
}

func (view SelectedOrderView) SelectedSequence() observerelation.SelectedSequenceEdit {
	return view.selected
}

func (view SelectedOrderView) OriginalDocument() aggregate.Document { return view.inventory.Document() }

func (view SelectedOrderView) SettingsPath() string { return view.inventory.SettingsPath() }
func (view SelectedOrderView) Scope() target.Scope  { return view.selection.scope }

func (view SelectedOrderView) AfterRetiring(correlations []observerelation.CorrelationKey) (SelectedOrderView, error) {
	if err := view.selected.Projection().Validate(); err != nil {
		return SelectedOrderView{}, err
	}
	if len(correlations) == 0 {
		return view, nil
	}
	selected := make([]observerelation.CorrelationKey, 0, len(view.retiring))
	for _, relation := range view.retiring {
		selected = append(selected, relation.key)
	}
	retained, err := observerelation.RemainingCorrelations(selected, correlations)
	if err != nil {
		return SelectedOrderView{}, err
	}
	var remaining []ScopedRelation
	for _, relation := range view.retiring {
		if _, keep := retained[relation.key]; keep {
			remaining = append(remaining, relation)
		}
	}
	return newSelectedOrderView(view.selection, view.inventory, remaining)
}

func (view SelectedOrderView) DefinitionRoots() []string {
	seen := make(map[string]bool)
	for _, relations := range [][]ScopedRelation{view.selection.relations, view.retiring} {
		for _, relation := range relations {
			if relation.sourceContext != nil {
				seen[relation.sourceContext.BaseRoot()] = true
			}
		}
	}
	roots := make([]string, 0, len(seen))
	for root := range seen {
		roots = append(roots, root)
	}
	slices.Sort(roots)
	return roots
}

func (view SelectedOrderView) SameSelection(other SelectedOrderView) bool {
	if !view.selected.SameIntent(other.selected) || view.inventory.settingsPath != other.inventory.settingsPath ||
		view.inventory.sourceContext == nil || other.inventory.sourceContext == nil || *view.inventory.sourceContext != *other.inventory.sourceContext {
		return false
	}
	return slices.EqualFunc(view.selection.relations, other.selection.relations, ScopedRelation.sameSelection) && slices.EqualFunc(view.retiring, other.retiring, ScopedRelation.sameSelection)
}

func (left ScopedRelation) sameSelection(right ScopedRelation) bool {
	return left.key.Subject() == right.key.Subject() && left.key.ExpectedRelation().Equal(right.key.ExpectedRelation()) && left.scope == right.scope &&
		left.sourceContext != nil && right.sourceContext != nil && *left.sourceContext == *right.sourceContext
}

func (view SelectedOrderView) VerifySelectedExpected(selected observerelation.SelectedSequenceEdit, expected aggregate.Document) error {
	if !view.selected.SameMutation(selected) || view.selected.Projection().Validate() != nil {
		return fmt.Errorf("Pi completed image does not have its selected normalizer edit")
	}
	if err := expected.Validate(); err != nil {
		return err
	}
	if expected.ContentSize() > MaximumSettingsBytes {
		return fmt.Errorf("Pi completed image exceeds the settings byte limit")
	}
	post, err := inventoryFromContent(view.inventory, expected.Content(), expected.Exists())
	if err != nil {
		return err
	}
	rows, err := normalizedOrderRows(view.selection, post)
	if err != nil {
		return err
	}
	observed, err := newObservedOrderSequence(view.selection, rows, post.revision)
	if err != nil {
		return err
	}
	want, err := newObservedOrderSequence(view.selection, selected.ExpectedRows(), post.revision)
	if err != nil {
		return err
	}
	if !observed.SameOrdering(want) {
		return fmt.Errorf("Pi completed image differs from its selected removal/order rows")
	}
	return nil
}
