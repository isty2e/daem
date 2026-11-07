// Package hostconfig owns desired native configuration sources and placement
// intent. Host grammar selection, reservations and filesystem effects stay out.
package hostconfig

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

type Ownership string

const (
	OwnershipKeys     Ownership = "keys"
	OwnershipDocument Ownership = "document"
)

func ParseOwnership(value string) (Ownership, error) {
	switch Ownership(value) {
	case "", OwnershipKeys:
		return OwnershipKeys, nil
	case OwnershipDocument:
		return OwnershipDocument, nil
	default:
		return "", fmt.Errorf("unknown host configuration ownership %q", value)
	}
}

// Syntax is requested grammar intent. Default defers to the host profile,
// rather than guessing the format from document contents.
type Syntax string

const (
	SyntaxDefault          Syntax = ""
	SyntaxJSON             Syntax = "json"
	SyntaxJSONC            Syntax = "jsonc"
	SyntaxJSONLineComments Syntax = "json-line-comments"
	SyntaxTOML             Syntax = "toml"
)

func ParseSyntax(value string) (Syntax, error) {
	switch Syntax(value) {
	case SyntaxDefault, SyntaxJSON, SyntaxJSONC, SyntaxJSONLineComments, SyntaxTOML:
		return Syntax(value), nil
	default:
		return "", fmt.Errorf("unknown host configuration syntax %q", value)
	}
}

// Spec is input for one host-root-relative configuration declaration. Target
// cardinality is normalized at ingress; one canonical Config has one target.
type Spec struct {
	Name      string
	Source    source.Source
	Target    target.Target
	Scope     target.Scope
	Path      string
	Ownership Ownership
	Syntax    Syntax
}

type Config struct {
	id        entity.ID
	source    source.Source
	target    target.Target
	scope     target.Scope
	path      string
	ownership Ownership
	syntax    Syntax
}

func New(spec Spec) (Config, error) {
	if strings.IndexFunc(spec.Name, unsafeControl) >= 0 {
		return Config{}, fmt.Errorf("host configuration id must not contain control characters")
	}
	id, err := entity.New(entity.KindHostConfig, spec.Name)
	if err != nil {
		return Config{}, err
	}

	selected, err := target.ParseTarget(string(spec.Target))
	if err != nil {
		return Config{}, err
	}
	scope, err := target.ParseScope(string(spec.Scope))
	if err != nil {
		return Config{}, err
	}

	if err := ValidateRelativePath(spec.Path); err != nil {
		return Config{}, err
	}
	if err := validateSource(spec.Source, scope); err != nil {
		return Config{}, err
	}

	ownership, err := ParseOwnership(string(spec.Ownership))
	if err != nil {
		return Config{}, err
	}
	syntax, err := ParseSyntax(string(spec.Syntax))
	if err != nil {
		return Config{}, err
	}

	return Config{id: id, source: spec.Source, target: selected, scope: scope, path: spec.Path, ownership: ownership, syntax: syntax}, nil
}

// ValidateRelativePath refuses noncanonical paths without cleaning or expansion.
func ValidateRelativePath(value string) error {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return fmt.Errorf("host configuration path must be non-empty, trimmed UTF-8")
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, `\`) || strings.IndexFunc(value, unsafeControl) >= 0 {
		return fmt.Errorf("host configuration path must be a safe host-root-relative slash path")
	}
	if len(value) >= 2 && value[1] == ':' && (value[0] >= 'a' && value[0] <= 'z' || value[0] >= 'A' && value[0] <= 'Z') {
		return fmt.Errorf("host configuration path must not select a filesystem drive")
	}
	for component := range strings.SplitSeq(value, "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("host configuration path must not contain empty or relative components")
		}
	}
	return nil
}

func unsafeControl(value rune) bool {
	return unicode.IsControl(value) || unicode.Is(unicode.Bidi_Control, value)
}

func validateSource(value source.Source, scope target.Scope) error {
	if _, err := source.SourceIDFor(value); err != nil {
		return err
	}
	if local, ok := value.Local(); ok {
		if local.Mode() != source.LocalSourceModeVendor {
			return fmt.Errorf("host configuration local sources must use vendor mode")
		}
		if scope == target.ScopeGlobal && !local.IsAbsolute() {
			return fmt.Errorf("global local filesystem sources must use an absolute path")
		}
	}
	if s3, ok := value.S3(); ok && s3.Format() != source.S3ObjectFormatFile {
		return fmt.Errorf("host configuration S3 sources must use file format")
	}
	return nil
}

func (config Config) ID() entity.ID         { return config.id }
func (config Config) Source() source.Source { return config.source }
func (config Config) Target() target.Target { return config.target }
func (config Config) Scope() target.Scope   { return config.scope }
func (config Config) Path() string          { return config.path }
func (config Config) Ownership() Ownership  { return config.ownership }
func (config Config) Syntax() Syntax        { return config.syntax }

func (config Config) Validate() error {
	if config.id.Kind() != entity.KindHostConfig {
		return fmt.Errorf("host configuration has invalid entity kind")
	}
	_, err := New(Spec{
		Name: config.id.Name(), Source: config.source, Target: config.target,
		Scope: config.scope, Path: config.path, Ownership: config.ownership, Syntax: config.syntax,
	})
	return err
}
