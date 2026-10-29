package configdocument

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Value is immutable native data. Its canonical encoding retains distinctions
// such as JSON null, TOML integers/floats, and TOML temporal types.
type Value struct{ data nativeValue }

type (
	nativeValue    interface{ nativeValue() }
	nullNode       struct{}
	boolNode       bool
	stringNode     string
	jsonNumberNode string
	integerNode    int64
	floatNode      float64
	arrayNode      []Value
	objectNode     []objectMember
	temporalNode   struct{ kind, text string }
)

type objectMember struct {
	key   string
	value Value
}

func (nullNode) nativeValue()       {}
func (boolNode) nativeValue()       {}
func (stringNode) nativeValue()     {}
func (jsonNumberNode) nativeValue() {}
func (integerNode) nativeValue()    {}
func (floatNode) nativeValue()      {}
func (arrayNode) nativeValue()      {}
func (objectNode) nativeValue()     {}
func (temporalNode) nativeValue()   {}

func newString(value string) (Value, error) {
	if !utf8.ValidString(value) {
		return Value{}, fmt.Errorf("%w: string must be valid UTF-8", ErrInvalidValue)
	}
	return Value{data: stringNode(value)}, nil
}

func newArray(values []Value) (Value, error) {
	for _, value := range values {
		if value.data == nil {
			return Value{}, fmt.Errorf("%w: array contains an uninitialized value", ErrInvalidValue)
		}
	}
	return Value{data: arrayNode(slices.Clone(values))}, nil
}

func newObject(values []objectMember) (Value, error) {
	members := slices.Clone(values)
	slices.SortFunc(members, func(left, right objectMember) int {
		return strings.Compare(left.key, right.key)
	})
	for index, member := range members {
		if !utf8.ValidString(member.key) || member.value.data == nil {
			return Value{}, fmt.Errorf("%w: invalid object member", ErrInvalidValue)
		}
		if index > 0 && members[index-1].key == member.key {
			return Value{}, fmt.Errorf("%w: duplicate object key", ErrInvalidValue)
		}
	}
	return Value{data: objectNode(members)}, nil
}

func newTemporal(kind, text string) (Value, error) {
	layout := ""
	switch kind {
	case "offset-date-time":
		layout = time.RFC3339Nano
	case "local-date-time":
		layout = "2006-01-02T15:04:05.999999999"
	case "local-date":
		layout = "2006-01-02"
	case "local-time":
		layout = "15:04:05.999999999"
	default:
		return Value{}, fmt.Errorf("%w: unknown temporal kind", ErrInvalidValue)
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "t", "T"), "z", "Z")
	if kind != "local-time" {
		normalized = strings.Replace(normalized, " ", "T", 1)
	}
	parsed, err := time.Parse(layout, normalized)
	if err != nil {
		return Value{}, fmt.Errorf("%w: invalid %s", ErrInvalidValue, kind)
	}
	return Value{data: temporalNode{kind: kind, text: parsed.Format(layout)}}, nil
}

// IsObject reports native object/table structure, including an empty object.
func (value Value) IsObject() bool { _, ok := value.data.(objectNode); return ok }

// IsArray reports an atomic array, including a TOML array of tables.
func (value Value) IsArray() bool { _, ok := value.data.(arrayNode); return ok }

func (value Value) IsAtomic() bool {
	if value.data == nil {
		return false
	}
	if members, object := value.data.(objectNode); object {
		return len(members) == 0
	}
	return true
}

// Equal compares typed native meaning, not source spelling or presentation.
func (value Value) Equal(other Value) bool {
	return equalNative(value, other)
}

// Canonical returns versioned private value data, not host syntax or display
// text. The zero Value has no encoding.
// It is unbounded; operation-owned encoding uses CanonicalContext.
func (value Value) Canonical() string {
	if value.data == nil {
		return ""
	}
	encoder := canonicalEncoder{ctx: context.Background()}
	encoder.raw("v1:")
	encoder.encode(value, 0)
	return encoder.buffer.String()
}

func canonicalFloat(value float64) string {
	switch {
	case math.IsNaN(value):
		return "nan"
	case math.IsInf(value, 1):
		return "inf"
	case math.IsInf(value, -1):
		return "-inf"
	default:
		return strconv.FormatFloat(value, 'g', -1, 64)
	}
}

