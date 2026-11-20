package selecteddoc

import (
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/observe/relation/opencodeorder"
	"github.com/isty2e/daem/internal/assurance/observe/relation/piorder"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// SelectedDocumentEdit owns one normalized planning observation and original
// CAS image. Only the complete-document factory can lower its positional edit.
type SelectedDocumentEdit struct {
	selected     observerelation.SelectedSequenceEdit
	document     aggregate.DocumentAddress
	original     aggregate.Document
	path         string
	maximumBytes int64
	origin       selectedDocumentOrigin
}

type selectedDocumentOrigin interface {
	same(selectedDocumentOrigin) bool
	verify(observerelation.SelectedSequenceEdit, aggregate.Document) error
	definitionRoots() []string
	afterRetiring([]durablecarrier.ManagedCarrierClaim) (SelectedDocumentEdit, error)
}

type (
	piSelectedOrigin       struct{ observation piorder.SelectedOrderView }
	openCodeSelectedOrigin struct {
		observation opencodeorder.SelectedDocumentOrderView
	}
)

func (origin piSelectedOrigin) same(other selectedDocumentOrigin) bool {
	value, same := other.(piSelectedOrigin)
	return same && origin.observation.SameSelection(value.observation)
}

func (origin piSelectedOrigin) verify(edit observerelation.SelectedSequenceEdit, expected aggregate.Document) error {
	return origin.observation.VerifySelectedExpected(edit, expected)
}

func (origin piSelectedOrigin) definitionRoots() []string {
	return origin.observation.DefinitionRoots()
}

func (origin piSelectedOrigin) afterRetiring(claims []durablecarrier.ManagedCarrierClaim) (SelectedDocumentEdit, error) {
	keys, err := RetiringCorrelations(claims, target.TargetPi, origin.observation.Scope(), desiredextension.CarrierPiPackage)
	if err != nil {
		return SelectedDocumentEdit{}, err
	}
	view, err := origin.observation.AfterRetiring(keys)
	if err != nil {
		return SelectedDocumentEdit{}, err
	}
	return NewPiDocumentEdit(view)
}

func (origin openCodeSelectedOrigin) same(other selectedDocumentOrigin) bool {
	value, same := other.(openCodeSelectedOrigin)
	return same && origin.observation.SameSelection(value.observation)
}

func (origin openCodeSelectedOrigin) verify(edit observerelation.SelectedSequenceEdit, expected aggregate.Document) error {
	return origin.observation.VerifySelectedExpected(edit, expected)
}

func (origin openCodeSelectedOrigin) definitionRoots() []string { return nil }

func (origin openCodeSelectedOrigin) afterRetiring(claims []durablecarrier.ManagedCarrierClaim) (SelectedDocumentEdit, error) {
	keys, err := RetiringCorrelations(claims, target.TargetOpenCode, origin.observation.Scope(), desiredextension.CarrierOpenCodePlugin)
	if err != nil {
		return SelectedDocumentEdit{}, err
	}
	view, err := origin.observation.AfterRetiring(keys)
	if err != nil {
		return SelectedDocumentEdit{}, err
	}
	return NewOpenCodeDocumentEdit(view)
}

func (edit SelectedDocumentEdit) Sequence() observerelation.ObservedRelationSequence {
	return edit.selected.Original()
}

func (edit SelectedDocumentEdit) SelectedSequence() observerelation.SelectedSequenceEdit {
	return edit.selected
}

func (edit SelectedDocumentEdit) DocumentAddress() aggregate.DocumentAddress { return edit.document }

func (edit SelectedDocumentEdit) Original() aggregate.Document { return edit.original }
func (edit SelectedDocumentEdit) SelectedPath() string         { return edit.path }
func (edit SelectedDocumentEdit) Scope() target.Scope          { return edit.document.Scope() }

func (edit SelectedDocumentEdit) Changed() bool { return edit.selected.Changed() }

func (edit SelectedDocumentEdit) MaximumBytes() int64 { return edit.maximumBytes }

func (edit SelectedDocumentEdit) DefinitionRoots() []string {
	if edit.origin == nil {
		return nil
	}
	return edit.origin.definitionRoots()
}

func (edit SelectedDocumentEdit) Validate() error {
	if edit.origin == nil || edit.maximumBytes <= 0 || edit.path == "" {
		return fmt.Errorf("selected document edit requires its private normalizer factory")
	}
	if err := edit.document.Validate(); err != nil {
		return err
	}
	if err := edit.original.Validate(); err != nil {
		return err
	}
	return edit.selected.Projection().Validate()
}

func (edit SelectedDocumentEdit) SameIntent(other SelectedDocumentEdit) bool {
	return edit.origin != nil && other.origin != nil && edit.document == other.document && edit.path == other.path &&
		edit.selected.SameIntent(other.selected) && edit.origin.same(other.origin)
}

func (edit SelectedDocumentEdit) SameMutation(other SelectedDocumentEdit) bool {
	return edit.SameIntent(other) && edit.original.Equal(other.original) && edit.selected.SameMutation(other.selected)
}

// AfterRetiringClaims projects a later reservation from selected exact claims.
// It retains the original CAS image and does not prove that retirement occurred.
// Claims outside this target, scope or carrier family are ignored; relevant
// unselected or repeated expectations fail. Callers supply only claims whose
// retirement precedes the reservation's binding phase.
func (edit SelectedDocumentEdit) AfterRetiringClaims(claims []durablecarrier.ManagedCarrierClaim) (SelectedDocumentEdit, error) {
	if err := edit.Validate(); err != nil {
		return SelectedDocumentEdit{}, err
	}
	return edit.origin.afterRetiring(claims)
}

func NewPiDocumentEdit(view piorder.SelectedOrderView) (SelectedDocumentEdit, error) {
	selected := view.SelectedSequence()
	address, err := aggregate.NewDocumentAddress(target.TargetPi, view.Scope(), selected.Projection().Destination())
	if err != nil {
		return SelectedDocumentEdit{}, err
	}
	edit := SelectedDocumentEdit{selected: selected, document: address, original: view.OriginalDocument(), path: view.SettingsPath(), maximumBytes: piorder.MaximumSettingsBytes, origin: piSelectedOrigin{view}}
	if err := edit.Validate(); err != nil {
		return SelectedDocumentEdit{}, err
	}
	return edit, nil
}

func NewOpenCodeDocumentEdit(view opencodeorder.SelectedDocumentOrderView) (SelectedDocumentEdit, error) {
	selected := view.SelectedSequence()
	address, err := aggregate.NewDocumentAddress(target.TargetOpenCode, view.Scope(), selected.Projection().Destination())
	if err != nil {
		return SelectedDocumentEdit{}, err
	}
	edit := SelectedDocumentEdit{selected: selected, document: address, original: view.OriginalDocument(), path: view.Path(), maximumBytes: opencodeorder.MaximumConfigBytes, origin: openCodeSelectedOrigin{view}}
	if err := edit.Validate(); err != nil {
		return SelectedDocumentEdit{}, err
	}
	return edit, nil
}

// SelectedEdits consumes only operation-selected retirement claims. Normalizers
// derive exact row positions; callers supply neither slots nor loose subject joins.
func RetiringCorrelations(claims []durablecarrier.ManagedCarrierClaim, selectedTarget target.Target, scope target.Scope, family desiredextension.Carrier) ([]observerelation.CorrelationKey, error) {
	var result []observerelation.CorrelationKey
	for _, claim := range claims {
		if err := claim.Validate(); err != nil {
			return nil, err
		}
		identity := claim.Identity()
		if identity.Target() != selectedTarget || identity.Scope() != scope || identity.Carrier().Family() != family {
			continue
		}
		key, err := observerelation.NewCorrelationKey(identity.RelationSubject(), identity.ExpectedRelation())
		if err != nil {
			return nil, err
		}
		result = append(result, key)
	}
	return result, nil
}
