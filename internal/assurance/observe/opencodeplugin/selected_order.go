package opencodeplugin

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/aggregate"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type SelectedDocumentOrderView struct {
	selection orderSelection
	document  Document
	retiring  []ScopedRelation
	selected  observerelation.SelectedSequenceEdit
}

func (observation DocumentOrderObservation) WithRetiringRelations(retiring []ScopedRelation) (SelectedDocumentOrderView, error) {
	if err := observation.Validate(); err != nil {
		return SelectedDocumentOrderView{}, err
	}
	return newSelectedDocumentView(observation.selection, observation.document, retiring)
}

func ReconstructRetirement(capability profile.ExtensionOrderCapability, document Document, desired, retiring []ScopedRelation) (SelectedDocumentOrderView, error) {
	if err := capability.Validate(); err != nil {
		return SelectedDocumentOrderView{}, err
	}
	if capability.Carrier() != desiredextension.CarrierOpenCodePlugin || !filepath.IsAbs(document.path) || filepath.Clean(document.path) != document.path {
		return SelectedDocumentOrderView{}, fmt.Errorf("OpenCode retirement requires its admitted capability and selected document")
	}
	protected := slices.Clone(desired)
	slices.SortFunc(protected, func(left, right ScopedRelation) int {
		return topology.CompareSubjectID(left.key.Subject(), right.key.Subject())
	})
	selection := orderSelection{scope: capability.Scope(), directory: filepath.Dir(document.path), capability: capability, relations: protected}
	return newSelectedDocumentView(selection, document, retiring)
}

func newSelectedDocumentView(selection orderSelection, document Document, retiring []ScopedRelation) (SelectedDocumentOrderView, error) {
	desiredIdentities := make(map[hostrelation.HostLoadIdentity]bool)
	for _, member := range selection.constraint.Members() {
		desiredIdentities[member.HostLoadIdentity()] = true
	}
	desiredSubjects := make(map[topology.SubjectID]bool, len(selection.relations))
	protectedIdentities := make(map[hostrelation.HostLoadIdentity]bool, len(selection.relations))
	for _, relation := range selection.relations {
		if err := relation.key.Validate(); err != nil {
			return SelectedDocumentOrderView{}, err
		}
		if relation.scope != selection.scope || desiredSubjects[relation.key.Subject()] {
			return SelectedDocumentOrderView{}, fmt.Errorf("OpenCode desired retirement protection requires unique scope-matched subjects")
		}
		identity, err := opencodeconfig.HostLoadIdentity(string(relation.key.ExpectedRelation().SubjectKey()), filepath.Join(selection.directory, "opencode.json"))
		if err != nil {
			return SelectedDocumentOrderView{}, err
		}
		load := hostrelation.HostLoadIdentity(identity)
		if protectedIdentities[load] {
			return SelectedDocumentOrderView{}, fmt.Errorf("OpenCode desired retirement protection has an ambiguous load identity")
		}
		desiredSubjects[relation.key.Subject()], protectedIdentities[load], desiredIdentities[load] = true, true, true
	}
	retained := slices.Clone(retiring)
	slices.SortFunc(retained, func(left, right ScopedRelation) int {
		return strings.Compare(left.key.Subject().String(), right.key.Subject().String())
	})
	sources := make(map[string]topology.SubjectID, len(retained))
	identities := make(map[hostrelation.HostLoadIdentity]bool, len(retained))
	for index, relation := range retained {
		if err := relation.key.Validate(); err != nil {
			return SelectedDocumentOrderView{}, err
		}
		if relation.scope != selection.scope || index != 0 && retained[index-1].key.Subject() == relation.key.Subject() {
			return SelectedDocumentOrderView{}, fmt.Errorf("OpenCode retirement requires unique scope-matched correlations")
		}
		source := string(relation.key.ExpectedRelation().SubjectKey())
		identity, err := opencodeconfig.HostLoadIdentity(source, filepath.Join(selection.directory, "opencode.json"))
		if err != nil {
			return SelectedDocumentOrderView{}, err
		}
		load := hostrelation.HostLoadIdentity(identity)
		_, desired := selection.exactSubjects[source]
		_, repeatedSource := sources[source]
		if desired || desiredSubjects[relation.key.Subject()] || desiredIdentities[load] || repeatedSource || identities[load] {
			return SelectedDocumentOrderView{}, fmt.Errorf("OpenCode retirement overlaps desired or ambiguous source identity")
		}
		sources[source], identities[load] = relation.key.Subject(), true
	}
	rows, err := normalizedDocumentRows(selection, document)
	if err != nil {
		return SelectedDocumentOrderView{}, err
	}
	var removed []int
	for index, entry := range document.entries {
		subject, retiring := sources[entry.source]
		if !retiring {
			continue
		}
		var err error
		rows[index], err = observerelation.NewCorrelatedObservedRelationRow(rows[index].HostLoadIdentity(), subject)
		if err != nil {
			return SelectedDocumentOrderView{}, err
		}
		removed = append(removed, index)
	}
	sequence, err := newObservedOrderSequence(selection, document, rows)
	if err != nil {
		return SelectedDocumentOrderView{}, err
	}
	var selected observerelation.SelectedSequenceEdit
	if selection.constraint.MemberCount() == 0 {
		selected, err = observerelation.NewRetirementSequenceEdit(selection.capability, sequence, removed)
	} else {
		selected, err = observerelation.NewSelectedSequenceEdit(selection.capability, sequence, selection.constraint, removed)
	}
	if err != nil {
		return SelectedDocumentOrderView{}, err
	}
	return SelectedDocumentOrderView{selection: selection, document: document, retiring: retained, selected: selected}, nil
}

