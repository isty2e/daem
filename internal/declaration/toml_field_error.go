package declaration

import (
	"bytes"
	"errors"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
)

// PositionManifestKeyError maps canonical-field failures to manifest guidance.
// Metadata must describe exactly the document range; ranges start on a line.
func PositionManifestKeyError(content []byte, span DocumentRange, metadata toml.MetaData, cause error) error {
	var canonical *tomlstrict.NonCanonicalKeyError
	if !errors.As(cause, &canonical) {
		return cause
	}
	line, column, ok := tomlstrict.KeyPosition(content[span.Start:span.End], metadata, canonical.Key())
	if ok {
		line += bytes.Count(content[:span.Start], []byte{'\n'})
	}
	return UnknownManifestKeyError{cause: cause, line: line, column: column}
}