// DecodeValue admits canonical private data for the selected native grammar.
// Its byte limit applies to this encoding, independently of document ingress.
func DecodeValue(ctx context.Context, syntax Syntax, canonical string, limits Limits) (Value, error) {
	if ctx == nil {
		return Value{}, fmt.Errorf("configuration context is required")
	}
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}
	if err := syntax.validate(); err != nil {
		return Value{}, err
	}
	if err := limits.Validate(); err != nil {
		return Value{}, err
	}
	if int64(len(canonical)) > limits.MaximumBytes {
		return Value{}, ErrLimitExceeded
	}
	data, found := strings.CutPrefix(canonical, "v1:")
	if !found {
		return Value{}, fmt.Errorf("%w: unknown encoding version", ErrInvalidValue)
	}
	value, err := decodeCanonical(ctx, syntax, json.RawMessage(data), limits.MaximumDepth, 0)
	if err != nil {
		return Value{}, err
	}
	encoded, err := value.CanonicalContext(ctx, limits)
	if err != nil {
		return Value{}, err
	}
	if encoded != canonical {
		return Value{}, fmt.Errorf("%w: noncanonical encoding", ErrInvalidValue)
	}
	return value, nil
}

func decodeCanonical(ctx context.Context, syntax Syntax, data json.RawMessage, maximumDepth, depth int) (Value, error) {
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}
	if depth > maximumDepth {
		return Value{}, ErrLimitExceeded
	}
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) < 1 || len(fields) > 2 {
		return Value{}, ErrInvalidValue
	}
	var kind string
	if err := json.Unmarshal(fields[0], &kind); err != nil {
		return Value{}, ErrInvalidValue
	}
	if (kind == "array" || kind == "object") && depth >= maximumDepth {
		return Value{}, ErrLimitExceeded
	}
	if kind == "null" {
		if len(fields) != 1 || syntax == TOML {
			return Value{}, ErrInvalidValue
		}
		return Value{data: nullNode{}}, nil
	}
	if len(fields) != 2 {
		return Value{}, ErrInvalidValue
	}
	switch kind {
	case "bool":
		var value bool
		if err := json.Unmarshal(fields[1], &value); err != nil || string(fields[1]) == "null" {
			return Value{}, ErrInvalidValue
		}
		return Value{data: boolNode(value)}, nil
	case "array":
		var encoded []json.RawMessage
		if err := json.Unmarshal(fields[1], &encoded); err != nil || string(fields[1]) == "null" {
			return Value{}, ErrInvalidValue
		}
		values := make([]Value, 0, len(encoded))
		for _, child := range encoded {
			value, err := decodeCanonical(ctx, syntax, child, maximumDepth, depth+1)
			if err != nil {
				return Value{}, err
			}
			values = append(values, value)
		}
		return newArray(values)
	case "object":
		var encoded [][]json.RawMessage
		if err := json.Unmarshal(fields[1], &encoded); err != nil || string(fields[1]) == "null" {
			return Value{}, ErrInvalidValue
		}
		values := make([]objectMember, 0, len(encoded))
		for _, member := range encoded {
			if len(member) != 2 {
				return Value{}, ErrInvalidValue
			}
			var key string
			if err := json.Unmarshal(member[0], &key); err != nil || string(member[0]) == "null" {
				return Value{}, ErrInvalidValue
			}
			value, err := decodeCanonical(ctx, syntax, member[1], maximumDepth, depth+1)
			if err != nil {
				return Value{}, err
			}
			values = append(values, objectMember{key: key, value: value})
		}
		return newObject(values)
	default:
		var text string
		if err := json.Unmarshal(fields[1], &text); err != nil || string(fields[1]) == "null" {
			return Value{}, ErrInvalidValue
		}
		return decodeScalar(syntax, kind, text)
	}
}

func decodeScalar(syntax Syntax, kind, text string) (Value, error) {
	if kind == "string" {
		return newString(text)
	}
	if kind == "json-number" {
		if syntax == TOML {
			return Value{}, ErrInvalidValue
		}
		return newJSONNumber(text)
	}
	if syntax != TOML {
		return Value{}, ErrInvalidValue
	}
	switch kind {
	case "integer":
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return Value{}, ErrInvalidValue
		}
		return Value{data: integerNode(parsed)}, nil
	case "float":
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return Value{}, ErrInvalidValue
		}
		return Value{data: floatNode(parsed)}, nil
	default:
		return newTemporal(kind, text)
	}
}
