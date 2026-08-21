package mcpcodec

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"
)

var rawJSONMessageType = reflect.TypeFor[json.RawMessage]()

type boundedCanonicalJSONSize struct {
	bytes int64
}

func canonicalJSONEncodedSize(value any) (int64, error) {
	switch value.(type) {
	case ClaudeProjectMCPServerEntry,
		ClaudeGlobalMCPServerEntry,
		AntigravityGlobalMCPServerEntry,
		OpenCodeProjectMCPServerEntry,
		OpenCodeGlobalMCPServerEntry,
		PiMCPAdapterServerEntry,
		map[string]json.RawMessage:
	default:
		return 0, fmt.Errorf("unsupported canonical MCP JSON value type %T", value)
	}

	counter := boundedCanonicalJSONSize{}
	if err := counter.addValue(reflect.ValueOf(value), 0); err != nil {
		return 0, err
	}
	if err := counter.addBytes(1); err != nil {
		return 0, err
	}
	return counter.bytes, nil
}

func (counter *boundedCanonicalJSONSize) addValue(value reflect.Value, depth int) error {
	if value.Type() == rawJSONMessageType {
		if value.IsNil() {
			return counter.addBytes(int64(len("null")))
		}
		return counter.addRawJSON(value.Bytes(), depth)
	}

	switch value.Kind() {
	case reflect.Bool:
		if value.Bool() {
			return counter.addBytes(int64(len("true")))
		}
		return counter.addBytes(int64(len("false")))
	case reflect.Map:
		return counter.addMap(value, depth)
	case reflect.Slice, reflect.Array:
		return counter.addSequence(value, depth)
	case reflect.String:
		return counter.addJSONString(value.String())
	case reflect.Struct:
		return counter.addStruct(value, depth)
	default:
		return fmt.Errorf("unsupported canonical MCP JSON value kind %s", value.Kind())
	}
}

func (counter *boundedCanonicalJSONSize) addMap(value reflect.Value, depth int) error {
	if value.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("canonical MCP JSON map key type %s is unsupported", value.Type().Key())
	}
	if value.IsNil() {
		return counter.addBytes(int64(len("null")))
	}
	if value.Len() == 0 {
		return counter.addBytes(int64(len("{}")))
	}
	if err := counter.addBytes(1); err != nil {
		return err
	}
	iterator := value.MapRange()
	first := true
	for iterator.Next() {
		if !first {
			if err := counter.addBytes(1); err != nil {
				return err
			}
		}
		first = false
		if err := counter.addNewlineIndent(depth + 1); err != nil {
			return err
		}
		if err := counter.addJSONString(iterator.Key().String()); err != nil {
			return err
		}
		if err := counter.addBytes(int64(len(": "))); err != nil {
			return err
		}
		if err := counter.addValue(iterator.Value(), depth+1); err != nil {
			return err
		}
	}
	if err := counter.addNewlineIndent(depth); err != nil {
		return err
	}
	return counter.addBytes(1)
}

func (counter *boundedCanonicalJSONSize) addSequence(value reflect.Value, depth int) error {
	if value.Kind() == reflect.Slice && value.IsNil() {
		return counter.addBytes(int64(len("null")))
	}
	if value.Len() == 0 {
		return counter.addBytes(int64(len("[]")))
	}
	if err := counter.addBytes(1); err != nil {
		return err
	}
	for index := 0; index < value.Len(); index++ {
		if index > 0 {
			if err := counter.addBytes(1); err != nil {
				return err
			}
		}
		if err := counter.addNewlineIndent(depth + 1); err != nil {
			return err
		}
		if err := counter.addValue(value.Index(index), depth+1); err != nil {
			return err
		}
	}
	if err := counter.addNewlineIndent(depth); err != nil {
		return err
	}
	return counter.addBytes(1)
}

func (counter *boundedCanonicalJSONSize) addStruct(value reflect.Value, depth int) error {
	if err := counter.addBytes(1); err != nil {
		return err
	}
	included := 0
	valueType := value.Type()
	for index := 0; index < value.NumField(); index++ {
		fieldType := valueType.Field(index)
		if fieldType.PkgPath != "" {
			continue
		}
		name, omitEmpty, admitted, err := canonicalJSONField(fieldType)
		if err != nil {
			return err
		}
		if !admitted {
			continue
		}
		fieldValue := value.Field(index)
		if omitEmpty && isEmptyCanonicalJSONValue(fieldValue) {
			continue
		}
		if included > 0 {
			if err := counter.addBytes(1); err != nil {
				return err
			}
		}
		included++
		if err := counter.addNewlineIndent(depth + 1); err != nil {
			return err
		}
		if err := counter.addJSONString(name); err != nil {
			return err
		}
		if err := counter.addBytes(int64(len(": "))); err != nil {
			return err
		}
		if err := counter.addValue(fieldValue, depth+1); err != nil {
			return err
		}
	}
	if included > 0 {
		if err := counter.addNewlineIndent(depth); err != nil {
			return err
		}
	}
	return counter.addBytes(1)
}

