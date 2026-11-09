// Package configuration owns static native document contracts. It does not
// observe files, resolve physical roots or grant mutation authority.
package configuration

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

var (
	ErrNoNativeSurface = errors.New("no native configuration surface in this scope")
	ErrDocumentPath    = errors.New("native document path is not admitted")
	ErrGrammar         = errors.New("native document grammar is not admitted")
)

type Grammar string

const (
	Opaque           Grammar = "opaque"
	JSON             Grammar = "json"
	JSONC            Grammar = "jsonc"
	JSONLineComments Grammar = "json-line-comments"
	TOML             Grammar = "toml"
)

type DocumentClass string

const (
	GenericConfiguration DocumentClass = "generic_configuration"
	SharedConfiguration  DocumentClass = "shared_configuration"
	ModelCatalog         DocumentClass = "model_catalog"
	Preferences          DocumentClass = "preferences"
)

type Observation string

const (
	ReferentAliases       Observation = "referent_aliases"
	InstalledClaims       Observation = "installed_claims"
	PiLoader              Observation = "pi_loader_entries"
	CodexRuntimeLocations Observation = "codex_runtime_locations"
	ClaudeProjectLocation Observation = "claude_project_location"
	OpenCodeCandidates    Observation = "opencode_candidates"
)

// FileModePolicy describes creation, not authority to change an existing mode.
type FileModePolicy string

const PrivateCreation FileModePolicy = "private_creation"

type DocumentInput struct {
	Target target.Target
	Scope  target.Scope
	Path   string
	Syntax hostconfig.Syntax
}

type AliasInput struct {
	Target      target.Target
	Scope       target.Scope
	Destination output.Destination
	Syntax      hostconfig.Syntax
}

// AliasBinding retains the static address contract independently of selected
// filesystem facts, authoring admission and producer grammar.
type AliasBinding struct {
	location AliasLocation
	policy   string
}

func newAliasBinding(root rootFact, relative, policy string) (AliasBinding, error) {
	reference, err := root.reference()
	if err != nil {
		return AliasBinding{}, err
	}
	location, err := NewAliasLocation(reference, relative)
	if err != nil {
		return AliasBinding{}, err
	}
	binding := AliasBinding{location: location, policy: policy}
	if err := binding.Validate(); err != nil {
		return AliasBinding{}, err
	}
	return binding, nil
}

func (binding AliasBinding) Location() AliasLocation { return binding.location }
func (binding AliasBinding) RootReference() output.RootReference {
	return binding.location.RootReference()
}

func (binding AliasBinding) Coordinate() RootCoordinate { return binding.location.Coordinate() }
func (binding AliasBinding) PolicyID() string           { return binding.policy }

func (binding AliasBinding) Validate() error {
	if err := binding.location.Validate(); err != nil {
		return err
	}
	if binding.policy == "" {
		return fmt.Errorf("native alias binding requires its carrier or layout policy")
	}
	return nil
}

// DocumentSpec is declaration-independent. Its alias is a candidate address;
// each participant still needs its own selected-root and physical qualification.
type DocumentSpec struct {
	target              target.Target
	scope               target.Scope
	nativePath          string
	catalogPath         string
	binding             AliasBinding
	destination         output.Destination
	grammar             Grammar
	class               DocumentClass
	mode                FileModePolicy
	observations        observationSet
	slots               dedicatedSlotSet
	alternate           output.Destination
	outsideNativeLayout bool
}

// RootBindingForAlias projects the alias's static root, path and policy without
// imposing placement or grammar admission on retained namespace addresses.
func RootBindingForAlias(input AliasInput) (AliasBinding, error) {
	if err := input.Destination.ValidateScope(input.Scope); err != nil {
		return AliasBinding{}, err
	}
	var root rootFact
	var relative, policy string
	if dedicated, matched := dedicatedAliasFor(input); matched {
		root, relative, policy = dedicated.root, dedicated.relative, dedicated.policy
	} else {
		layout, err := selectLayout(input.Target, input.Scope)
		if err != nil {
			return AliasBinding{}, err
		}
		var matched bool
		relative, matched = layout.bindingRelativePathFor(input.Destination)
		if !matched {
			return AliasBinding{}, fmt.Errorf("%w: alias %q has no native root binding", ErrDocumentPath, input.Destination.String())
		}
		root, policy = layout.root, layout.policy
	}
	return newAliasBinding(root, relative, policy)
}

