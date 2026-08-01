// Package jsonstrict validates JSON syntax shared by private persistence boundaries.
package jsonstrict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Validate requires one UTF-8 JSON value with unique object keys and bounded nesting.
func Validate(content []byte, document string, maximumDepth int) error {
	if strings.TrimSpace(document) == "" {
		return fmt.Errorf("JSON document label is required")
	}
	if maximumDepth <= 0 {
		return fmt.Errorf("%s JSON maximum depth must be positive", document)
	}
	if !utf8.Valid(content) {
		return fmt.Errorf("%s is not valid UTF-8", document)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := consumeValue(decoder, document, maximumDepth, 0); err != nil {
		return err
	}
	if token, err := decoder.Token(); err == nil {
		return fmt.Errorf("%s contains multiple JSON values beginning with %v", document, token)
	} else if err != io.EOF {
		return fmt.Errorf("parse %s trailer: %w", document, err)
	}
	return nil
}

// ValidateVersionedObject validates one strict JSON object and returns its
// required positive integer version. Schema-specific fields remain opaque.
func ValidateVersionedObject(content []byte, document string, maximumDepth int) (int, error) {
	if err := Validate(content, document, maximumDepth); err != nil {
		return 0, err
	}

	var envelope struct {
		Version json.RawMessage `json:"version"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		return 0, fmt.Errorf("%s must be a JSON object: %w", document, err)
	}
	if bytes.Equal(bytes.TrimSpace(content), []byte("null")) {
		return 0, fmt.Errorf("%s must be a JSON object", document)
	}
	if envelope.Version == nil {
		return 0, fmt.Errorf("%s field %q is required", document, "version")
	}
	var version int
	if err := json.Unmarshal(envelope.Version, &version); err != nil || version <= 0 {
		return 0, fmt.Errorf("%s field %q must be a positive integer", document, "version")
	}
	return version, nil
}

func consumeValue(decoder *json.Decoder, document string, maximumDepth int, depth int) error {
	if depth > maximumDepth {
		return fmt.Errorf("%s JSON exceeds maximum depth %d", document, maximumDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("parse %s: %w", document, err)
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("parse %s object key: %w", document, err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("%s object key is not a string", document)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%s contains duplicate object key %q", document, key)
			}
			seen[key] = struct{}{}
			if err := consumeValue(decoder, document, maximumDepth, depth+1); err != nil {
				return err
			}
		}
		return consumeClosingDelimiter(decoder, document, '}')
	case '[':
		for decoder.More() {
			if err := consumeValue(decoder, document, maximumDepth, depth+1); err != nil {
				return err
			}
		}
		return consumeClosingDelimiter(decoder, document, ']')
	default:
		return fmt.Errorf("%s has unexpected JSON delimiter %q", document, delimiter)
	}
}

func consumeClosingDelimiter(decoder *json.Decoder, document string, expected json.Delim) error {
	closing, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("parse %s closing delimiter: %w", document, err)
	}
	if closing != expected {
		return fmt.Errorf("%s has invalid closing delimiter %v", document, closing)
	}
	return nil
}