func canonicalJSONField(field reflect.StructField) (string, bool, bool, error) {
	if field.Anonymous {
		return "", false, false, fmt.Errorf(
			"embedded canonical MCP JSON field %s is unsupported",
			field.Name,
		)
	}
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return "", false, false, fmt.Errorf(
			"canonical MCP JSON field %s requires an explicit json tag",
			field.Name,
		)
	}
	name, options, _ := strings.Cut(tag, ",")
	if name == "-" {
		return "", false, false, nil
	}
	if name == "" {
		return "", false, false, fmt.Errorf(
			"canonical MCP JSON field %s requires an explicit json name",
			field.Name,
		)
	}
	omitEmpty := false
	for options != "" {
		var option string
		option, options, _ = strings.Cut(options, ",")
		if option != "omitempty" {
			return "", false, false, fmt.Errorf(
				"canonical MCP JSON field %s uses unsupported json option %q",
				field.Name,
				option,
			)
		}
		omitEmpty = true
	}
	return name, omitEmpty, true, nil
}

func isEmptyCanonicalJSONValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return value.Len() == 0
	case reflect.Bool:
		return !value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return value.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return value.IsNil()
	default:
		return false
	}
}

func (counter *boundedCanonicalJSONSize) addJSONString(value string) error {
	if err := counter.addBytes(2); err != nil {
		return err
	}
	if err := counter.addBytes(int64(len(value))); err != nil {
		return err
	}
	for index := 0; index < len(value); {
		character := value[index]
		if character < utf8.RuneSelf {
			expansion := int64(0)
			switch character {
			case '\\', '"', '\b', '\f', '\n', '\r', '\t':
				expansion = 1
			case '<', '>', '&':
				expansion = 5
			default:
				if character < 0x20 {
					expansion = 5
				}
			}
			if expansion > 0 {
				if err := counter.addBytes(expansion); err != nil {
					return err
				}
			}
			index++
			continue
		}
		runeValue, width := utf8.DecodeRuneInString(value[index:])
		switch {
		case runeValue == utf8.RuneError && width == 1:
			if err := counter.addBytes(5); err != nil {
				return err
			}
		case runeValue == '\u2028' || runeValue == '\u2029':
			if err := counter.addBytes(3); err != nil {
				return err
			}
		}
		index += width
	}
	return nil
}

func (counter *boundedCanonicalJSONSize) addRawJSON(content []byte, depth int) error {
	needIndent := false
	inString := false
	escaped := false
	for index := 0; index < len(content); {
		character := content[index]
		if inString {
			switch {
			case escaped:
				escaped = false
				if err := counter.addBytes(1); err != nil {
					return err
				}
				index++
			case character == '\\':
				escaped = true
				if err := counter.addBytes(1); err != nil {
					return err
				}
				index++
			case character == '"':
				inString = false
				if err := counter.addBytes(1); err != nil {
					return err
				}
				index++
			case character == '<' || character == '>' || character == '&':
				if err := counter.addBytes(6); err != nil {
					return err
				}
				index++
			case character == 0xe2 && index+2 < len(content) &&
				content[index+1] == 0x80 && content[index+2]&^1 == 0xa8:
				if err := counter.addBytes(6); err != nil {
					return err
				}
				index += 3
			default:
				if err := counter.addBytes(1); err != nil {
					return err
				}
				index++
			}
			continue
		}

		if isJSONSpace(character) {
			index++
			continue
		}
		if needIndent && character != '}' && character != ']' {
			needIndent = false
			depth++
			if err := counter.addNewlineIndent(depth); err != nil {
				return err
			}
		}
		switch character {
		case '"':
			inString = true
			if err := counter.addBytes(1); err != nil {
				return err
			}
		case '{', '[':
			needIndent = true
			if err := counter.addBytes(1); err != nil {
				return err
			}
		case ',':
			if err := counter.addBytes(1); err != nil {
				return err
			}
			if err := counter.addNewlineIndent(depth); err != nil {
				return err
			}
		case ':':
			if err := counter.addBytes(2); err != nil {
				return err
			}
		case '}', ']':
			if needIndent {
				needIndent = false
			} else {
				depth--
				if err := counter.addNewlineIndent(depth); err != nil {
					return err
				}
			}
			if err := counter.addBytes(1); err != nil {
				return err
			}
		default:
			if err := counter.addBytes(1); err != nil {
				return err
			}
		}
		index++
	}
	return nil
}

func (counter *boundedCanonicalJSONSize) addNewlineIndent(depth int) error {
	if depth < 0 || int64(depth) > (maximumDocumentBytes-1)/2 {
		return validateMCPDocumentByteCount(maximumDocumentBytes + 1)
	}
	return counter.addBytes(1 + 2*int64(depth))
}

func (counter *boundedCanonicalJSONSize) addBytes(count int64) error {
	if count < 0 || count > maximumDocumentBytes-counter.bytes {
		return validateMCPDocumentByteCount(maximumDocumentBytes + 1)
	}
	counter.bytes += count
	return nil
}

func isJSONSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}
