package fields

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

type canonicalTypeMetadata struct {
	fields map[reflect.Type][]canonicalField
	err    error
}

var canonicalTypeCache sync.Map // map[reflect.Type]func() canonicalTypeMetadata

func canonicalMetadataFor(typ reflect.Type) canonicalTypeMetadata {
	if cached, ok := canonicalTypeCache.Load(typ); ok {
		return cached.(func() canonicalTypeMetadata)()
	}

	build := sync.OnceValue(func() canonicalTypeMetadata {
		fields := make(map[reflect.Type][]canonicalField)
		visited := make(map[reflect.Type]bool)
		err := collectCanonicalTypeFields(typ, fields, visited)
		return canonicalTypeMetadata{fields: fields, err: err}
	})

	// Recursive child traversal stays local; callers only see a completed result.
	actual, _ := canonicalTypeCache.LoadOrStore(typ, build)
	return actual.(func() canonicalTypeMetadata)()
}

func collectCanonicalTypeFields(typ reflect.Type, fields map[reflect.Type][]canonicalField, visited map[reflect.Type]bool) error {
	if visited[typ] {
		return nil
	}
	visited[typ] = true
	if canonicalKeyLeaf(typ) {
		return nil
	}

	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return collectCanonicalTypeFields(typ.Elem(), fields, visited)
	case reflect.Struct:
		compiled := make([]canonicalField, 0, typ.NumField())
		for index := range typ.NumField() {
			field := typ.Field(index)
			name, _, _ := strings.Cut(field.Tag.Get("toml"), ",")
			if name == "-" {
				continue
			}
			if field.Anonymous {
				return fmt.Errorf("TOML type %s must not contain anonymous field %s", typ, field.Name)
			}
			if !field.IsExported() {
				continue
			}
			if name == "" {
				return fmt.Errorf("TOML field %s.%s requires an explicit nonempty toml tag", typ, field.Name)
			}
			compiled = append(compiled, canonicalField{name: name, typ: field.Type})
		}
		fields[typ] = compiled
		for _, field := range compiled {
			if err := collectCanonicalTypeFields(field.typ, fields, visited); err != nil {
				return err
			}
		}
	}
	return nil
}