// AdmitRecordedAliasBinding selects the recorded rule family, then checks its
// exact static projection. Unknown revisions are not reinterpreted as current.
func AdmitRecordedAliasBinding(input AliasInput, reference output.RootReference, coordinate RootCoordinate, policy string) (AliasBinding, error) {
	if err := input.Destination.ValidateScope(input.Scope); err != nil {
		return AliasBinding{}, err
	}
	var canonical AliasBinding
	var err error
	for _, dedicated := range dedicatedDocumentAliases {
		if dedicated.policy == policy && dedicated.target == input.Target && dedicated.scope == input.Scope && dedicated.alias == input.Destination.String() {
			canonical, err = newAliasBinding(dedicated.root, dedicated.relative, dedicated.policy)
			break
		}
	}
	if canonical == (AliasBinding{}) && err == nil {
		for key, layout := range nativeLayouts {
			if layout.policy != policy || key.target != input.Target || key.scope != input.Scope {
				continue
			}
			if path, matched := layout.bindingRelativePathFor(input.Destination); matched {
				canonical, err = newAliasBinding(layout.root, path, layout.policy)
			}
			break
		}
	}
	if err != nil {
		return AliasBinding{}, err
	}
	if canonical == (AliasBinding{}) || canonical.RootReference() != reference || canonical.Coordinate() != coordinate || canonical.policy != policy {
		return AliasBinding{}, fmt.Errorf("%w: recorded alias binding does not match its versioned contract", ErrDocumentPath)
	}
	return canonical, nil
}

func NewDocumentSpec(input DocumentInput) (DocumentSpec, error) {
	layout, err := selectLayout(input.Target, input.Scope)
	if err != nil {
		return DocumentSpec{}, err
	}
	if err := hostconfig.ValidateRelativePath(input.Path); err != nil {
		return DocumentSpec{}, err
	}
	if !layout.contains(input.Path) {
		return DocumentSpec{}, fmt.Errorf("%w: %q is outside its native layout", ErrDocumentPath, input.Path)
	}
	destination, err := output.NewDestination(layout.aliasRoot, layout.aliasDirectory+input.Path)
	if err != nil {
		return DocumentSpec{}, err
	}
	relative, matched := layout.bindingRelativePathFor(destination)
	if !matched {
		return DocumentSpec{}, fmt.Errorf("%w: native document has no root binding", ErrDocumentPath)
	}
	return buildDocument(input, layout.root, relative, destination, layout.policy, layout.observations, false)
}

func DocumentSpecForAlias(input AliasInput) (DocumentSpec, error) {
	if err := input.Destination.ValidateScope(input.Scope); err != nil {
		return DocumentSpec{}, err
	}
	if dedicated, matched := dedicatedAliasFor(input); matched {
		return buildDocument(DocumentInput{Target: input.Target, Scope: input.Scope, Path: dedicated.relative, Syntax: input.Syntax},
			dedicated.root, dedicated.relative, input.Destination, dedicated.policy, dedicated.observations, true)
	}
	layout, err := selectLayout(input.Target, input.Scope)
	if err != nil {
		return DocumentSpec{}, err
	}
	nativePath, matched := layout.nativePathFor(input.Destination)
	if !matched {
		return DocumentSpec{}, fmt.Errorf("%w: alias %q is outside its native layout", ErrDocumentPath, input.Destination.String())
	}
	return NewDocumentSpec(DocumentInput{Target: input.Target, Scope: input.Scope, Path: nativePath, Syntax: input.Syntax})
}