func (view SelectedDocumentOrderView) SelectedSequence() observerelation.SelectedSequenceEdit {
	return view.selected
}

func (view SelectedDocumentOrderView) OriginalDocument() aggregate.Document {
	if !view.document.exists {
		return aggregate.AbsentDocument()
	}
	return aggregate.ExistingDocument(view.document.content)
}

func (view SelectedDocumentOrderView) Path() string        { return view.document.path }
func (view SelectedDocumentOrderView) Scope() target.Scope { return view.selection.scope }

func (view SelectedDocumentOrderView) SameSelection(other SelectedDocumentOrderView) bool {
	if !view.selected.SameIntent(other.selected) || view.document.path != other.document.path ||
		!maps.Equal(view.selection.exactSubjects, other.selection.exactSubjects) ||
		!slices.Equal(view.selection.relations, other.selection.relations) || len(view.retiring) != len(other.retiring) {
		return false
	}
	for index, relation := range view.retiring {
		otherRelation := other.retiring[index]
		if relation.key.Subject() != otherRelation.key.Subject() || !relation.key.ExpectedRelation().Equal(otherRelation.key.ExpectedRelation()) || relation.scope != otherRelation.scope {
			return false
		}
	}
	return true
}

func (view SelectedDocumentOrderView) VerifySelectedExpected(selected observerelation.SelectedSequenceEdit, expected aggregate.Document) error {
	if !view.selected.SameMutation(selected) || view.selected.Projection().Validate() != nil {
		return fmt.Errorf("OpenCode completed image does not have its selected normalizer edit")
	}
	if err := expected.Validate(); err != nil {
		return err
	}
	if expected.ContentSize() > MaximumConfigBytes {
		return fmt.Errorf("OpenCode completed image exceeds the config byte limit")
	}
	document, err := documentFromContent(view.document, expected.Content(), expected.Exists())
	if err != nil {
		return err
	}
	rows, err := normalizedDocumentRows(view.selection, document)
	if err != nil {
		return err
	}
	observed, err := newObservedOrderSequence(view.selection, document, rows)
	if err != nil {
		return err
	}
	want, err := newObservedOrderSequence(view.selection, document, selected.ExpectedRows())
	if err != nil {
		return err
	}
	if !observed.SameOrdering(want) {
		return fmt.Errorf("OpenCode completed image differs from its selected removal/order rows")
	}
	return nil
}
