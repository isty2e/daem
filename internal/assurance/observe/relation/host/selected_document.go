package relationhost

import (
	"context"
	"fmt"
	"slices"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
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
	piSelectedOrigin       struct{ observation pipackage.SelectedOrderView }
	openCodeSelectedOrigin struct {
		observation opencodeplugin.SelectedDocumentOrderView
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
	keys, err := retiringCorrelations(claims, target.TargetPi, origin.observation.Scope(), desiredextension.CarrierPiPackage)
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
	keys, err := retiringCorrelations(claims, target.TargetOpenCode, origin.observation.Scope(), desiredextension.CarrierOpenCodePlugin)
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

func NewPiDocumentEdit(view pipackage.SelectedOrderView) (SelectedDocumentEdit, error) {
	selected := view.SelectedSequence()
	address, err := aggregate.NewDocumentAddress(target.TargetPi, view.Scope(), selected.Projection().Destination())
	if err != nil {
		return SelectedDocumentEdit{}, err
	}
	edit := SelectedDocumentEdit{selected: selected, document: address, original: view.OriginalDocument(), path: view.SettingsPath(), maximumBytes: pipackage.MaximumSettingsBytes, origin: piSelectedOrigin{view}}
	if err := edit.Validate(); err != nil {
		return SelectedDocumentEdit{}, err
	}
	return edit, nil
}

func NewOpenCodeDocumentEdit(view opencodeplugin.SelectedDocumentOrderView) (SelectedDocumentEdit, error) {
	selected := view.SelectedSequence()
	address, err := aggregate.NewDocumentAddress(target.TargetOpenCode, view.Scope(), selected.Projection().Destination())
	if err != nil {
		return SelectedDocumentEdit{}, err
	}
	edit := SelectedDocumentEdit{selected: selected, document: address, original: view.OriginalDocument(), path: view.Path(), maximumBytes: opencodeplugin.MaximumConfigBytes, origin: openCodeSelectedOrigin{view}}
	if err := edit.Validate(); err != nil {
		return SelectedDocumentEdit{}, err
	}
	return edit, nil
}

// SelectedEdits consumes only operation-selected retirement claims. Normalizers
// derive exact row positions; callers supply neither slots nor loose subject joins.
func (observation OrderObservation) SelectedEdits(retiring []durablecarrier.ManagedCarrierClaim) ([]SelectedDocumentEdit, error) {
	var result []SelectedDocumentEdit
	if observation.pi != nil {
		original, err := observation.pi.CaptureInput()
		if err != nil {
			return nil, err
		}
		keys, err := retiringCorrelations(retiring, target.TargetPi, original.Scope, desiredextension.CarrierPiPackage)
		if err != nil {
			return nil, err
		}
		var relations []pipackage.ScopedRelation
		for _, key := range keys {
			if len(original.Relations) == 0 {
				return nil, fmt.Errorf("Pi ordered retirement requires its selected command context")
			}
			context, explicit := original.Relations[0].SourceContext()
			if !explicit {
				return nil, fmt.Errorf("Pi retirement command context is not explicit")
			}
			relation, err := pipackage.NewScopedRelationWithContext(key, original.Scope, context)
			if err != nil {
				return nil, err
			}
			relations = append(relations, relation)
		}
		view, err := observation.pi.WithRetiringRelations(relations)
		if err != nil {
			return nil, err
		}
		edit, err := NewPiDocumentEdit(view)
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
			view, err := original.WithRetiringRelations(relations)
			if err != nil {
				return nil, err
			}
			edit, err := NewOpenCodeDocumentEdit(view)
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
) ([]SelectedDocumentEdit, error) {
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
	var desired []opencodeplugin.ScopedRelation
	for _, record := range records {
		if record.scope != scope || record.carrier != desiredextension.CarrierOpenCodePlugin {
			continue
		}
		relation, err := opencodeplugin.NewScopedRelation(record.key, scope)
		if err != nil {
			return nil, err
		}
		desired = append(desired, relation)
	}
	inventory, err := opencodeplugin.ReadInventory(opencodeplugin.InventoryInput{ManifestRoot: paths.ManifestRoot, Scope: scope})
	if err != nil {
		return nil, err
	}
	var edits []SelectedDocumentEdit
	for _, document := range inventory.Documents() {
		view, err := opencodeplugin.ReconstructRetirement(capability, document, desired, relations)
		if err != nil {
			return nil, err
		}
		edit, err := NewOpenCodeDocumentEdit(view)
		if err != nil {
			return nil, err
		}
		edits = append(edits, edit)
	}
	return edits, nil
}

func openCodeRetiringRelations(retiring []durablecarrier.ManagedCarrierClaim, scope target.Scope) ([]opencodeplugin.ScopedRelation, error) {
	keys, err := retiringCorrelations(retiring, target.TargetOpenCode, scope, desiredextension.CarrierOpenCodePlugin)
	if err != nil {
		return nil, err
	}
	var relations []opencodeplugin.ScopedRelation
	for _, key := range keys {
		relation, err := opencodeplugin.NewScopedRelation(key, scope)
		if err != nil {
			return nil, err
		}
		relations = append(relations, relation)
	}
	return relations, nil
}

func retiringCorrelations(claims []durablecarrier.ManagedCarrierClaim, selectedTarget target.Target, scope target.Scope, family desiredextension.Carrier) ([]observerelation.CorrelationKey, error) {
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

type CompletedSequence struct {
	document         aggregate.DocumentAddress
	edit             configdocument.JSONArrayEdit
	identityContract string
}

func (sequence CompletedSequence) DocumentAddress() aggregate.DocumentAddress {
	return sequence.document
}
func (sequence CompletedSequence) ArrayEdit() configdocument.JSONArrayEdit { return sequence.edit }

// VerifiedSelectedImage is complete pure evidence, not a write capability. Its
// expected image and positional witnesses exist only after every normalizer passes.
type VerifiedSelectedImage struct {
	original     aggregate.Document
	expected     aggregate.Document
	participants []compose.Participant
	sequences    []CompletedSequence
	limits       configdocument.Limits
	initialized  bool
}

func (completed VerifiedSelectedImage) Validate() error {
	if !completed.initialized {
		return fmt.Errorf("complete verified selected image is required")
	}
	return nil
}

func (completed VerifiedSelectedImage) Original() aggregate.Document  { return completed.original }
func (completed VerifiedSelectedImage) Expected() aggregate.Document  { return completed.expected }
func (completed VerifiedSelectedImage) Limits() configdocument.Limits { return completed.limits }
func (completed VerifiedSelectedImage) Participants() []compose.Participant {
	return slices.Clone(completed.participants)
}

func (completed VerifiedSelectedImage) Sequences() []CompletedSequence {
	return slices.Clone(completed.sequences)
}

func CompleteSelectedImage(ctx context.Context, before aggregate.Document, limits configdocument.Limits, projections []compose.ProjectionEdit, selected []SelectedDocumentEdit) (VerifiedSelectedImage, error) {
	for _, edit := range selected {
		if err := edit.Validate(); err != nil {
			return VerifiedSelectedImage{}, err
		}
		if before.ContentSize() > edit.maximumBytes {
			return VerifiedSelectedImage{}, fmt.Errorf("selected sequence original exceeds its host document byte limit")
		}
		if !before.Equal(edit.original) {
			return VerifiedSelectedImage{}, fmt.Errorf("selected sequence does not describe the original document")
		}
	}
	if err := compose.ValidateImage(ctx, before, limits); err != nil {
		return VerifiedSelectedImage{}, err
	}
	var arrays []configdocument.JSONArrayEdit
	var sequences []CompletedSequence
	for _, edit := range selected {
		if !edit.Changed() {
			continue
		}
		syntax, err := configdocument.ParseSyntax(string(edit.selected.Projection().Grammar()))
		if err != nil {
			return VerifiedSelectedImage{}, err
		}
		loaded, err := configdocument.Parse(ctx, before.Content(), syntax, limits)
		if err != nil {
			return VerifiedSelectedImage{}, err
		}
		array, err := configdocument.PrepareJSONArrayEdit(ctx, loaded, edit.selected.Projection().ArrayPath(), edit.selected.RemovedSlots(), edit.selected.SurvivorOrder())
		if err != nil {
			return VerifiedSelectedImage{}, err
		}
		duplicate := false
		for index, previous := range arrays {
			if previous.ArrayPath() != array.ArrayPath() {
				continue
			}
			if !previous.Equal(array) || sequences[index].identityContract != edit.selected.MemberIdentityContract() {
				return VerifiedSelectedImage{}, fmt.Errorf("selected classes have incompatible mutations of one sequence")
			}
			duplicate = true
		}
		if !duplicate {
			arrays = append(arrays, array)
			sequences = append(sequences, CompletedSequence{document: edit.document, edit: array, identityContract: edit.selected.MemberIdentityContract()})
		}
	}
	expected, participants, err := compose.PrepareSelectedImage(ctx, before, limits, projections, arrays)
	if err != nil {
		return VerifiedSelectedImage{}, err
	}
	for _, edit := range selected {
		if err := ctx.Err(); err != nil {
			return VerifiedSelectedImage{}, err
		}
		if err := edit.origin.verify(edit.selected, expected); err != nil {
			return VerifiedSelectedImage{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return VerifiedSelectedImage{}, err
	}
	return VerifiedSelectedImage{original: before, expected: expected, participants: participants, sequences: sequences, limits: limits, initialized: true}, nil
}
