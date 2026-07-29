// Package effective observes provider-effective MCP configuration without
// acquiring write authority over provider or shared config sources.
package effective

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/topology"
)

// State classifies whether one provider-effective name is safe to project.
type State string

const (
	StateExact        State = "exact"
	StateConflicting  State = "conflicting"
	StateUnobservable State = "unobservable"
)

// SourceKind identifies how one active source enters provider evaluation.
type SourceKind string

const (
	SourceNormal        SourceKind = "normal"
	SourceImport        SourceKind = "import"
	SourceHostDiscovery SourceKind = "host_discovery"
)

// SourceState classifies the precision of one source observation.
type SourceState string

const (
	SourceAbsent SourceState = "absent"
	SourceExact  SourceState = "exact"
	SourceOpaque SourceState = "opaque"
)

// RelativePrecedence locates one source relative to the managed source.
type RelativePrecedence string

const (
	PrecedenceLower    RelativePrecedence = "lower"
	PrecedenceSelected RelativePrecedence = "selected"
	PrecedenceHigher   RelativePrecedence = "higher"
)

// DefinitionEquivalence classifies one exact same-name definition relative to
// the selected managed contribution.
type DefinitionEquivalence string

const (
	DefinitionEquivalenceNotApplicable DefinitionEquivalence = "not_applicable"
	DefinitionEquivalenceEquivalent    DefinitionEquivalence = "equivalent"
	DefinitionEquivalenceDifferent     DefinitionEquivalence = "different"
	DefinitionEquivalenceUnknown       DefinitionEquivalence = "unknown"
)

// SourceObservationInput is constructor input for one active config source.
type SourceObservationInput struct {
	ID                    string
	Path                  string
	Kind                  SourceKind
	Precedence            RelativePrecedence
	Shared                bool
	State                 SourceState
	DefinesSelectedName   bool
	DefinitionEquivalence DefinitionEquivalence
	Detail                string
}

// SourceObservation is redaction-safe provenance for one active config source.
type SourceObservation struct {
	id                    string
	path                  string
	kind                  SourceKind
	precedence            RelativePrecedence
	shared                bool
	state                 SourceState
	definesSelectedName   bool
	definitionEquivalence DefinitionEquivalence
	detail                string
}

// NewSourceObservation validates and constructs one source observation.
func NewSourceObservation(input SourceObservationInput) (SourceObservation, error) {
	if input.ID == "" || strings.TrimSpace(input.ID) != input.ID {
		return SourceObservation{}, fmt.Errorf("effective MCP source id must be non-empty and trimmed")
	}
	if input.Path == "" || !filepath.IsAbs(input.Path) || filepath.Clean(input.Path) != input.Path {
		return SourceObservation{}, fmt.Errorf("effective MCP source path %q must be absolute and clean", input.Path)
	}
	switch input.Kind {
	case SourceNormal, SourceImport, SourceHostDiscovery:
	default:
		return SourceObservation{}, fmt.Errorf("effective MCP source kind %q is unsupported", input.Kind)
	}
	switch input.Precedence {
	case PrecedenceLower, PrecedenceSelected, PrecedenceHigher:
	default:
		return SourceObservation{}, fmt.Errorf(
			"effective MCP source precedence %q is unsupported",
			input.Precedence,
		)
	}
	switch input.State {
	case SourceAbsent, SourceExact, SourceOpaque:
	default:
		return SourceObservation{}, fmt.Errorf("effective MCP source state %q is unsupported", input.State)
	}
	if input.State != SourceExact && input.DefinesSelectedName {
		return SourceObservation{}, fmt.Errorf(
			"effective MCP source %q cannot define a name without exact evidence",
			input.ID,
		)
	}
	switch {
	case !input.DefinesSelectedName &&
		input.DefinitionEquivalence != DefinitionEquivalenceNotApplicable:
		return SourceObservation{}, fmt.Errorf(
			"effective MCP source %q without a selected name requires not-applicable equivalence",
			input.ID,
		)
	case input.DefinesSelectedName:
		switch input.DefinitionEquivalence {
		case DefinitionEquivalenceEquivalent,
			DefinitionEquivalenceDifferent,
			DefinitionEquivalenceUnknown:
		default:
			return SourceObservation{}, fmt.Errorf(
				"effective MCP source %q with a selected name requires classified equivalence",
				input.ID,
			)
		}
	}
	if input.State == SourceOpaque && strings.TrimSpace(input.Detail) == "" {
		return SourceObservation{}, fmt.Errorf("opaque effective MCP source %q requires detail", input.ID)
	}
	if input.State != SourceOpaque && input.Detail != "" {
		return SourceObservation{}, fmt.Errorf(
			"non-opaque effective MCP source %q cannot carry failure detail",
			input.ID,
		)
	}
	return SourceObservation{
		id:                    input.ID,
		path:                  input.Path,
		kind:                  input.Kind,
		precedence:            input.Precedence,
		shared:                input.Shared,
		state:                 input.State,
		definesSelectedName:   input.DefinesSelectedName,
		definitionEquivalence: input.DefinitionEquivalence,
		detail:                input.Detail,
	}, nil
}

func (source SourceObservation) ID() string                     { return source.id }
func (source SourceObservation) Path() string                   { return source.path }
func (source SourceObservation) Kind() SourceKind               { return source.kind }
func (source SourceObservation) Precedence() RelativePrecedence { return source.precedence }
func (source SourceObservation) Shared() bool                   { return source.shared }
func (source SourceObservation) State() SourceState             { return source.state }
func (source SourceObservation) DefinesSelectedName() bool      { return source.definesSelectedName }

func (source SourceObservation) DefinitionEquivalence() DefinitionEquivalence {
	return source.definitionEquivalence
}
func (source SourceObservation) Detail() string { return source.detail }

