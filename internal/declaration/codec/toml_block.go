package codec

import (
	"context"

	"github.com/isty2e/daem/internal/declaration"
	tomlfields "github.com/isty2e/daem/internal/encoding/toml/fields"
)

func decodeBlockFields(content []byte, span declaration.DocumentRange, target any) error {
	metadata, err := tomlfields.DecodeAdmitted(context.Background(), content[span.Start:span.End], target)
	return declaration.PositionManifestKeyError(content, span, metadata, err)
}
