package declaration

import (
	"bytes"
	"errors"

	"github.com/isty2e/daem/internal/encoding/toml"
	tomlfields "github.com/isty2e/daem/internal/encoding/toml/fields"
)

// PositionManifestKeyError maps canonical-field failures to manifest guidance.
// Metadata must describe exactly the document range; ranges start on a line.
func PositionManifestKeyError(content []byte, span DocumentRange, metadata tomlsyntax.Metadata, cause error) error {
	var canonical *tomlfields.NonCanonicalKeyError
	if !errors.As(cause, &canonical) {
		return cause
	}
	line, column, ok := tomlsyntax.KeyPosition(content[span.Start:span.End], metadata, canonical.Key())
	if ok {
		line += bytes.Count(content[:span.Start], []byte{'\n'})
	}
	return UnknownManifestKeyError{cause: cause, line: line, column: column}
}