// ObservationInput is constructor input for one provider-effective MCP name.
type ObservationInput struct {
	Subject      topology.SubjectID
	ServerName   string
	SelectedPath string
	Sources      []SourceObservation
}

// Observation is immutable provider-effective collision evidence for one
// locked MCP projection.
type Observation struct {
	subject      topology.SubjectID
	serverName   string
	selectedPath string
	sources      []SourceObservation
	state        State
}

// NewObservation validates source coverage and derives the effective state.
func NewObservation(input ObservationInput) (Observation, error) {
	if err := input.Subject.Validate(); err != nil {
		return Observation{}, fmt.Errorf("effective MCP subject: %w", err)
	}
	if input.ServerName == "" || strings.TrimSpace(input.ServerName) != input.ServerName {
		return Observation{}, fmt.Errorf("effective MCP server name must be non-empty and trimmed")
	}
	if input.SelectedPath == "" ||
		!filepath.IsAbs(input.SelectedPath) ||
		filepath.Clean(input.SelectedPath) != input.SelectedPath {
		return Observation{}, fmt.Errorf(
			"effective MCP selected path %q must be absolute and clean",
			input.SelectedPath,
		)
	}

	sources := append([]SourceObservation(nil), input.Sources...)
	selectedCount := 0
	seenIDs := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if _, duplicate := seenIDs[source.ID()]; duplicate {
			return Observation{}, fmt.Errorf("duplicate effective MCP source id %q", source.ID())
		}
		seenIDs[source.ID()] = struct{}{}
		if source.Precedence() != PrecedenceSelected {
			continue
		}
		if source.Kind() != SourceNormal || source.Path() != input.SelectedPath {
			return Observation{}, fmt.Errorf(
				"selected effective MCP source %q does not match managed path %q",
				source.ID(),
				input.SelectedPath,
			)
		}
		selectedCount++
	}
	if selectedCount != 1 {
		return Observation{}, fmt.Errorf(
			"effective MCP observation requires one selected normal source, got %d",
			selectedCount,
		)
	}

	state := StateExact
	for _, source := range sources {
		if source.State() == SourceOpaque {
			state = StateUnobservable
			break
		}
		if source.DefinesSelectedName() &&
			(source.Precedence() != PrecedenceSelected || source.Kind() != SourceNormal) {
			state = StateConflicting
		}
	}
	return Observation{
		subject:      input.Subject,
		serverName:   input.ServerName,
		selectedPath: input.SelectedPath,
		sources:      sources,
		state:        state,
	}, nil
}

func (observation Observation) Subject() topology.SubjectID { return observation.subject }
func (observation Observation) ServerName() string          { return observation.serverName }
func (observation Observation) SelectedPath() string        { return observation.selectedPath }
func (observation Observation) State() State                { return observation.state }

func (observation Observation) Sources() []SourceObservation {
	return append([]SourceObservation(nil), observation.sources...)
}

// BlockingSources returns opaque sources first, or exact same-name conflicts
// when every active source was observable.
func (observation Observation) BlockingSources() []SourceObservation {
	result := make([]SourceObservation, 0)
	for _, source := range observation.sources {
		if observation.state == StateUnobservable {
			if source.State() == SourceOpaque {
				result = append(result, source)
			}
			continue
		}
		if source.DefinesSelectedName() &&
			(source.Precedence() != PrecedenceSelected || source.Kind() != SourceNormal) {
			result = append(result, source)
		}
	}
	return result
}

// LowerFallbackPresent reports exact same-name content that may become
// effective after the managed contribution is removed.
func (observation Observation) LowerFallbackPresent() bool {
	return len(observation.LowerFallbackSources()) > 0
}

// LowerFallbackSources returns exact lower-precedence same-name definitions in
// provider observation order.
func (observation Observation) LowerFallbackSources() []SourceObservation {
	return observation.sameNameSourcesAt(PrecedenceLower)
}

// LowerFallbackEquivalence classifies the exact lower same-name definitions.
// Mixed or incomparable definitions remain unknown rather than guessing which
// unowned source the provider will ultimately select.
func (observation Observation) LowerFallbackEquivalence() (DefinitionEquivalence, bool) {
	var (
		found      bool
		equivalent bool
		different  bool
		unknown    bool
	)
	for _, source := range observation.sources {
		if source.State() != SourceExact ||
			!source.DefinesSelectedName() ||
			source.Precedence() != PrecedenceLower {
			continue
		}
		found = true
		switch source.DefinitionEquivalence() {
		case DefinitionEquivalenceEquivalent:
			equivalent = true
		case DefinitionEquivalenceDifferent:
			different = true
		case DefinitionEquivalenceUnknown:
			unknown = true
		}
	}
	switch {
	case !found:
		return DefinitionEquivalenceNotApplicable, false
	case unknown || (equivalent && different):
		return DefinitionEquivalenceUnknown, true
	case different:
		return DefinitionEquivalenceDifferent, true
	default:
		return DefinitionEquivalenceEquivalent, true
	}
}

// HigherConflictPresent reports exact same-name content above the selected
// managed source.
func (observation Observation) HigherConflictPresent() bool {
	return len(observation.HigherConflictSources()) > 0
}

// HigherConflictSources returns exact higher-precedence same-name definitions
// in provider observation order.
func (observation Observation) HigherConflictSources() []SourceObservation {
	return observation.sameNameSourcesAt(PrecedenceHigher)
}

func (observation Observation) sameNameSourcesAt(
	precedence RelativePrecedence,
) []SourceObservation {
	result := make([]SourceObservation, 0)
	for _, source := range observation.sources {
		if source.State() == SourceExact &&
			source.DefinesSelectedName() &&
			source.Precedence() == precedence {
			result = append(result, source)
		}
	}
	return result
}