func buildDocument(input DocumentInput, root rootFact, catalogPath string, destination output.Destination, policy string, observations observationSet, outsideNativeLayout bool) (DocumentSpec, error) {
	requested, err := hostconfig.ParseSyntax(string(input.Syntax))
	if err != nil {
		return DocumentSpec{}, err
	}
	binding, err := newAliasBinding(root, catalogPath, policy)
	if err != nil {
		return DocumentSpec{}, err
	}
	rule, err := documentRuleFor(input.Target, input.Scope, input.Path, outsideNativeLayout)
	if err != nil {
		return DocumentSpec{}, err
	}
	grammar, err := selectGrammar(rule.grammar, input.Path, requested)
	if err != nil {
		return DocumentSpec{}, err
	}
	spec := DocumentSpec{
		target: input.Target, scope: input.Scope, nativePath: input.Path, catalogPath: catalogPath,
		binding: binding, destination: destination,
		grammar: grammar, class: rule.class, mode: PrivateCreation,
		observations: observations | rule.observations, slots: rule.slots, outsideNativeLayout: outsideNativeLayout,
	}
	if rule.alternate != "" {
		layout, err := selectLayout(input.Target, input.Scope)
		if err != nil {
			return DocumentSpec{}, err
		}
		spec.alternate, err = output.NewDestination(layout.aliasRoot, layout.aliasDirectory+rule.alternate)
		if err != nil {
			return DocumentSpec{}, err
		}
	}
	return spec, nil
}

func selectGrammar(known Grammar, nativePath string, requested hostconfig.Syntax) (Grammar, error) {
	if known != "" {
		if requested != hostconfig.SyntaxDefault && Grammar(requested) != known {
			return "", fmt.Errorf("%w: %q requires %s", ErrGrammar, nativePath, known)
		}
		return known, nil
	}
	if requested != hostconfig.SyntaxDefault {
		return Grammar(requested), nil
	}
	switch strings.ToLower(path.Ext(nativePath)) {
	case ".json":
		return JSON, nil
	case ".jsonc":
		return JSONC, nil
	case ".toml":
		return TOML, nil
	default:
		return Opaque, nil
	}
}

func (spec DocumentSpec) Target() target.Target               { return spec.target }
func (spec DocumentSpec) Scope() target.Scope                 { return spec.scope }
func (spec DocumentSpec) RootReference() output.RootReference { return spec.binding.RootReference() }
func (spec DocumentSpec) RootRelativePath() string            { return spec.catalogPath }
func (spec DocumentSpec) Destination() output.Destination     { return spec.destination }
func (spec DocumentSpec) PolicyID() string                    { return spec.binding.PolicyID() }
func (spec DocumentSpec) Grammar() Grammar                    { return spec.grammar }
func (spec DocumentSpec) Class() DocumentClass                { return spec.class }
func (spec DocumentSpec) ModePolicy() FileModePolicy          { return spec.mode }
func (spec DocumentSpec) RequiredObservations() []Observation { return spec.observations.values() }

// RequireWriterGrammar allows only a producer language accepted by the physical
// document. Existing bytes still pass the selected producer's own parser.
func (spec DocumentSpec) RequireWriterGrammar(writer Grammar) error {
	compatible := writer != "" && writer != Opaque && writer == spec.grammar
	switch spec.grammar {
	case JSONC:
		compatible = writer == JSON || writer == JSONLineComments || writer == JSONC
	case JSONLineComments:
		compatible = writer == JSON || writer == JSONLineComments
	}
	if !compatible {
		return fmt.Errorf("%w: declared %s syntax cannot produce the %s syntax required by the physical document", ErrGrammar, writer, spec.grammar)
	}
	return nil
}

// NativePlacementPath inverts the generic native layout. It does not classify
// ordinary writer semantics or waive that writer's physical-alias agreement.
func (spec DocumentSpec) NativePlacementPath() (string, bool) {
	if spec.outsideNativeLayout || spec.binding.Validate() != nil {
		return "", false
	}
	return spec.nativePath, true
}

func (spec DocumentSpec) AlternateFile() (output.Destination, bool) {
	return spec.alternate, spec.alternate.Validate() == nil
}

func (spec DocumentSpec) Validate() error {
	var canonical DocumentSpec
	var err error
	syntax := hostconfig.SyntaxDefault
	if spec.grammar != Opaque {
		syntax = hostconfig.Syntax(spec.grammar)
	}
	if spec.outsideNativeLayout {
		canonical, err = DocumentSpecForAlias(AliasInput{Target: spec.target, Scope: spec.scope, Destination: spec.destination, Syntax: syntax})
	} else {
		canonical, err = NewDocumentSpec(DocumentInput{Target: spec.target, Scope: spec.scope, Path: spec.nativePath, Syntax: syntax})
	}
	if err != nil {
		return err
	}
	if canonical != spec {
		return fmt.Errorf("document spec differs from its canonical contract")
	}
	return nil
}
