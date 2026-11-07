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

// DocumentSpec is declaration-independent. Its alias is a candidate address;
// each participant still needs its own selected-root and physical qualification.
type DocumentSpec struct {
	target              target.Target
	scope               target.Scope
	nativePath          string
	root                output.RootReference
	relative            string
	destination         output.Destination
	policy              string
	grammar             Grammar
	class               DocumentClass
	mode                FileModePolicy
	observations        observationSet
	slots               dedicatedSlotSet
	alternate           output.Destination
	outsideNativeLayout bool
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
	return buildDocument(input, layout.root, layout.relativePrefix+input.Path, destination, layout.policy, layout.observations, false)
}

func DocumentSpecForAlias(input AliasInput) (DocumentSpec, error) {
	if err := input.Destination.ValidateScope(input.Scope); err != nil {
		return DocumentSpec{}, err
	}
	for _, dedicated := range dedicatedDocumentAliases {
		if dedicated.target == input.Target && dedicated.scope == input.Scope && dedicated.alias == input.Destination.String() {
			return buildDocument(DocumentInput{Target: input.Target, Scope: input.Scope, Path: dedicated.relative, Syntax: input.Syntax},
				dedicated.root, dedicated.relative, input.Destination, dedicated.policy, dedicated.observations, true)
		}
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

func buildDocument(input DocumentInput, root rootFact, relative string, destination output.Destination, policy string, observations observationSet, outsideNativeLayout bool) (DocumentSpec, error) {
	requested, err := hostconfig.ParseSyntax(string(input.Syntax))
	if err != nil {
		return DocumentSpec{}, err
	}
	reference, err := output.NewRootReference(root.identity, root.locator)
	if err != nil {
		return DocumentSpec{}, err
	}
	if err := hostconfig.ValidateRelativePath(relative); err != nil {
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
		target: input.Target, scope: input.Scope, nativePath: input.Path,
		root: reference, relative: relative, destination: destination, policy: policy,
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
func (spec DocumentSpec) RootReference() output.RootReference { return spec.root }
func (spec DocumentSpec) RootRelativePath() string            { return spec.relative }
func (spec DocumentSpec) Destination() output.Destination     { return spec.destination }
func (spec DocumentSpec) PolicyID() string                    { return spec.policy }
func (spec DocumentSpec) Grammar() Grammar                    { return spec.grammar }
func (spec DocumentSpec) Class() DocumentClass                { return spec.class }
func (spec DocumentSpec) ModePolicy() FileModePolicy          { return spec.mode }
func (spec DocumentSpec) RequiredObservations() []Observation { return spec.observations.values() }

// NativePlacementPath inverts the generic native layout. It does not classify
// ordinary writer semantics or waive that writer's physical-alias agreement.
func (spec DocumentSpec) NativePlacementPath() (string, bool) {
	if spec.outsideNativeLayout || spec.root.Validate() != nil {
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
