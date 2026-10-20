package codec

import (
	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
)

func decodeBlockFields(content []byte, span declaration.DocumentRange, target any) error {
	metadata, err := toml.Decode(string(content[span.Start:span.End]), target)
	if err == nil {
		err = tomlstrict.ValidateCanonicalKeys(metadata, target)
	}
	return declaration.PositionManifestKeyError(content, span, metadata, err)
}
