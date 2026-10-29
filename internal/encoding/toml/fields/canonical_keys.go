// Package fields applies explicit-tag mapping policy at daem-owned DTO boundaries.
package fields

import (
	"context"
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strings"

	toml "github.com/isty2e/daem/internal/encoding/toml"
)

// NonCanonicalKeyError retains the authored metadata path for source positioning.
type NonCanonicalKeyError struct {
	key       toml.Key
	canonical string
}

func (err *NonCanonicalKeyError) Error() string {
	if err.canonical != "" {
		return fmt.Sprintf("TOML key %q must use canonical spelling %q", err.key.String(), err.canonical)
	}
	return fmt.Sprintf("decoded TOML key %q must match an explicit TOML field name", err.key.String())
}

// Key returns the complete observed path, including implicit dotted parents.
func (err *NonCanonicalKeyError) Key() toml.Key { return slices.Clone(err.key) }

// ValidateCanonicalKeys validates all statically reachable struct definitions,
// then decoded key spellings. Undecoded paths retain the caller's policy.
// Non-ignored exported fields require named tags; non-ignored anonymous fields
// are unsupported. Custom decoders own their value interpretation.
func ValidateCanonicalKeys(metadata toml.Metadata, target any) error {
	_, err := classifyKeys(context.Background(), metadata, target)
	return err
}

func classifyKeys(ctx context.Context, metadata toml.Metadata, target any) ([]toml.Key, error) {
	typ := reflect.TypeOf(target)
	if typ == nil {
		return nil, fmt.Errorf("canonical TOML keys require a decoded target")
	}
	compiled := canonicalMetadataFor(typ)
	if compiled.err != nil {
		return nil, compiled.err
	}
	validator := canonicalKeyValidator{fields: compiled.fields}

	var undecoded []toml.Key
	for _, key := range metadata.Keys() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		known, err := validator.classifyKey(typ, key)
		if err != nil {
			return nil, err
		}
		if !known {
			undecoded = append(undecoded, key)
		}
	}
	return undecoded, nil
}

type canonicalField struct {
	name string
	typ  reflect.Type
}

type canonicalKeyValidator struct {
	fields map[reflect.Type][]canonicalField
}

func (validator *canonicalKeyValidator) classifyKey(typ reflect.Type, key toml.Key) (bool, error) {
	remaining := key
	for {
		if len(remaining) == 0 {
			return true, nil
		}
		var custom bool
		typ, custom = canonicalKeyType(typ)
		if custom {
			return true, nil
		}
		switch typ.Kind() {
		case reflect.Struct:
			fields := validator.fields[typ]
			var next reflect.Type
			for _, field := range fields {
				if field.name == remaining[0] {
					next = field.typ
					break
				}
			}
			if next == nil {
				for _, field := range fields {
					// Folding supplies repair guidance, never field admission.
					if strings.EqualFold(field.name, remaining[0]) {
						return false, &NonCanonicalKeyError{key: slices.Clone(key), canonical: field.name}
					}
				}
				return false, nil
			}
			typ = next
			remaining = remaining[1:]
		case reflect.Map:
			typ = typ.Elem()
			remaining = remaining[1:]
		default:
			return true, nil
		}
	}
}

func canonicalKeyType(typ reflect.Type) (reflect.Type, bool) {
	for {
		if canonicalKeyLeaf(typ) {
			return typ, true
		}
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			typ = typ.Elem()
		default:
			return typ, false
		}
	}
}

func canonicalKeyLeaf(typ reflect.Type) bool {
	if typ.Implements(reflect.TypeFor[toml.Unmarshaler]()) ||
		typ.Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		return true
	}
	return typ.Kind() != reflect.Pointer &&
		(reflect.PointerTo(typ).Implements(reflect.TypeFor[toml.Unmarshaler]()) ||
			reflect.PointerTo(typ).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()))
}
