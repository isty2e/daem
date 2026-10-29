package tomlsyntax

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"time"

	parser "github.com/pelletier/go-toml/v2"
)

// Unmarshaler interprets a single raw TOML value at a schema boundary.
type Unmarshaler interface{ UnmarshalTOML([]byte) error }

// Decode is the context-free syntax boundary for small already-bounded fragments.
func Decode(content string, target any) (Metadata, error) {
	return DecodeAdmitted(context.Background(), []byte(content), target)
}

// Temporal retains the TOML temporal kind independently of parser-library types.
type Temporal struct {
	Kind string
	Text string
}

// DecodeAdmitted decodes already-admitted syntax. Unknown-field decisions and
// canonical schema spelling remain with the caller, not this grammar boundary.
// All consumers use the same TOML 1.1 parser; BOM handling is source ingress.
func DecodeAdmitted(ctx context.Context, content []byte, target any) (Metadata, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	input := bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	custom := usesCustomDecoder(reflect.TypeOf(target), make(map[reflect.Type]bool))
	if custom {
		var err error
		input, err = customDecoderInput(ctx, input)
		if err != nil {
			return Metadata{}, err
		}
	}
	err := parser.NewDecoder(bytes.NewReader(input)).EnableUnmarshalerInterface().Decode(target)
	if err != nil {
		var failure *parser.DecodeError
		if errors.As(err, &failure) && len(failure.Key()) != 0 {
			return Metadata{}, fmt.Errorf("decode TOML key %q: %w", failedValueKey(ctx, input, failure).String(), err)
		}
		return Metadata{}, err
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	if values, nativeMap := target.(*map[string]any); nativeMap {
		if _, err := normalizeNative(ctx, *values); err != nil {
			return Metadata{}, err
		}
	}
	index, err := IndexDocument(ctx, content)
	if err != nil {
		return Metadata{}, err
	}
	metadata := Metadata{index: index, digest: sha256.Sum256(content)}
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func failedValueKey(ctx context.Context, content []byte, failure *parser.DecodeError) Key {
	selected := Key(failure.Key())
	index, err := IndexDocument(ctx, content)
	if err != nil {
		return selected
	}
	lines := sourceLineStarts(content)
	line, column := failure.Position()
	if line < 1 || line > len(lines) || column < 1 {
		return selected
	}
	offset := lines[line-1] + column - 1
	for _, key := range index.keys {
		if key.value.Start <= offset && offset < key.value.End && len(key.path) > len(selected) {
			selected = key.path
		}
	}
	return selected
}

// DecodeValue interprets the raw value or table body supplied to a custom DTO.
func DecodeValue(content []byte) (any, error) {
	var value struct {
		Value any `toml:"value"`
	}
	input := append([]byte("value = "), content...)
	if err := parser.Unmarshal(input, &value); err != nil {
		var table map[string]any
		if tableErr := parser.Unmarshal(content, &table); tableErr != nil {
			return nil, err
		}
		return table, nil
	}
	return value.Value, nil
}

// Native decodes a document map without leaking parser-specific temporal types.
func Native(ctx context.Context, content []byte) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var values map[string]any
	if _, err := DecodeAdmitted(ctx, content, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func normalizeNative(ctx context.Context, value any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			converted, err := normalizeNative(ctx, child)
			if err != nil {
				return nil, err
			}
			value[key] = converted
		}
	case []any:
		for index, child := range value {
			converted, err := normalizeNative(ctx, child)
			if err != nil {
				return nil, err
			}
			value[index] = converted
		}
	case time.Time:
		return Temporal{"offset-date-time", value.Format(time.RFC3339Nano)}, nil
	case parser.LocalDate:
		return Temporal{"local-date", value.String()}, nil
	case parser.LocalTime:
		return Temporal{"local-time", value.String()}, nil
	case parser.LocalDateTime:
		return Temporal{"local-date-time", value.String()}, nil
	}
	return value, nil
}

func sourceLineStarts(content []byte) []int {
	lines := []int{0}
	for index, char := range content {
		if char == '\n' {
			lines = append(lines, index+1)
		}
	}
	return lines
}
