package tomlstrict

import (
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
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
// are unsupported. Custom decoders and Primitive own their grammar.
func ValidateCanonicalKeys(metadata toml.MetaData, target any) error {
	typ := reflect.TypeOf(target)
	if typ == nil {
		return fmt.Errorf("canonical TOML keys require a decoded target")
	}
	compiled := canonicalMetadataFor(typ)
	if compiled.err != nil {
		return compiled.err
	}
	validator := canonicalKeyValidator{fields: compiled.fields}

	undecoded := make(map[string]struct{})
	for _, key := range metadata.Undecoded() {
		undecoded[key.String()] = struct{}{}
	}
	for _, key := range metadata.Keys() {
		if _, skipped := undecoded[key.String()]; skipped {
			continue
		}
		if err := validator.validateKey(typ, key); err != nil {
			return err
		}
	}
	return nil
}

type canonicalField struct {
	name string
	typ  reflect.Type
}

type canonicalKeyValidator struct {
	fields map[reflect.Type][]canonicalField
}

func (validator *canonicalKeyValidator) validateKey(typ reflect.Type, key toml.Key) error {
	remaining := key
	for {
		if len(remaining) == 0 {
			return nil
		}
		var custom bool
		typ, custom = canonicalKeyType(typ)
		if custom {
			return nil
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
				failure := &NonCanonicalKeyError{key: slices.Clone(key)}
				for _, field := range fields {
					// Folding supplies repair guidance, never field admission.
					if strings.EqualFold(field.name, remaining[0]) {
						failure.canonical = field.name
						break
					}
				}
				return failure
			}
			typ = next
			remaining = remaining[1:]
		case reflect.Map:
			typ = typ.Elem()
			remaining = remaining[1:]
		default:
			return nil
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
	if typ == reflect.TypeFor[toml.Primitive]() ||
		typ.Implements(reflect.TypeFor[toml.Unmarshaler]()) ||
		typ.Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		return true
	}
	return typ.Kind() != reflect.Pointer &&
		(reflect.PointerTo(typ).Implements(reflect.TypeFor[toml.Unmarshaler]()) ||
			reflect.PointerTo(typ).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()))
}
