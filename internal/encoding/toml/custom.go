package tomlsyntax

import (
	"bytes"
	"context"
	"reflect"

	parser "github.com/pelletier/go-toml/v2"
)

func usesCustomDecoder(kind reflect.Type, seen map[reflect.Type]bool) bool {
	if kind == nil || seen[kind] {
		return false
	}
	seen[kind] = true
	contract := reflect.TypeFor[Unmarshaler]()
	if kind.Implements(contract) || reflect.PointerTo(kind).Implements(contract) {
		return true
	}
	switch kind.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return usesCustomDecoder(kind.Elem(), seen)
	case reflect.Struct:
		for index := range kind.NumField() {
			field := kind.Field(index)
			if field.IsExported() && field.Tag.Get("toml") != "-" && usesCustomDecoder(field.Type, seen) {
				return true
			}
		}
	}
	return false
}

// Custom table hooks otherwise receive concatenated bodies across array members.
// Normalize only their private decode input; public spans retain original bytes.
func customDecoderInput(ctx context.Context, content []byte) ([]byte, error) {
	var values map[string]any
	if err := parser.Unmarshal(content, &values); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var normalized bytes.Buffer
	if err := parser.NewEncoder(&normalized).SetTablesInline(true).Encode(values); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return normalized.Bytes(), nil
}
