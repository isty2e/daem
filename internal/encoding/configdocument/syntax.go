// Package configdocument edits native configuration values without owning host
// paths, resource policy, or filesystem effects.
package configdocument

import (
	"errors"
	"fmt"
)

// Syntax is an admitted native document grammar, not a filename inference.
type Syntax uint8

const (
	JSON Syntax = iota + 1
	JSONC
	JSONLineComments
	TOML
)

// ParseSyntax requires an explicit grammar; host defaulting happens at placement.
func ParseSyntax(value string) (Syntax, error) {
	switch value {
	case "json":
		return JSON, nil
	case "jsonc":
		return JSONC, nil
	case "json-line-comments":
		return JSONLineComments, nil
	case "toml":
		return TOML, nil
	default:
		return 0, fmt.Errorf("unknown native configuration syntax %q", value)
	}
}

func (syntax Syntax) String() string {
	switch syntax {
	case JSON:
		return "json"
	case JSONC:
		return "jsonc"
	case JSONLineComments:
		return "json-line-comments"
	case TOML:
		return "toml"
	default:
		return ""
	}
}

func (syntax Syntax) validate() error {
	if syntax.String() == "" {
		return fmt.Errorf("native configuration syntax is required")
	}
	return nil
}

// Limits are supplied by the owning document contract. They are not inferred
// from settings or environment variables.
type Limits struct {
	MaximumBytes int64
	MaximumDepth int
}

func (limits Limits) Validate() error {
	if limits.MaximumBytes <= 0 || limits.MaximumDepth <= 0 {
		return fmt.Errorf("positive native configuration byte and depth limits are required")
	}
	return nil
}

var (
	ErrInvalidDocument   = errors.New("invalid native configuration document")
	ErrInvalidValue      = errors.New("invalid canonical configuration value")
	ErrLimitExceeded     = errors.New("native configuration limit exceeded")
	ErrPathConflict      = errors.New("configuration key path crosses a non-object value")
	ErrOverlappingChange = errors.New("configuration changes overlap")
)
