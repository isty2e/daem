package contribution

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SourceProviderLabel is the redaction-safe provider spelling shown in source
// diagnostics. Canonical provider identity is carried separately by Carrier.
type SourceProviderLabel string

// SourceContributionKind identifies a provider-bundled source contribution kind.
type SourceContributionKind string

const (
	SourceContributionSkill     SourceContributionKind = "skill"
	SourceContributionMCPServer SourceContributionKind = "mcp-server"
	SourceContributionApp       SourceContributionKind = "app"
	SourceContributionHook      SourceContributionKind = "hook"
)

// SourceContributionSpec contains decoded provider contribution facts.
type SourceContributionSpec struct {
	Kind         SourceContributionKind
	Key          string
	SourceMarker string
}

// SourceContribution is one decoded source-declared contribution before
// provider-scoped topology lowering.
type SourceContribution struct {
	kind         SourceContributionKind
	key          string
	sourceMarker string
}

// NewSourceContribution validates and constructs one source-declared contribution.
func NewSourceContribution(spec SourceContributionSpec) (SourceContribution, error) {
	if !validSourceContributionKind(spec.Kind) {
		return SourceContribution{}, fmt.Errorf("source contribution has unsupported kind %q", spec.Kind)
	}
	if !ValidSourceToken(spec.Key) {
		return SourceContribution{}, fmt.Errorf("source contribution %s has unsafe key %q", spec.Kind, spec.Key)
	}
	if !ValidSourceToken(spec.SourceMarker) {
		return SourceContribution{}, fmt.Errorf("source contribution %s/%q has unsafe source marker %q", spec.Kind, spec.Key, spec.SourceMarker)
	}
	return SourceContribution{
		kind:         spec.Kind,
		key:          spec.Key,
		sourceMarker: spec.SourceMarker,
	}, nil
}

// Kind returns the provider-bundled contribution kind.
func (contribution SourceContribution) Kind() SourceContributionKind {
	return contribution.kind
}

// Key returns the provider-scoped contribution key.
func (contribution SourceContribution) Key() string {
	return contribution.key
}

// SourceMarker returns the bounded source-artifact marker that produced this row.
func (contribution SourceContribution) SourceMarker() string {
	return contribution.sourceMarker
}

// ValidSourceToken reports whether a token is safe for source-inspection diagnostics.
func ValidSourceToken(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	return !ContainsUnsafeDiagnosticRune(value)
}

// ContainsUnsafeDiagnosticRune reports whether value contains unsafe diagnostic text.
func ContainsUnsafeDiagnosticRune(value string) bool {
	if !utf8.ValidString(value) {
		return true
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character) {
			return true
		}
	}
	return false
}

func validSourceContributionKind(kind SourceContributionKind) bool {
	switch kind {
	case SourceContributionSkill,
		SourceContributionMCPServer,
		SourceContributionApp,
		SourceContributionHook:
		return true
	default:
		return false
	}
}
